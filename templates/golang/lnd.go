package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lightninglabs/lndclient"
)

type LndNodes struct {
	Lnd0 lndclient.LndServices
	Lnd1 lndclient.LndServices
	Lnd2 lndclient.LndServices
}

func getLndNodes(ctx context.Context) (*LndNodes, error) {
	lnd0, err := getLndClient(0)
	if err != nil {
		return nil, err
	}

	info, err := lnd0.Client.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Connected to node 0: %x\n", info.IdentityPubkey)

	lnd1, err := getLndClient(1)
	if err != nil {
		return nil, err
	}

	info, err = lnd1.Client.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Connected to node 1: %x\n", info.IdentityPubkey)

	lnd2, err := getLndClient(2)
	if err != nil {
		return nil, err
	}

	info, err = lnd2.Client.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Connected to node 2: %x\n", info.IdentityPubkey)

	return &LndNodes{
		Lnd0: lnd0.LndServices,
		Lnd1: lnd1.LndServices,
		Lnd2: lnd2.LndServices,
	}, nil
}

func getLndClient(nodeIdx int) (*lndclient.GrpcLndServices, error) {
	server := os.Getenv(fmt.Sprintf("LND_%v_RPCSERVER", nodeIdx))
	if server == "" {
		return nil, fmt.Errorf("client: %v server env var not found",
			nodeIdx)
	}

	cert := os.Getenv(fmt.Sprintf("LND_%v_CERT", nodeIdx))
	if cert == "" {
		return nil, fmt.Errorf("client: %v cert env var not found",
			nodeIdx)
	}

	macaroon := os.Getenv(fmt.Sprintf("LND_%v_MACAROON", nodeIdx))
	if macaroon == "" {
		return nil, fmt.Errorf("client: %v macaroon env var not found",
			nodeIdx)
	}

	return lndclient.NewLndServices(&lndclient.LndServicesConfig{
		LndAddress:         server,
		Network:            lndclient.NetworkRegtest,
		CustomMacaroonPath: macaroon,
		TLSPath:            cert,
	})
}
