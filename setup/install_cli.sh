#!/bin/bash

# Grab bitcoin/lnd based on arch.
ARCH=$(dpkg --print-architecture)

if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    echo "Installing bitcoin / lnd for $ARCH"
    BITCOIN_URL="https://bitcoincore.org/bin/bitcoin-core-26.1/bitcoin-26.1-aarch64-linux-gnu.tar.gz"
    LND_URL="https://github.com/lightningnetwork/lnd/releases/download/v0.17.4-beta/lnd-linux-arm64-v0.17.4-beta.tar.gz"
elif [ "$ARCH" = "amd64" ]; then
    echo "Installing bitcoin / lnd for amd64"
    BITCOIN_URL="https://bitcoincore.org/bin/bitcoin-core-26.1/bitcoin-26.1-x86_64-linux-gnu.tar.gz"
    LND_URL="https://github.com/lightningnetwork/lnd/releases/download/v0.17.4-beta/lnd-linux-amd64-v0.17.4-beta.tar.gz"
else
    echo "Unsupported architecture $ARCH"
    exit 1
fi

# Bitcoin unzips without the arch suffix.
bitcoin_dir=bitcoin-26.1

# Download Bitcoin Core
wget "$BITCOIN_URL"
tar -xvf "$(basename "$BITCOIN_URL")"
mv "$bitcoin_dir/bin/bitcoin-cli" /bin

# Download and setup LND
wget "$LND_URL"
tar -xvf "$(basename "$LND_URL")"
mv "$(basename "$LND_URL" .tar.gz)/lncli" /bin

# Clean up downloaded files: both the unpacked dirs and the targz.
rm -rf "$bitcoin_dir" "$(basename "$BITCOIN_URL")" "$(basename "$LND_URL")"  "$(basename "$LND_URL" .tar.gz)"
