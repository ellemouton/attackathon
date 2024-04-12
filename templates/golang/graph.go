package main

import (
	"context"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
)

// GraphHarness is responsible for all graph related operations (P2P, channels,
// gossip etc).
type GraphHarness struct {
	LndNodes Nodes
}

type OpenChannelReq struct {
	Source int
	Dest   route.Vertex
	// Host is an optional host address for the node, we'll lookup in our
	// graph if this value is empty.
	Host        string
	CapacitySat btcutil.Amount
	PushAmt     btcutil.Amount
	Private     bool
}

// OpenChannel is a blocking call that opens a channel from the source node
// provided to the target:
// - looks up a node in the graph
// - connects to it if we are not currently connected
// - opens a channel with the parameters provided
// - opens a channel and mines block to confirm it
// - waits for the channel to be active
func (c *GraphHarness) OpenChannel(ctx context.Context,
	req OpenChannelReq) (*wire.OutPoint, error) {

	connected, err := c.PeerConnected(ctx, req.Source, req.Dest)
	if err != nil {
		return nil, err
	}

	if !connected {
		host := req.Host
		if host == "" {
			node, err := c.LookupNode(
				ctx, req.Source, req.Dest, false,
			)
			if err != nil {
				return nil, err
			}

			if len(node.Addresses) == 0 {
				return nil, fmt.Errorf("no public address "+
					"for: %v", req.Dest)
			}

			host = node.Addresses[0]
		}

		err = c.ConnectPeer(ctx, req.Source, req.Dest, host)
		if err != nil {
			return nil, err
		}
	}

	sourceNode := c.LndNodes.GetNode(req.Source)
	streamChan, errChan, err := sourceNode.Client.OpenChannelStream(
		ctx, req.Dest, req.CapacitySat, req.PushAmt, req.Private,
	)
	if err != nil {
		return nil, err
	}
	for {
		select {
		case update := <-streamChan:
                        // Wait for the channel to be pending before we mine.
			if update.ChanPending != nil {
				if err := Mine(6); err != nil {
					return nil, fmt.Errorf("could not "+
						"mine: %v", err)
				}
			}

			if update.ChanOpen != nil {
				return outpointFromRPC(
					update.ChanOpen.ChannelPoint,
				), nil
			}

		case e := <-errChan:
			return nil, e

		case <-ctx.Done():
			return nil, ctx.Err()
		}

	}
}

func (c *GraphHarness) LookupNode(ctx context.Context, source int,
	dest route.Vertex, includeChannels bool) (*lndclient.NodeInfo, error) {

	sourceNode := c.LndNodes.GetNode(source)
	destNode, err := sourceNode.Client.GetNodeInfo(
		ctx, dest, includeChannels,
	)
	if err != nil {
		return nil, err
	}

	return destNode, nil
}

func (c *GraphHarness) ConnectPeer(ctx context.Context, source int,
	dest route.Vertex, addr string) error {

	sourceNode := c.LndNodes.GetNode(source)
	if err := sourceNode.Client.Connect(ctx, dest, addr, true); err != nil {
		return err
	}

	for i := 0; i < 5; i++ {
		connected, err := c.PeerConnected(ctx, source, dest)
		if err != nil || connected {
			return err
		}

		select {
		case <-time.After(time.Second):

		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("timeout waiting for peer: %v to connect", dest)
}

func (c *GraphHarness) PeerConnected(ctx context.Context, source int,
	target route.Vertex) (bool, error) {

	sourceNode := c.LndNodes.GetNode(source)

	peers, err := sourceNode.Client.ListPeers(ctx)
	if err != nil {
		return false, nil
	}

	for _, peer := range peers {
		if peer.Pubkey == target {
			return true, nil
		}
	}

	return false, nil
}
