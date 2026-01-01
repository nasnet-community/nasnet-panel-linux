package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

var hashCmd = &cobra.Command{
	Use:   "hash-password [password]",
	Short: "Generate a bcrypt hash for a password",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		hash, err := bcrypt.GenerateFromPassword([]byte(args[0]), 10)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(string(hash))
	},
}

func init() {
	rootCmd.AddCommand(hashCmd)
}
