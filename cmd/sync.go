package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtlgro/remark-sync/internal/obsidian"
	"github.com/mtlgro/remark-sync/internal/ocr"
	"github.com/mtlgro/remark-sync/internal/parser"
	"github.com/mtlgro/remark-sync/internal/remarkable"
	"github.com/mtlgro/remark-sync/internal/state"
	"github.com/mtlgro/remark-sync/internal/tasks"
	"github.com/spf13/cobra"
)

var (
	filterName string
	dryRun     bool
	fullSync   bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Download documents updated since last sync, OCR them, and post action items",
	Long: `sync fetches every document modified since the last successful run from the
Remarkable tablet over USB, runs OCR on each page, scans for lines matching
a configured action pattern, and POSTs each match to the task-central REST API.

On the first run (or when --full is passed) all documents are processed.
The timestamp of each run is saved to ~/.config/remark-sync/state.yaml.

Use --name to restrict processing to documents whose name contains
the given substring (case-insensitive).

Use --dry-run to print extracted actions without posting them or updating
the last-sync timestamp.`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().StringVarP(&filterName, "name", "n", "",
		"Only process documents whose name contains this substring (case-insensitive)")
	syncCmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Print extracted actions without posting them to task central")
	syncCmd.Flags().BoolVar(&fullSync, "full", false,
		"Process all documents regardless of last-sync timestamp")
}

func runSync(cmd *cobra.Command, args []string) error {
	// Record the start time before doing any work so documents modified
	// during this run are captured on the next run.
	syncStart := time.Now().UTC()

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("loading sync state: %w", err)
	}

	client, err := remarkable.NewClient()
	if err != nil {
		return err
	}

	fmt.Println("Fetching document list...")
	docs, err := client.ListDocuments()
	if err != nil {
		return err
	}

	// Filter documents.
	var toProcess []remarkable.Document
	skippedOld := 0
	for _, d := range docs {
		if filterName != "" && !strings.Contains(strings.ToLower(d.VissibleName), strings.ToLower(filterName)) {
			continue
		}
		if !fullSync && !st.LastSync.IsZero() && !modifiedAfter(d.ModifiedClient, st.LastSync) {
			skippedOld++
			continue
		}
		toProcess = append(toProcess, d)
	}

	if !st.LastSync.IsZero() && !fullSync {
		fmt.Printf("Last sync: %s\n", st.LastSync.Local().Format("2006-01-02 15:04:05"))
	}
	if skippedOld > 0 {
		fmt.Printf("Skipping %d unchanged document(s).\n", skippedOld)
	}

	if len(toProcess) == 0 {
		fmt.Println("No new or updated documents to process.")
		return nil
	}
	fmt.Printf("Processing %d document(s)...\n", len(toProcess))

	ocrCfg := ocr.Config{
		AzureEndpoint: cfg.OCR.AzureEndpoint,
		AzureKey:      cfg.OCR.AzureKey,
		Lang:          cfg.OCR.Lang,
	}

	patterns := make([]*parser.Pattern, 0, len(cfg.ActionPatterns))
	for _, ap := range cfg.ActionPatterns {
		p, err := parser.CompilePattern(ap.Name, ap.Start, ap.End)
		if err != nil {
			return fmt.Errorf("action pattern %q: %w", ap.Name, err)
		}
		patterns = append(patterns, p)
	}

	var obsClient *obsidian.Client
	if cfg.Obsidian.URL != "" {
		baseFolder := cfg.Obsidian.BaseFolder
		if baseFolder == "" {
			baseFolder = "reMarkable"
		}
		obsClient = obsidian.NewClient(cfg.Obsidian.URL, cfg.Obsidian.APIKey, baseFolder)
	}

	taskClient := tasks.NewClient(cfg.TaskAPI.URL, cfg.TaskAPI.Token, cfg.TaskAPI.Headers)

	totalPosted := 0
	for _, doc := range toProcess {
		fmt.Printf("\n▶ %s\n", doc.VissibleName)

		actions, err := processDocument(client, doc, ocrCfg, patterns, cfg.PDFOutputDir, obsClient)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}

		if len(actions) == 0 {
			fmt.Println("  No action items found.")
			continue
		}

		fmt.Printf("  Found %d action item(s):\n", len(actions))
		for _, a := range actions {
			fmt.Printf("    [%s] %s\n", a.Type, a.Description)
		}

		if dryRun {
			continue
		}

		for _, a := range actions {
			exists, checkErr := taskClient.TaskExists(a.Description)
			if checkErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not check for duplicate [%s] %q: %v\n",
					a.Type, a.Description, checkErr)
			} else if exists {
				fmt.Printf("  Skipped (already exists): [%s] %s\n", a.Type, a.Description)
				continue
			}

			req := tasks.CreateTaskRequest{
				Title:       a.Description,
				Type:        a.Type,
				Description: "Source: Remarkable — " + doc.VissibleName,
			}
			if postErr := taskClient.PostTask(req); postErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to post [%s] %q: %v\n",
					a.Type, a.Description, postErr)
			} else {
				totalPosted++
			}
		}
	}

	fmt.Println()
	if dryRun {
		fmt.Println("Dry run complete — no tasks were posted and last-sync time was not updated.")
	} else {
		fmt.Printf("Done. Posted %d action item(s) to task central.\n", totalPosted)

		if cfg.PDFOutputDir != "" {
			cleanupOrphanedPDFs(cfg.PDFOutputDir, docs)
		}

		st.LastSync = syncStart
		if saveErr := st.Save(); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save sync state: %v\n", saveErr)
		}
	}
	return nil
}

