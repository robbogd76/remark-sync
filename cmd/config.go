package cmd

import (
	"fmt"

	"github.com/mtlgro/remark-sync/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the current configuration (tokens are masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath, err := config.Path()
		if err != nil {
			return err
		}
		fmt.Printf("Config file: %s\n\n", cfgPath)

		// Print config with masked secrets.
		display := *cfg
		if display.TaskAPI.Token != "" {
			display.TaskAPI.Token = mask(display.TaskAPI.Token)
		}
		if display.OCR.AzureKey != "" {
			display.OCR.AzureKey = mask(display.OCR.AzureKey)
		}

		out, err := yaml.Marshal(&display)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default config file if one does not exist",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath, err := config.Path()
		if err != nil {
			return err
		}

		// Load will return defaults if the file doesn't exist.
		c, err := config.Load()
		if err != nil {
			return err
		}

		if err := c.Save(); err != nil {
			return err
		}
		fmt.Printf("Config written to %s\n", cfgPath)
		fmt.Println("Edit it to set task_api.url, ocr settings, and action_filter.")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configInitCmd)
}

// mask shows only the first and last 4 characters of a token.
func mask(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
