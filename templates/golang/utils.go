package main

import (
	"crypto/rand"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lntypes"
)

// genPreimage creates a random preimage, panicing if it fails.
func genPreimage() lntypes.Preimage {
	randomBytes := make([]byte, 32)

	// Fill the byte slice with random data
	_, err := rand.Read(randomBytes)
	if err != nil {
		panic(err)
	}

	preimage, err := lntypes.MakePreimage(randomBytes)
	if err != nil {
		panic(err)
	}

	return preimage
}

func outpointFromRPC(rpc *lnrpc.ChannelPoint) *wire.OutPoint {
	var (
		hash *chainhash.Hash
		err  error
	)

	switch h := rpc.FundingTxid.(type) {
	case *lnrpc.ChannelPoint_FundingTxidBytes:
		hash, err = chainhash.NewHash(h.FundingTxidBytes)

	case *lnrpc.ChannelPoint_FundingTxidStr:
		hash, err = chainhash.NewHashFromStr(h.FundingTxidStr)

	default:
		panic("Unknown channel point type")
	}

	if err != nil {
		panic(err)
	}

	return wire.NewOutPoint(hash, rpc.OutputIndex)
}
