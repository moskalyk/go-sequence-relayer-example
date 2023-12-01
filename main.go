package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/0xsequence/ethkit/ethmonitor"
	"github.com/0xsequence/ethkit/ethreceipts"
	"github.com/0xsequence/ethkit/ethrpc"
	"github.com/0xsequence/ethkit/ethtxn"
	"github.com/0xsequence/ethkit/ethwallet"
	"github.com/0xsequence/ethkit/go-ethereum/common"
	"github.com/0xsequence/go-sequence"
	"github.com/0xsequence/go-sequence/core"
	v2 "github.com/0xsequence/go-sequence/core/v2"
	"github.com/0xsequence/go-sequence/relayer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/goware/logger"
)

type MetaTxnID string

type MetaTxnStatus uint8

type Transaction struct {
	DelegateCall  bool           `abi:"delegateCall"`  // Performs delegatecall
	RevertOnError bool           `abi:"revertOnError"` // Reverts transaction bundle if tx fails
	GasLimit      *big.Int       `abi:"gasLimit"`      // Maximum gas to be forwarded
	To            common.Address `abi:"target"`        // Address to send transaction, aka target
	Value         *big.Int       `abi:"value"`         // Amount of ETH to pass with the call
	Data          []byte         `abi:"data"`          // Calldata to pass

	Transactions Transactions // Child transactions
	Nonce        *big.Int     // Meta-Transaction nonce, with encoded space
	Signature    []byte       // Meta-Transaction signature

	encoded bool
}

type Transactions []*Transaction

type SignedTransactions struct {
	ChainID       *big.Int
	WalletConfig  core.WalletConfig
	WalletContext sequence.WalletContext

	Transactions Transactions // The meta-transactions
	Nonce        *big.Int     // Nonce of the transactions
	Digest       common.Hash  // Digest of the transactions
	Signature    []byte       // Signature (encoded as bytes from *Signature) of the txn digest
}

type Relayer interface {
	// ..
	GetProvider() *ethrpc.Provider

	// ..
	EstimateGasLimits(ctx context.Context, walletConfig core.WalletConfig, walletContext sequence.WalletContext, txns Transactions) (Transactions, error)

	// NOTE: nonce space is 160 bits wide
	GetNonce(ctx context.Context, walletConfig core.WalletConfig, walletContext sequence.WalletContext, space *big.Int, blockNum *big.Int) (*big.Int, error)

	// Relay will submit the Sequence signed meta transaction to the relayer. The method will block until the relayer
	// responds with the native transaction hash (*types.Transaction), which means the relayer has submitted the transaction
	// request to the network. Clients can use WaitReceipt to wait until the metaTxnID has been mined.
	Relay(ctx context.Context, signedTxs *SignedTransactions) (MetaTxnID, *types.Transaction, ethtxn.WaitReceipt, error)

	// ..
	Wait(ctx context.Context, metaTxnID MetaTxnID, optTimeout ...time.Duration) (MetaTxnStatus, *types.Receipt, error)
}

func main() {

	// eoa, _ := ethwallet.NewWalletFromRandomEntropy()
	eoa, _ := ethwallet.NewWalletFromPrivateKey("")

	w, err := sequence.GenericNewWalletSingleOwner[*v2.WalletConfig](eoa, sequence.V2SequenceContext())
	if err != nil {
		log.Fatal(err)
	}

	// set provider
	nodeURL := "https://nodes.sequence.app/arbitrum-nova" // Replace with your actual node URL
	p, err := ethrpc.NewProvider(nodeURL)
	if err != nil {
		log.Fatalf("failed to create new provider: %v", err)
	}
	w.SetProvider(p)

	// address
	fmt.Println("relayer wallet address:", w.Address().Hex())

	log := logger.NewLogger(logger.LogLevel_DEBUG)

	// Receipts listener
	monitorOptions := ethmonitor.DefaultOptions
	monitorOptions.Logger = log
	monitorOptions.WithLogs = true
	monitorOptions.BlockRetentionLimit = 400
	monitorOptions.StartBlockNumber = big.NewInt(-20)

	monitor, err := ethmonitor.NewMonitor(p, monitorOptions)
	if err != nil {
		log.Fatalf("monitor create failed: %v", err)
	}

	receiptsListener, err := ethreceipts.NewReceiptsListener(log, p, monitor)
	if err != nil {
		log.Fatalf("receiptlistener create failed: %v", err)
	}

	// Setup Relayer on the wallet
	relayerURL := "https://arbitrum-nova-relayer.sequence.app"
	relayer, err := relayer.NewRpcRelayer(p, receiptsListener, relayerURL, http.DefaultClient)
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
	nonce, _ := w.GetNonce()
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
