package cmd

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"gitlab.com/accumulatenetwork/accumulate/internal/api/v2"
	"gitlab.com/accumulatenetwork/accumulate/internal/api/v2/query"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

const minorBlockApiTimeout = 2 * time.Minute
const majorBlockApiTimeout = 30 * time.Second

var blocksCmd = &cobra.Command{
	Use:   "blocks",
	Short: "Create and get blocks",
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		var txFetchMode query.TxFetchMode
		var blockFilterMode query.BlockFilterMode

		if len(args) > 0 {
			switch arg := args[0]; arg {
			case "minor":
				if len(args) > 3 {
					txFetchMode, err = parseFetchMode(args)
					if err != nil {
						printError(cmd, err)
						return
					}
					blockFilterMode, err = parseBlockFilterMode(args)
					if err != nil {
						printError(cmd, err)
						return
					}
					err = GetMinorBlocks(cmd, args[1], args[2], args[3], txFetchMode, blockFilterMode)
					if err != nil {
						printError(cmd, err)
						return
					}
				} else {
					fmt.Println("Usage:")
					PrintGetMinorBlocks()
				}
			case "major":
				if len(args) > 3 {
					err = GetMajorBlocks(cmd, args[1], args[2], args[3])
					if err != nil {
						printError(cmd, err)
						return
					}
				} else {
					fmt.Println("Usage:")
					PrintGetMajorBlocks()
				}
			default:
				fmt.Println("Usage:")
				PrintBlocks()
			}
		} else {
			fmt.Println("Usage:")
			PrintBlocks()
		}
	},
}

func parseFetchMode(args []string) (query.TxFetchMode, error) {
	if len(args) > 4 {
		txFetchMode, ok := query.TxFetchModeByName(args[4])
		if ok {
			return txFetchMode, nil
		} else {
			return query.TxFetchModeOmit, fmt.Errorf("%s is not a valid fetch mode. Use expand|ids|countOnly|omit", args[4])
		}
	}
	return query.TxFetchModeExpand, nil
}

func parseBlockFilterMode(args []string) (query.BlockFilterMode, error) {
	if len(args) > 5 {
		blockFilterMode, ok := query.BlockFilterModeByName(args[5])
		if ok {
			return blockFilterMode, nil
		} else {
			return query.BlockFilterModeExcludeNone, fmt.Errorf("%s is not a block filter mode. Use excludenone|excludeempty", args[4])
		}
	}
	return query.BlockFilterModeExcludeNone, nil
}

var (
	BlocksWait      time.Duration
	BlocksNoWait    bool
	BlocksWaitSynth time.Duration
)

func init() {
	blocksCmd.Flags().DurationVarP(&BlocksWait, "wait", "w", 0, "Wait for the transaction to complete")
	blocksCmd.Flags().DurationVar(&BlocksWaitSynth, "wait-synth", 0, "Wait for synthetic transactions to complete")
}

func PrintGetMinorBlocks() {
	fmt.Println("  accumulate blocks minor [partition-url] [start index] [count] [tx fetch mode expand|ids|countOnly|omit (optional)] [block filter mode excludenone|excludeempty (optional)] Get minor blocks")
}

func PrintGetMajorBlocks() {
	fmt.Println("  accumulate blocks major [partition-url] [start index] [count] Get major blocks")
}

func PrintBlocks() {
	PrintGetMinorBlocks()
	PrintGetMajorBlocks()
}

func GetMinorBlocks(cmd *cobra.Command, accountUrl string, s string, e string, txFetchMode query.TxFetchMode, blockFilterMode query.BlockFilterMode) error {
	start, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	end, err := strconv.Atoi(e)
	if err != nil {
		return err
	}

	u, err := url.Parse(accountUrl)
	if err != nil {
		return err
	}

	params := new(api.MinorBlocksQuery)
	params.UrlQuery.Url = u
	params.QueryPagination.Start = uint64(start)
	params.QueryPagination.Count = uint64(end)
	params.TxFetchMode = txFetchMode
	params.BlockFilterMode = blockFilterMode

	// Temporary increase timeout, we may get a large result set which takes a while to construct
	globalTimeout := Client.Timeout
	Client.Timeout = minorBlockApiTimeout
	defer func() {
		Client.Timeout = globalTimeout
	}()

	res, err := Client.QueryMinorBlocks(context.Background(), params)
	if err != nil {
		rpcError, err := PrintJsonRpcError(err)
		cmd.Println(rpcError)
		return err
	}

	out, err := PrintMultiResponse(res)
	if err != nil {
		return err
	}

	printOutput(cmd, out, nil)
	return nil
}

func GetMajorBlocks(cmd *cobra.Command, accountUrl string, s string, e string) error {
	start, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	end, err := strconv.Atoi(e)
	if err != nil {
		return err
	}

	u, err := url.Parse(accountUrl)
	if err != nil {
		return err
	}

	params := new(api.MajorBlocksQuery)
	params.UrlQuery.Url = u
	params.QueryPagination.Start = uint64(start)
	params.QueryPagination.Count = uint64(end)

	// Temporary increase timeout, we may get a large result set which takes a while to construct
	globalTimeout := Client.Timeout
	Client.Timeout = majorBlockApiTimeout
	defer func() {
		Client.Timeout = globalTimeout
	}()

	res, err := Client.QueryMajorBlocks(context.Background(), params)
	if err != nil {
		rpcError, err := PrintJsonRpcError(err)
		cmd.Println(rpcError)
		return err
	}

	out, err := PrintMultiResponse(res)
	if err != nil {
		return err
	}

	printOutput(cmd, out, nil)
	return nil
}
