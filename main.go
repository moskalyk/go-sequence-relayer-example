package main

import (
	"context"
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
	v1 "github.com/0xsequence/go-sequence/core/v1"
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

	eoa, _ := ethwallet.NewWalletFromPrivateKey("")

	w, _ := sequence.GenericNewWalletSingleOwner[*v1.WalletConfig](eoa, sequence.WalletContext{
		FactoryAddress:    common.HexToAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3"),
		MainModuleAddress: common.HexToAddress("0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"),
	})

	// address
	println("{}", w.Address().Hex())
	nonce, _ := w.GetNonce()

	// nonce
	println("{}", nonce)

	var functionAbi = `[{"type":"function","name":"mint","inputs":[{"type":"uint256","name":"amount"}]}]`

	parsedAbi, _ := abi.JSON(strings.NewReader(functionAbi))
	calldata, _ := parsedAbi.Pack("mint", 100)

	txs := &sequence.Transaction{
		To:            common.HexToAddress("0xdB59649AAD68e1E44911281748667a5F5b52fed2"),
		Data:          calldata,
		Value:         big.NewInt(0),
		GasLimit:      big.NewInt(190000),
		DelegateCall:  false,
		RevertOnError: false,
		Nonce:         nonce,
	}

	nodeURL := "https://nodes.sequence.app/arbitrum-nova" // Replace with your actual node URL

	p, _ := ethrpc.NewProvider(nodeURL)
	log := logger.NewLogger(logger.LogLevel_DEBUG)

	monitorOptions := ethmonitor.DefaultOptions
	monitorOptions.Logger = log
	monitorOptions.WithLogs = true
	monitorOptions.BlockRetentionLimit = 1000

	monitor, _ := ethmonitor.NewMonitor(p, monitorOptions)

	receipts, _ := ethreceipts.NewReceiptsListener(log, p, monitor)

	relayer, _ := relayer.NewRpcRelayer(p, receipts, nodeURL, http.DefaultClient)
	_ = w.SetRelayer(relayer)

	signed, _ := w.SignTransaction(context.Background(), txs)
	metaTxnID, _, _, err := w.SendTransaction(context.Background(), signed)

	println("{}", metaTxnID)
	println("{}", err)
}