// modifiedAfter reports whether a document's ModifiedClient timestamp is
// strictly after cutoff. Returns true if the timestamp cannot be parsed so
// that documents with unreadable timestamps are never silently skipped.
func modifiedAfter(modifiedClient string, cutoff time.Time) bool {
	if modifiedClient == "" {
		return true
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, modifiedClient); err == nil {
			return t.After(cutoff)
		}
	}
	// Unparseable — include the document to be safe.
	return true
}

// processDocument downloads a document as PDF from the tablet, optionally
// saves it to pdfOutputDir, optionally sends OCR text to Obsidian, and
// returns parsed action items.
func processDocument(client *remarkable.Client, doc remarkable.Document, ocrCfg ocr.Config, patterns []*parser.Pattern, pdfOutputDir string, obsClient *obsidian.Client) ([]parser.Action, error) {
	fmt.Println("  Downloading PDF...")
	pdfData, err := client.DownloadPDF(doc)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	if pdfOutputDir != "" {
		if err := savePDF(pdfData, pdfOutputDir, doc.FolderPath, doc.VissibleName); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not save PDF: %v\n", err)
		}
	}

	fmt.Println("  Running OCR...")
	text, err := ocr.OCRPdf(pdfData, ocrCfg)
	if err != nil {
		return nil, fmt.Errorf("OCR: %w", err)
	}

	if obsClient != nil {
		if err := obsClient.WriteNote(doc.FolderPath, doc.VissibleName, text); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not send to Obsidian: %v\n", err)
		} else {
			fmt.Println("  Sent to Obsidian.")
		}
	}

	return parser.ParseActions(text, patterns), nil
}

// savePDF writes pdfData to dir/<folderPath>/<sanitized docName>.pdf,
// creating directories as needed. folderPath is a slash-separated chain of
// folder names (e.g. "Work/2024") and may be empty for root-level documents.
func savePDF(data []byte, dir, folderPath, docName string) error {
	// Build the target directory, sanitizing each folder name component.
	destDir := dir
	for _, segment := range strings.Split(folderPath, "/") {
		if segment != "" {
			destDir = filepath.Join(destDir, sanitizeFilename(segment))
		}
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating PDF output directory: %w", err)
	}
	filename := sanitizeFilename(docName) + ".pdf"
	dest := filepath.Join(destDir, filename)
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}
	fmt.Printf("  Saved PDF: %s\n", dest)
	return nil
}

// sanitizeFilename replaces characters that are invalid in filenames on
// Windows and Unix with a hyphen.
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-",
		"?", "-", "\"", "-", "<", "-", ">", "-", "|", "-",
	)
	return strings.TrimSpace(replacer.Replace(name))
}

// expectedPDFPath returns the filesystem path where savePDF would write the
// PDF for doc, mirroring the reMarkable folder structure under outputDir.
func expectedPDFPath(outputDir string, doc remarkable.Document) string {
	destDir := outputDir
	for _, segment := range strings.Split(doc.FolderPath, "/") {
		if segment != "" {
			destDir = filepath.Join(destDir, sanitizeFilename(segment))
		}
	}
	return filepath.Join(destDir, sanitizeFilename(doc.VissibleName)+".pdf")
}

// cleanupOrphanedPDFs removes any .pdf files under outputDir that no longer
// correspond to a document on the reMarkable, then prunes empty directories.
func cleanupOrphanedPDFs(outputDir string, docs []remarkable.Document) {
	expected := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		expected[filepath.Clean(expectedPDFPath(outputDir, d))] = struct{}{}
	}

	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() || strings.ToLower(filepath.Ext(path)) != ".pdf" {
			return nil
		}
		if _, ok := expected[filepath.Clean(path)]; !ok {
			if removeErr := os.Remove(path); removeErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not remove orphaned PDF %s: %v\n", path, removeErr)
			} else {
				fmt.Printf("  Removed orphaned PDF: %s\n", path)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: walking PDF output dir: %v\n", err)
	}

	removeEmptyDirs(outputDir)
}

// removeEmptyDirs removes all empty subdirectories under root, deepest first.
func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	// Process deepest directories first so a parent becomes empty only after
	// its children have been removed.
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err == nil && len(entries) == 0 {
			if removeErr := os.Remove(dirs[i]); removeErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not remove empty dir %s: %v\n", dirs[i], removeErr)
			}
		}
	}
}
