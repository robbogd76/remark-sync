// Package obsidian writes notes to an Obsidian vault via the Local REST API
// plugin (https://github.com/coddingtonbear/obsidian-local-rest-api).
//
// Default plugin address: http://localhost:27123
// Authentication: Bearer token set in the plugin settings.
//
// Files are created/overwritten with PUT /vault/{path}.
package obsidian

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client writes notes to Obsidian.
type Client struct {
	baseURL    string
	apiKey     string
	baseFolder string
	http       *http.Client
}

// NewClient creates a Client.
// baseURL is the plugin address (e.g. "http://localhost:27123").
// apiKey is the Bearer token from the plugin settings.
// baseFolder is the vault-root folder under which all reMarkable notes live.
func NewClient(baseURL, apiKey, baseFolder string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		baseFolder: baseFolder,
		http:       &http.Client{Timeout: 30 * time.Second},
	}
}

// WriteNote creates or overwrites the note at
//
//	<baseFolder>/<folderPath>/<docName>.md
//
// folderPath is the slash-separated ancestor chain from the reMarkable
// (e.g. "Work/2024"); it may be empty for root-level documents.
func (c *Client) WriteNote(folderPath, docName, ocrText string) error {
	vaultPath := buildVaultPath(c.baseFolder, folderPath, docName)
	u := c.baseURL + "/vault/" + encodePath(vaultPath)

	content := formatNote(docName, ocrText)

	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader([]byte(content)))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "text/markdown")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sending note to Obsidian: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Obsidian API returned HTTP %d: %s", resp.StatusCode, b)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// buildVaultPath assembles the full vault-relative path for the note file.
func buildVaultPath(baseFolder, folderPath, docName string) string {
	parts := []string{baseFolder}
	for _, seg := range strings.Split(folderPath, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	parts = append(parts, docName+".md")
	return strings.Join(parts, "/")
}

// encodePath percent-encodes each path segment individually so that forward
// slashes are preserved as URL path separators.
func encodePath(path string) string {
	segments := strings.Split(path, "/")
	encoded := make([]string, len(segments))
	for i, s := range segments {
		encoded[i] = url.PathEscape(s)
	}
	return strings.Join(encoded, "/")
}

// formatNote wraps the raw OCR text in a minimal Markdown document.
func formatNote(docName, ocrText string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("source: reMarkable\n")
	sb.WriteString("synced: ")
	sb.WriteString(time.Now().UTC().Format(time.RFC3339))
	sb.WriteString("\n---\n\n")
	sb.WriteString("# ")
	sb.WriteString(docName)
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(ocrText))
	sb.WriteByte('\n')
	return sb.String()
}
