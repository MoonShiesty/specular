#!/bin/bash
SBIN=`dirname $0`
SBIN="`cd "$SBIN"; pwd`"
echo $SBIN
. $SBIN/configure.sh

cd $CONTRACTS_DIR && npx hardhat compile
