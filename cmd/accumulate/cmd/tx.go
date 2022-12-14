package cmd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/AccumulateNetwork/jsonrpc2/v15"
	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"gitlab.com/accumulatenetwork/accumulate/internal/api/v2"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
	"golang.org/x/sync/errgroup"
)

var txCmd = &cobra.Command{
	Use:   "tx",
	Short: "Create and get token txs",
	Run: func(cmd *cobra.Command, args []string) {
		var out string
		var err error
		if len(args) > 0 {
			switch arg := args[0]; arg {
			case "submit":
				if len(args) > 2 {
					out, err = submit(args[1:])
				} else {
					fmt.Println("Usage:")
					PrintTXSubmit()
				}

			case "get":
				if len(args) > 1 {
					out, err = GetTX(args[1])
				} else {
					fmt.Println("Usage:")
					PrintTXGet()
				}
			case "pending":
				if len(args) == 1 {
					out, err = GetPendingTx(args[1], []string{})
				} else {
					out, err = GetPendingTx(args[1], args[2:])
				}
			case "history":
				if len(args) > 3 {
					out, err = GetTXHistory(args[1], args[2], args[3])
				} else {
					fmt.Println("Usage:")
					PrintTXHistoryGet()
				}
			case "create":
				if len(args) > 3 {
					out, err = CreateTX(args[1], args[2:])
				} else {
					fmt.Println("Usage:")
					PrintTXCreate()
				}
			case "execute":
				if len(args) > 2 {
					out, err = ExecuteTX(args[1], args[2:])
				} else {
					fmt.Println("Usage:")
					PrintTXExecute()
				}
			case "sign":
				if len(args) > 2 {
					out, err = SignTX(args[1], args[2:])
				} else {
					fmt.Println("Usage:")
					PrintTxSign()
				}
			default:
				fmt.Println("Usage:")
				PrintTX()
			}
		} else {
			fmt.Println("Usage:")
			PrintTX()
		}
		printOutput(cmd, out, err)
	},
}

var (
	TxWait          time.Duration
	TxNoWait        bool
	TxWaitSynth     time.Duration
	TxIgnorePending bool
)

func init() {
	txCmd.Flags().DurationVarP(&TxWait, "wait", "w", 0, "Wait for the transaction to complete")
	txCmd.Flags().DurationVar(&TxWaitSynth, "wait-synth", 0, "Wait for synthetic transactions to complete")
	txCmd.Flags().BoolVar(&Scratch, "scratch", false, "Read from the scratch chain")
}

func PrintTXGet() {
	fmt.Println("  accumulate tx get [txid | tx url]	Get token transaction by txid")
}

func PrintTXPendingGet() {
	fmt.Println("  accumulate tx pending [txid]			Get token transaction by txid")
	fmt.Println("  accumulate tx pending [height]			Get token transaction by block height")
	fmt.Println("  accumulate tx pending [starting transaction number]	[ending transaction number]		Get token transaction by beginning and ending height")
}

func PrintTXCreate() {
	fmt.Println("  accumulate tx create [token account url] [signing key ] [to] [amount]	Create new token tx")
	fmt.Println("  accumulate tx create [lite token account url] [to] [amount]	Create new token tx")
}

func PrintTXExecute() {
	fmt.Println("  accumulate tx execute [origin url] [signing key name] [payload]	Sign and execute an arbitrary transaction")
	fmt.Println("  accumulate tx execute [lite account url] [payload]	Sign and execute an arbitrary transaction")
}

func PrintTxSign() {
	fmt.Println("  accumulate tx sign [origin url] [key name[@key book or page]] [txid]	Sign a pending transaction")
}

func PrintTXHistoryGet() {
	fmt.Println("  accumulate tx history [url] [starting transaction number] [ending transaction number] --scratch (optional)	Get transaction history")
}

func PrintTXSubmit() {
	fmt.Println("  accumulate tx submit [transaction] [[signature 0] ...] Submit an arbitrary transaction + signature(s)")
}

