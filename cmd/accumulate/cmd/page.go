package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/AccumulateNetwork/jsonrpc2/v15"
	"github.com/spf13/cobra"
	url2 "gitlab.com/accumulatenetwork/accumulate/internal/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

func init() {
	pageCmd.AddCommand(
		pageGetCmd,
		pageCreateCmd,
		pageKeyCmd,
		// pageSetThresholdCmd,
		pageLockCmd,
		pageUnlockCmd)

	pageKeyCmd.AddCommand(
		pageKeyAddCmd,
		pageKeyUpdateCmd,
		pageKeyRemoveCmd)
}

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Create and manage Keys, Books, and Pages",
}

var pageGetCmd = &cobra.Command{
	Use:   "get [url]",
	Short: "Get existing Key Page by URL",
	Args:  cobra.ExactArgs(1),
	Run: runCmdFunc(func(args []string) (string, error) {
		return GetAndPrintKeyPage(args[0])
	}),
}

var pageCreateCmd = &cobra.Command{
	Use:   "create [origin key book url] [signing key name] [key index (optional)] [key height (optional)] [public key 1] ... [public key hex or name n + 1]",
	Short: "Create a key page",
	Args:  cobra.MinimumNArgs(3),
	Run: runCmdFunc(func(args []string) (string, error) {
		return CreateKeyPage(args[0], args[1:])
	}),
}

var pageKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Add, update, or remove keys from a key page",
}

var pageKeyAddCmd = &cobra.Command{
	Use:   "add [key page url] [signing key name] [key index (optional)] [key height (optional)] [new key name]",
	Short: "Add a key to a key page",
	Args:  cobra.RangeArgs(3, 5),
	Run: runCmdFunc(func(args []string) (string, error) {
		return KeyPageUpdate(args[0], protocol.KeyPageOperationTypeAdd, args[1:])
	}),
}

var pageKeyRemoveCmd = &cobra.Command{
	Use:   "remove [key page url] [signing key name] [key index (optional)] [key height (optional)] [old key name]",
	Short: "Remove a key from a key page",
	Args:  cobra.RangeArgs(3, 5),
	Run: runCmdFunc(func(args []string) (string, error) {
		return KeyPageUpdate(args[0], protocol.KeyPageOperationTypeRemove, args[1:])
	}),
}

var pageKeyUpdateCmd = &cobra.Command{
	Use:   "update [key page url] [signing key name] [key index (optional)] [key height (optional)] [old key name] [new public key or name]",
	Short: "Update a key on a key page",
	Args:  cobra.RangeArgs(4, 6),
	Run: runCmdFunc(func(args []string) (string, error) {
		return KeyPageUpdate(args[0], protocol.KeyPageOperationTypeUpdate, args[1:])
	}),
}

var pageSetThresholdCmd = &cobra.Command{
	Use:   "set-threshold [key page url] [signing key name] [key index (optional)] [key height (optional)] [threshold]",
	Short: "Set the M-of-N signature threshold for a key page",
	Args:  cobra.RangeArgs(3, 5),
	Run:   runCmdFunc(setKeyPageThreshold),
}
var _ = pageSetThresholdCmd // remove dead code removal

var pageLockCmd = &cobra.Command{
	Use:   "lock [key page url] [signing key name] [key index (optional)] [key height (optional)]",
	Short: "Lock a key page",
	Args:  cobra.RangeArgs(2, 4),
	Run:   runCmdFunc(lockKeyPage),
}

var pageUnlockCmd = &cobra.Command{
	Use:   "unlock [key page url] [signing key name] [key index (optional)] [key height (optional)]",
	Short: "Unlock a key page",
	Args:  cobra.RangeArgs(2, 4),
	Run:   runCmdFunc(unlockKeyPage),
}

// func keyPageExamples() {
// 	fmt.Println("\t\t example usage: accumulate key page create acc://RedWagon/RedBook redKey5 redKey1 redKey2 redKey3")
// 	fmt.Println("\t\t example usage: accumulate page key update acc://RedWagon/RedBook/1 redKey1 redKey2 redKey3")
// 	fmt.Println("\t\t example usage: accumulate page key add acc://RedWagon/RedBook/2 redKey2 redKey1")
// 	fmt.Println("\t\t example usage: accumulate page key remove acc://RedWagon/RedBook/1 redKey1 redKey2")
// }

func GetAndPrintKeyPage(url string) (string, error) {
	res, _, err := GetKeyPage(url)
	if err != nil {
		return "", fmt.Errorf("error retrieving key page for %s, %v", url, err)
	}

	return PrintChainQueryResponseV2(res)
}

func GetKeyPage(url string) (*QueryResponse, *protocol.KeyPage, error) {
	res, err := GetUrl(url)
	if err != nil {
		return nil, nil, err
	}

	if res.Type != protocol.AccountTypeKeyPage.String() {
		return nil, nil, fmt.Errorf("expecting key page but received %v", res.Type)
	}

	kp := protocol.KeyPage{}
	err = Remarshal(res.Data, &kp)
	if err != nil {
		return nil, nil, err
	}
	return res, &kp, nil
}

