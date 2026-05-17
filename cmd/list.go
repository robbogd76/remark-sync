package cmd

import (
	"fmt"
	"strings"

	"github.com/mtlgro/remark-sync/internal/remarkable"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents on the Remarkable tablet (USB)",
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := remarkable.NewClient()
	if err != nil {
		return err
	}

	docs, err := client.ListDocuments()
	if err != nil {
		return err
	}

	// Header
	fmt.Printf("%-45s %-12s %s\n", "Name", "Type", "Modified")
	fmt.Println(strings.Repeat("─", 90))

	for _, d := range docs {
		kind := "document"
		if d.IsFolder() {
			kind = "folder"
		}
		// Truncate long names for display
		name := d.VissibleName
		if len(name) > 44 {
			name = name[:41] + "..."
		}
		modified := d.ModifiedClient
		if len(modified) > 19 {
			modified = modified[:19] // trim to "2006-01-02T15:04:05"
		}
		fmt.Printf("%-45s %-12s %s\n", name, kind, modified)
	}

	fmt.Printf("\n%d item(s) total.\n", len(docs))
	return nil
}
