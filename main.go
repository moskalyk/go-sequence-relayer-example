package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"

	"github.com/0xsequence/ethkit/ethrpc"
	"github.com/0xsequence/ethkit/ethwallet"
	"github.com/0xsequence/ethkit/go-ethereum/common"
	"github.com/0xsequence/go-sequence"
	v2 "github.com/0xsequence/go-sequence/core/v2"
	"github.com/0xsequence/go-sequence/relayer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/goware/logger"
)

func main() {

	// Signer
	eoa, err := ethwallet.NewWalletFromPrivateKey("private-key-goes-here")
	if err != nil {
		log.Fatalf("created wallet failed: %v", err)
	}

	w, err := sequence.GenericNewWalletSingleOwner[*v2.WalletConfig](eoa, sequence.V2SequenceContext())
	if err != nil {
		log.Fatal(err)
	}

	// Setup provider
	nodeURL := "https://nodes.sequence.app/arbitrum-nova" // Replace with your actual node URL
	p, err := ethrpc.NewProvider(nodeURL)
	if err != nil {
		log.Fatalf("failed to create new provider: %v", err)
	}
	w.SetProvider(p)

	// address
	fmt.Println("relayer wallet address:", w.Address().Hex())

	log := logger.NewLogger(logger.LogLevel_DEBUG)

	// Setup Relayer on the wallet
	relayerURL := "https://arbitrum-nova-relayer.sequence.app"
	relayer, err := relayer.NewRpcRelayer(p, nil, relayerURL, http.DefaultClient)
	if err != nil {
		log.Fatalf("failed to create new rpc relayer client: %v", err)
	}

	err = w.SetRelayer(relayer)
	if err != nil {
		log.Fatalf("failed to set relayer: %v", err)
	}

	// wallet must be deployed first..
	isDeployed, err := w.IsDeployed()
	if err != nil {
		log.Fatalf("is deployed call failed: %v", err)
	}
	fmt.Println("is deployed?", isDeployed)

	// TODO/NOTE: I'm pretty sure we have to deploy this wallet first...?
	if !isDeployed {
		_, _, waitReceipt, err := w.Deploy(context.Background())
		if err != nil {
			log.Fatal("failed to deploy wallet: %v", err)
		}
		fmt.Println("waiting for wallet deployment txn..")
		receipt, err := waitReceipt(context.Background())
		if err != nil {
			log.Fatal("failed to get deploy wallet receipt: %v", err)
		}
		fmt.Println("wallet deployed, txn hash:", receipt.TxHash)
	}

	// The actual transactiom
	var functionAbi = `[{"type":"function","name":"mint","inputs":[{"type":"uint256","name":"amount"}]}]`
	parsedAbi, _ := abi.JSON(strings.NewReader(functionAbi))
	calldata, _ := parsedAbi.Pack("mint", 100)

	// nonce
	//
	// we can either get the next nonce, or generate a random one. By generating
	// a random one, you're able to send transactions in parallel.
	// nonce, _ := w.GetNonce()
	nonce, _ := sequence.GenerateRandomNonce()
	fmt.Println("nonce:", nonce)

	// txn
	txs := &sequence.Transaction{
		To:            common.HexToAddress("0xdB59649AAD68e1E44911281748667a5F5b52fed2"),
		Data:          calldata,
		Value:         big.NewInt(0),
		GasLimit:      big.NewInt(190000),
		DelegateCall:  false,
		RevertOnError: false,
		Nonce:         nonce,
	}

	// Sign the transaction
	signed, err := w.SignTransaction(context.Background(), txs)
	if err != nil {
		log.Fatalf("failed to sign transaction: %v", err)
	}

	// Send the transaction
	metaTxnID, _, waitReceipt, err := w.SendTransaction(context.Background(), signed)
	if err != nil {
		log.Fatal("failed to send transaction: %v", err)
	}
	fmt.Println("sent sequence metaTxnID:", metaTxnID)

	// Wait for txn to be mined + get receipt
	fmt.Println("waiting for the txn to be mined and get the receipt...")

	receipt, err := waitReceipt(context.Background())
	if err != nil {
		log.Fatalf("failed to wait for receipt: %v", err)
	}

	fmt.Println("got the txn receipt!", receipt.TxHash.String())
}
