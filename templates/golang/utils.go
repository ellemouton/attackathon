package main

import (
	"crypto/rand"

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

        preimage, err:= lntypes.MakePreimage(randomBytes)
        if err!=nil{
                panic(err)
        }

        return preimage
}
