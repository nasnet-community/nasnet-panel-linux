// Package bscclient is a minimal BSC JSON-RPC client used to verify USDT
// (BEP20) transfers by transaction hash. It speaks only the two calls we need
// and parses the ERC20 Transfer log itself, so it carries no chain SDK weight.
package bscclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
)

// transferTopic is keccak256("Transfer(address,address,uint256)") — topic[0]
// of every ERC20/BEP20 Transfer event.
const transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// usdtDecimals is the decimal count for USDT on BSC (BEP20 uses 18, not 6).
const usdtDecimals = 18

// usdtDivisor is 10^18, precomputed once (the BEP20 USDT wei->token divisor).
var usdtDivisor = new(big.Float).SetPrec(128).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(usdtDecimals), nil))

// Client talks to a single BSC RPC endpoint.
type Client struct {
	rpcURL string
	http   *http.Client
}

// New builds a client against rpcURL using the given HTTP client.
func New(rpcURL string, hc *http.Client) *Client {
	return &Client{rpcURL: rpcURL, http: hc}
}

// TransferResult describes the USDT transfer to the expected address found in a
// transaction.
type TransferResult struct {
	Mined         bool    // the tx is mined (a receipt exists)
	TxSuccess     bool    // receipt status == 0x1
	Found         bool    // a USDT transfer to the expected address was present
	AmountUSDT    float64 // total transferred to the expected address, value / 1e18
	Confirmations uint64  // current head - tx block + 1
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var r rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("rpc error: %s", r.Error.Message)
	}
	return json.Unmarshal(r.Result, out)
}

type txReceipt struct {
	Status      string `json:"status"`
	BlockNumber string `json:"blockNumber"`
	Logs        []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	} `json:"logs"`
}

// GetUSDTTransfer looks up txHash and sums every USDT (contract) Transfer whose
// recipient is expectedTo, returning the total plus confirmation count. Found is
// false when the tx is mined but carries no transfer to expectedTo; Mined is
// false when the tx isn't mined yet. Address comparison is case- and
// 0x-prefix-insensitive.
func (c *Client) GetUSDTTransfer(ctx context.Context, txHash, contract, expectedTo string) (*TransferResult, error) {
	var receipt *txReceipt
	if err := c.call(ctx, "eth_getTransactionReceipt", []any{txHash}, &receipt); err != nil {
		return nil, err
	}
	if receipt == nil {
		// Null result: tx unknown or still pending (not yet mined).
		return &TransferResult{}, nil
	}

	res := &TransferResult{Mined: true, TxSuccess: receipt.Status == "0x1"}

	contract = strings.ToLower(contract)
	want := normalizeAddr(expectedTo)
	for _, lg := range receipt.Logs {
		if strings.ToLower(lg.Address) != contract || len(lg.Topics) < 3 || strings.ToLower(lg.Topics[0]) != transferTopic {
			continue
		}
		if topicAddress(lg.Topics[2]) != want {
			continue // a USDT transfer, but not to our address
		}
		res.Found = true
		res.AmountUSDT += weiToUSDT(lg.Data) // sum, in case the tx pays us in multiple logs
	}
	if !res.Found {
		return res, nil
	}

	head, err := c.blockNumber(ctx)
	if err != nil {
		return nil, err
	}
	txBlock := hexToUint64(receipt.BlockNumber)
	if head >= txBlock {
		res.Confirmations = head - txBlock + 1
	}
	return res, nil
}

// normalizeAddr lowercases an address and strips any 0x prefix for comparison.
func normalizeAddr(a string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "0x")
}

func (c *Client) blockNumber(ctx context.Context) (uint64, error) {
	var hexNum string
	if err := c.call(ctx, "eth_blockNumber", []any{}, &hexNum); err != nil {
		return 0, err
	}
	return hexToUint64(hexNum), nil
}

// topicAddress extracts the 20-byte address from a 32-byte (left-padded) topic.
func topicAddress(topic string) string {
	t := strings.TrimPrefix(strings.ToLower(topic), "0x")
	if len(t) < 40 {
		return t
	}
	return t[len(t)-40:]
}

// weiToUSDT converts a 32-byte hex value (the Transfer data field) to a USDT
// amount, dividing by 10^18.
func weiToUSDT(data string) float64 {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(strings.ToLower(data), "0x"), 16)
	if !ok {
		return 0
	}
	f, _ := new(big.Float).SetPrec(128).Quo(new(big.Float).SetInt(v), usdtDivisor).Float64()
	return f
}

func hexToUint64(h string) uint64 {
	v, _ := new(big.Int).SetString(strings.TrimPrefix(strings.ToLower(h), "0x"), 16)
	if v == nil {
		return 0
	}
	return v.Uint64()
}
