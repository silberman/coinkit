#!/bin/bash

# This script is the entry point for the Docker container, designed to be run on
# the Google cloud platform from the coinkit directory.

KEYPAIR=`find /secrets/keypair | grep json | head -1`
echo loading keypair: $KEYPAIR

cserver \
    --keypair=$KEYPAIR \
    --network=./testnet/network.json \
    --logtostdout \
    --http=8000

