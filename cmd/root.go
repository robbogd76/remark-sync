package cmd

import (
	"fmt"
	"os"

	"github.com/mtlgro/remark-sync/internal/config"
	"github.com/spf13/cobra"
)

var cfg *config.Config

var rootCmd = &cobra.Command{
	Use:   "remark-sync",
	Short: "Pull Remarkable documents via USB, OCR handwriting, and send action items to task central",
	Long: `remark-sync connects to the Remarkable tablet over USB (http://10.11.99.1),
downloads documents as PDF, runs OCR on them, and posts any detected action items
(lines matching "<Action Type>: <description>") to a local task-central REST API.

The tablet must be connected via USB cable with USB web interface enabled.

Config file: ~/.config/remark-sync/config.yaml`,
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for the config-init subcommand.
		if cmd.Name() == "init" {
			return nil
		}
		var err error
		cfg, err = config.Load()
		return err
	},
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(configCmd)
}