func PrintTX() {
	PrintTXGet()
	PrintTXCreate()
	PrintTxSign()
	PrintTXExecute()
	PrintTXSubmit()
	PrintTXHistoryGet()
	PrintTXPendingGet()
}
func GetPendingTx(origin string, args []string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	//<record>#pending/<hash> - fetch an envelope by hash
	//<record>#pending/<index> - fetch an envelope by index/height
	//<record>#pending/<start>:<end> - fetch a range of envelope by index/height
	//build the fragments:
	params := api.UrlQuery{}

	var out string
	var perr error
	switch len(args) {
	case 0:
		//query with no parameters
		u = u.WithFragment("pending")
		params.Url = u
		res := api.MultiResponse{}
		err = queryAs("query", &params, &res)
		if err != nil {
			return "", err
		}
		out, perr = PrintMultiResponse(&res)
	case 1:
		if len(args[0]) == 64 {
			//this looks like a transaction hash, now check it
			txid, err := hex.DecodeString(args[0])
			if err != nil {
				return "", fmt.Errorf("cannot decode transaction id")
			}
			u = u.WithFragment(fmt.Sprintf("pending/%x", txid))
		} else {
			height, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return "", fmt.Errorf("expecting height, but could not convert argument, %v", err)
			}
			u = u.WithFragment(fmt.Sprintf("pending/%d", height))
		}
		params.Url = u
		res := api.TransactionQueryResponse{}
		err = queryAs("query", &params, &res)
		if err != nil {
			return "", err
		}
		out, perr = PrintTransactionQueryResponseV2(&res)
	case 2:
		//pagination
		start, err := strconv.Atoi(args[0])
		if err != nil {
			return "", fmt.Errorf("error converting start index %v", err)
		}
		count, err := strconv.Atoi(args[1])
		if err != nil {
			return "", fmt.Errorf("error converting count %v", err)
		}
		u = u.WithFragment(fmt.Sprintf("pending/%d:%d", start, count))
		params.Url = u
		res := api.MultiResponse{}
		err = queryAs("query", &params, &res)
		if err != nil {
			return "", err
		}
		out, perr = PrintMultiResponse(&res)
	default:
		return "", fmt.Errorf("invalid number of arguments")
	}

	return out, perr
}

func getTxUsingUrl(txID *url.TxID, wait time.Duration, ignorePending bool) (*api.TransactionQueryResponse, error) {
	params := new(api.TxnQuery)
	params.TxIdUrl = txID
	params.Prove = Prove
	params.IgnorePending = ignorePending

	return getTX(wait, params)
}

func getTxUsingHash(hash []byte, wait time.Duration, ignorePending bool) (*api.TransactionQueryResponse, error) {
	params := new(api.TxnQuery)
	params.Txid = hash
	params.Prove = Prove
	params.IgnorePending = ignorePending

	return getTX(wait, params)
}