// CreateKeyPage create a new key page
func CreateKeyPage(bookUrlStr string, args []string) (string, error) {
	bookUrl, err := url2.Parse(bookUrlStr)
	if err != nil {
		return "", err
	}

	args, signer, err := prepareSigner(bookUrl, args)
	if err != nil {
		return "", err
	}

	if len(args) < 1 {
		return "", fmt.Errorf("invalid number of arguments")
	}
	keyLabels := args

	ckp := protocol.CreateKeyPage{}
	ksp := make([]*protocol.KeySpecParams, len(keyLabels))
	ckp.Keys = ksp
	for i := range keyLabels {
		ksp := protocol.KeySpecParams{}

		pk, err := LookupByLabel(keyLabels[i])
		if err != nil {
			//now check to see if it is a valid key hex, if so we can assume that is the public key.
			ksp.PublicKey, err = pubKeyFromString(keyLabels[i])
			if err != nil {
				return "", fmt.Errorf("key name %s, does not exist in wallet, nor is it a valid public key", keyLabels[i])
			}
		} else {
			ksp.PublicKey = pk[32:]
		}

		ckp.Keys[i] = &ksp
	}

	res, err := dispatchTxRequest("create-key-page", &ckp, nil, bookUrl, signer)
	if err != nil {
		return "", err
	}

	if !TxNoWait && TxWait > 0 {
		_, err := waitForTxn(res.TransactionHash, TxWait)
		if err != nil {
			var rpcErr jsonrpc2.Error
			if errors.As(err, &rpcErr) {
				return PrintJsonRpcError(err)
			}
			return "", err
		}
	}
	return ActionResponseFrom(res).Print()

}

func KeyPageUpdate(origin string, op protocol.KeyPageOperationType, args []string) (string, error) {
	u, err := url2.Parse(origin)
	if err != nil {
		return "", err
	}

	args, signer, err := prepareSigner(u, args)
	if err != nil {
		return "", err
	}

	var newKey []byte
	var oldKey []byte

	ukp := protocol.UpdateKeyPage{}

	switch op {
	case protocol.KeyPageOperationTypeUpdate:
		if len(args) < 2 {
			return "", fmt.Errorf("invalid number of arguments")
		}
		oldKey, err = resolvePublicKey(args[0])
		if err != nil {
			return "", err
		}
		newKey, err = resolvePublicKey(args[1])
		if err != nil {
			return "", err
		}
		ukp.Operation = &protocol.UpdateKeyOperation{
			OldEntry: protocol.KeySpecParams{PublicKey: oldKey},
			NewEntry: protocol.KeySpecParams{PublicKey: newKey},
		}
	case protocol.KeyPageOperationTypeAdd:
		if len(args) < 1 {
			return "", fmt.Errorf("invalid number of arguments")
		}
		newKey, err = resolvePublicKey(args[0])
		if err != nil {
			return "", err
		}
		ukp.Operation = &protocol.AddKeyOperation{
			Entry: protocol.KeySpecParams{PublicKey: newKey},
		}
	case protocol.KeyPageOperationTypeRemove:
		if len(args) < 1 {
			return "", fmt.Errorf("invalid number of arguments")
		}
		oldKey, err = resolvePublicKey(args[0])
		if err != nil {
			return "", err
		}
		ukp.Operation = &protocol.RemoveKeyOperation{
			Entry: protocol.KeySpecParams{PublicKey: oldKey},
		}
	}

	res, err := dispatchTxRequest("update-key-page", &ukp, nil, u, signer)
	if err != nil {
		return "", err
	}

	return ActionResponseFrom(res).Print()
}

func setKeyPageThreshold(args []string) (string, error) {
	// TODO If the user passes "key/book/1 name-of-key 2", the last value is
	// interpreted as a key page index or height, so `args` ends up empty
	args, principal, signer, err := parseArgsAndPrepareSigner(args)
	if err != nil {
		return "", err
	}

	value, err := strconv.ParseUint(args[0], 10, 16)
	if err != nil {
		return "", fmt.Errorf("invalid threshold: %v", err)
	}

	op := new(protocol.SetThresholdKeyPageOperation)
	op.Threshold = uint64(value)
	txn := new(protocol.UpdateKeyPage)
	txn.Operation = op

	return dispatchTxAndPrintResponse("update-key-page", txn, nil, principal, signer)
}

func lockKeyPage(args []string) (string, error) {
	_, principal, signer, err := parseArgsAndPrepareSigner(args)
	if err != nil {
		return "", err
	}

	op := new(protocol.UpdateAllowedKeyPageOperation)
	op.Deny = append(op.Deny, protocol.TransactionTypeUpdateKeyPage)
	txn := new(protocol.UpdateKeyPage)
	txn.Operation = op

	return dispatchTxAndPrintResponse("update-key-page", txn, nil, principal, signer)
}

func unlockKeyPage(args []string) (string, error) {
	_, principal, signer, err := parseArgsAndPrepareSigner(args)
	if err != nil {
		return "", err
	}

	op := new(protocol.UpdateAllowedKeyPageOperation)
	op.Allow = append(op.Deny, protocol.TransactionTypeUpdateKeyPage)
	txn := new(protocol.UpdateKeyPage)
	txn.Operation = op

	return dispatchTxAndPrintResponse("update-key-page", txn, nil, principal, signer)
}
