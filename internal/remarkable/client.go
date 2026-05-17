package remarkable

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const usbHost = "http://10.11.99.1"

// Client communicates with the Remarkable tablet over the USB web interface.
type Client struct {
	http *http.Client
}

// NewClient creates a Client that connects to the tablet via USB.
// The tablet must be connected via USB cable and the USB web interface enabled.
func NewClient() (*Client, error) {
	return &Client{
		http: &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// usbDocument is the JSON shape returned by GET /documents/
type usbDocument struct {
	ID             string `json:"ID"`
	VissibleName   string `json:"VissibleName"`
	Type           string `json:"Type"`
	Parent         string `json:"Parent"`
	ModifiedClient string `json:"ModifiedClient"`
	Bookmarked     bool   `json:"Bookmarked"`
}

// ListDocuments returns all non-folder, non-trashed documents from the tablet,
// recursively fetching the contents of every folder. Each document's FolderPath
// reflects its full ancestor chain, e.g. "Work/2024".
func (c *Client) ListDocuments() ([]Document, error) {
	var docs []Document
	if err := c.collectDocuments("/documents/", "", &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// collectDocuments fetches the items at apiPath, appends non-folder items to
// docs (with FolderPath set to folderPath), and recurses into folders by
// appending their display name to the path.
func (c *Client) collectDocuments(apiPath, folderPath string, docs *[]Document) error {
	items, err := c.fetchItems(apiPath)
	if err != nil {
		return err
	}

	for _, item := range items {
		if item.Parent == "trash" {
			continue
		}
		if item.Type == CollectionType {
			childPath := item.VissibleName
			if folderPath != "" {
				childPath = folderPath + "/" + item.VissibleName
			}
			if err := c.collectDocuments("/documents/"+item.ID, childPath, docs); err != nil {
				return err
			}
		} else {
			*docs = append(*docs, Document{
				ID:             item.ID,
				VissibleName:   item.VissibleName,
				Type:           item.Type,
				Parent:         item.Parent,
				ModifiedClient: item.ModifiedClient,
				FolderPath:     folderPath,
			})
		}
	}
	return nil
}

// fetchItems calls GET on the given path and decodes the JSON array response.
func (c *Client) fetchItems(path string) ([]usbDocument, error) {
	resp, err := c.http.Get(usbHost + path)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w — is the tablet connected via USB?", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetching %s failed (HTTP %d): %s", path, resp.StatusCode, b)
	}

	var items []usbDocument
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", path, err)
	}
	return items, nil
}

// DownloadPDF fetches the document from the tablet as a PDF via USB.
// For native notebooks the tablet renders the strokes to PDF on demand;
// no local rendering is required.
func (c *Client) DownloadPDF(doc Document) ([]byte, error) {
	url := fmt.Sprintf("%s/download/%s/pdf", usbHost, doc.ID)

	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading %q: %w", doc.VissibleName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (HTTP %d): %s", resp.StatusCode, b)
	}

	return io.ReadAll(resp.Body)
}