func getTX(wait time.Duration, params *api.TxnQuery) (*api.TransactionQueryResponse, error) {
	var res api.TransactionQueryResponse

	if wait > 0 {
		params.Wait = wait

		t := Client.Timeout
		defer func() { Client.Timeout = t }()

		Client.Timeout = TxWait * 2
	}

	data, err := json.Marshal(params)
	jsondata := json.RawMessage(data)
	if err != nil {
		return nil, err
	}

	err = Client.RequestAPIv2(context.Background(), "query-tx", jsondata, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func GetTX(hashOrUrl string) (string, error) {
	var res *api.TransactionQueryResponse
	txID, err := url.ParseTxID(hashOrUrl)
	if err == nil {
		res, err = getTxUsingUrl(txID, TxWait, TxIgnorePending)
	} else {
		if len(hashOrUrl) == 64 {
			var hash []byte
			hash, err = hex.DecodeString(hashOrUrl)
			if err != nil {
				return "", err
			}
			res, err = getTxUsingHash(hash, TxWait, TxIgnorePending)
		} else {
			return "", fmt.Errorf("transaction ID could not be parsed from the txID URL %s, reason: %v", hashOrUrl, err)
		}
	}
	if err != nil {
		var rpcErr jsonrpc2.Error
		if errors.As(err, &rpcErr) {
			return PrintJsonRpcError(err)
		}
		return "", err
	}

	out, err := PrintTransactionQueryResponseV2(res)
	if err != nil {
		return "", err
	}

	if TxWaitSynth == 0 || len(res.Produced) == 0 {
		return out, nil
	}

	if TxWaitSynth > 0 {
		Client.Timeout = TxWaitSynth * 2
	}

	errg := new(errgroup.Group)
	for _, txid := range res.Produced {
		lclTxId := txid
		errg.Go(func() error {
			res, err := getTxUsingUrl(lclTxId, TxWaitSynth, true)
			if err != nil {
				return err
			}

			o, err := PrintTransactionQueryResponseV2(res)
			if err != nil {
				return err
			}

			if WantJsonOutput {
				out += "\n"
			}
			out += o
			return nil
		})
	}
	err = errg.Wait()
	if err != nil {
		var rpcErr jsonrpc2.Error
		if errors.As(err, &rpcErr) {
			return PrintJsonRpcError(err)
		}
		return PrintJsonRpcError(err)
	}

	return out, nil
}

func GetTXHistory(accountUrl string, startArg string, endArg string) (string, error) {
	var res api.MultiResponse
	start, err := strconv.Atoi(startArg)
	if err != nil {
		return "", err
	}
	end, err := strconv.Atoi(endArg)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(accountUrl)
	if err != nil {
		return "", err
	}

	params := new(api.TxHistoryQuery)
	params.UrlQuery.Url = u
	params.QueryPagination.Start = uint64(start)
	params.QueryPagination.Count = uint64(end)
	params.Scratch = Scratch

	data, err := json.Marshal(params)
	if err != nil {
		return "", err
	}

	if err := Client.RequestAPIv2(context.Background(), "query-tx-history", json.RawMessage(data), &res); err != nil {
		return PrintJsonRpcError(err)
	}

	return PrintMultiResponse(&res)
}

func CreateTX(sender string, args []string) (string, error) {
	//sender string, receiver string, amount string
	u, err := url.Parse(sender)
	if err != nil {
		return "", err
	}

	args, signer, err := prepareSigner(u, args)
	if err != nil {
		return "", err
	}

	if len(args) < 2 {
		return "", fmt.Errorf("unable to prepare signer, %v", err)
	}

	tokenUrl, err := GetTokenUrlFromAccount(u)
	if err != nil {
		return "", fmt.Errorf("invalid token url was obtained from %s, %v", u.String(), err)
	}

	u2, err := url.Parse(args[0])
	if err != nil {
		return "", fmt.Errorf("invalid receiver url %s, %v", args[0], err)
	}

	amount := args[1]
	send := new(protocol.SendTokens)

	amt, err := amountToBigInt(tokenUrl.String(), amount)
	if err != nil {
		return "", err
	}

	send.AddRecipient(u2, amt)

	return dispatchTxAndPrintResponse(send, u, signer)
}

func waitForTxnUsingHash(hash []byte, wait time.Duration, ignorePending bool) ([]*api.TransactionQueryResponse, error) {
	var queryResponses []*api.TransactionQueryResponse
	queryRes, err := getTxUsingHash(hash, wait, ignorePending)
	if err != nil {
		return nil, err
	}
	return waitForTxn(queryResponses, queryRes, wait)
}

func waitForTxn(queryResponses []*api.TransactionQueryResponse, queryRes *api.TransactionQueryResponse, wait time.Duration) ([]*api.TransactionQueryResponse, error) {
	queryResponses = append(queryResponses, queryRes)
	if queryRes.Produced != nil {
		for _, txid := range queryRes.Produced {
			txhash := txid.Hash()
			resp, err := waitForTxnUsingHash(txhash[:], wait, true)
			if err != nil {
				return nil, err
			}
			queryResponses = append(queryResponses, resp...)
		}
	}
	return queryResponses, nil
}

func ExecuteTX(sender string, args []string) (string, error) {
	//sender string, receiver string, amount string
	u, err := url.Parse(sender)
	if err != nil {
		return "", err
	}

	args, signer, err := prepareSigner(u, args)
	if err != nil {
		return "", fmt.Errorf("unable to prepare signer, %v", err)
	}

	var typ struct {
		Type protocol.TransactionType
	}
	err = yaml.Unmarshal([]byte(args[0]), &typ)
	if err != nil {
		return "", fmt.Errorf("invalid payload 1: %v", err)
	}

	txn, err := protocol.NewTransactionBody(typ.Type)
	if err != nil {
		return "", fmt.Errorf("invalid payload 2: %v", err)
	}

	err = yaml.Unmarshal([]byte(args[0]), txn)
	if err != nil {
		return "", fmt.Errorf("invalid payload 3: %v", err)
	}

	return dispatchTxAndPrintResponse(txn, u, signer)
}

func SignTX(sender string, args []string) (string, error) {
	//sender string, receiver string, amount string
	u, err := url.Parse(sender)
	if err != nil {
		return "", err
	}

	args, signer, err := prepareSigner(u, args)
	if err != nil {
		return "", fmt.Errorf("unable to prepare signer, %v", err)
	}
	if len(args) != 1 {
		PrintTxSign()
		return "", nil
	}

	txHash, err := hex.DecodeString(args[0])
	if err != nil {
		return "", fmt.Errorf("unable to parse transaction hash: %v", err)
	}

	return dispatchTxAndPrintResponse(txHash, u, signer)
}

func submit(args []string) (string, error) {
	txn := new(protocol.Transaction)
	err := txn.UnmarshalJSON([]byte(args[0]))
	if err != nil {
		return "", err
	}

	var sigs []protocol.Signature
	for _, arg := range args[1:] {
		sig, err := protocol.UnmarshalSignatureJSON([]byte(arg))
		if err != nil {
			return "", err
		}
		sigs = append(sigs, sig)
	}

	req := new(api.ExecuteRequest)
	req.Envelope = &protocol.Envelope{Transaction: []*protocol.Transaction{txn}, Signatures: sigs}
	if TxPretend {
		req.CheckOnly = true
	}

	res, err := Client.ExecuteDirect(context.Background(), req)
	if err != nil {
		return PrintJsonRpcError(err)
	}
	if res.Code != 0 {
		result := new(protocol.TransactionStatus)
		if Remarshal(res.Result, result) != nil {
			return "", errors.New(errors.StatusEncodingError, res.Message)
		}
		return "", result.Error
	}

	var resps []*api.TransactionQueryResponse
	if TxWait != 0 {
		resps, err = waitForTxnUsingHash(res.TransactionHash, TxWait, TxIgnorePending)
		if err != nil {
			return PrintJsonRpcError(err)
		}
	}

	result, err := ActionResponseFrom(res).Print()
	if err != nil {
		return "", err
	}
	if res.Code == 0 {
		for _, response := range resps {
			str, err := PrintTransactionQueryResponseV2(response)
			if err != nil {
				return PrintJsonRpcError(err)
			}
			result = fmt.Sprint(result, str, "\n")
		}
	}
	return result, nil
}
