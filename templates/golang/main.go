package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	ctx := context.Background()
	_, err := getLndNodes(ctx)
	if err != nil {
		fmt.Printf("Could not set up connection: %v\n", err)
		os.Exit(1)
	}

}
