package remarkable

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"strings"

	"github.com/mtlgro/remark-sync/internal/remarkable/rmparse"
)

// contentMeta holds the parts of a Remarkable .content file we care about.
type contentMeta struct {
	FileType string   `json:"fileType"`
	Pages    []string `json:"pages"` // page UUIDs in display order
}

// RenderNotebookPages extracts each page from a native Remarkable notebook
// bundle (ZIP), parses the .rm stroke data, renders it to a PNG, and returns
// the PNG bytes in page order.
//
// Returns ErrNoPDF (reused sentinel) if the bundle is not a notebook.
func RenderNotebookPages(bundle []byte) ([][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return nil, fmt.Errorf("opening notebook bundle: %w", err)
	}

	meta, docUUID, err := readContentMeta(zr)
	if err != nil {
		return nil, err
	}
	if meta.FileType != "notebook" {
		return nil, fmt.Errorf("document type is %q, expected \"notebook\"", meta.FileType)
	}
	if len(meta.Pages) == 0 {
		return nil, fmt.Errorf("notebook has no pages")
	}

	var pages [][]byte
	for _, pageUUID := range meta.Pages {
		rmPath := docUUID + "/" + pageUUID + ".rm"
		rmData, err := readZipEntry(zr, rmPath)
		if err != nil {
			// Blank / not-yet-written pages may simply be absent.
			continue
		}

		strokes, err := rmparse.Parse(bytes.NewReader(rmData))
		if err != nil {
			// Unrecognised format — skip rather than abort.
			continue
		}

		img := RenderStrokes(strokes)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			continue
		}
		pages = append(pages, buf.Bytes())
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("notebook bundle had no renderable pages")
	}
	return pages, nil
}

// readContentMeta finds the top-level *.content file in the ZIP and decodes it.
// Also returns the document UUID (the filename stem before ".content").
func readContentMeta(zr *zip.Reader) (*contentMeta, string, error) {
	for _, f := range zr.File {
		// Only match top-level *.content files (no directory separator).
		if strings.Contains(f.Name, "/") || !strings.HasSuffix(f.Name, ".content") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, "", fmt.Errorf("opening %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, "", fmt.Errorf("reading %s: %w", f.Name, err)
		}

		var meta contentMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", f.Name, err)
		}

		docUUID := strings.TrimSuffix(f.Name, ".content")
		return &meta, docUUID, nil
	}
	return nil, "", fmt.Errorf("no .content file found in bundle")
}

// readZipEntry reads the full contents of the named entry from the ZIP.
func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%q not found in bundle", name)
}
