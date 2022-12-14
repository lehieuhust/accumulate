package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"gitlab.com/accumulatenetwork/accumulate/cmd/accumulate/walletd"
)

var encryptCmd = &cobra.Command{
	Use:   "encrypt",
	Short: "encrypt the database",
	Run: func(cmd *cobra.Command, args []string) {
		var out string
		var err error
		if len(args) == 0 {
			out, err = walletd.EncryptDatabase()
		} else {
			fmt.Println("Usage:")
			PrintEncrypt()
		}
		printOutput(cmd, out, err)
	},
}

func PrintEncrypt() {
	fmt.Println("  accumulate encrypt 		Encrypt the database, will be prompted for password")
}
