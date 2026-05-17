package remarkable

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ErrNoPDF is returned when a document bundle contains no PDF file.
// This happens for native Remarkable notebooks (stored as .rm stroke files).
var ErrNoPDF = fmt.Errorf("document bundle contains no PDF — this is a native notebook; export it as PDF from the Remarkable app first")

// ExtractPDF locates and extracts the first PDF found inside a Remarkable
// document bundle (ZIP). Returns the PDF bytes and its name within the archive.
func ExtractPDF(data []byte) ([]byte, string, error) {
	// The bundle may occasionally be a raw PDF rather than a ZIP.
	if isPDF(data) {
		return data, "document.pdf", nil
	}

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", fmt.Errorf("opening document bundle: %w", err)
	}

	for _, f := range r.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
			rc, err := f.Open()
			if err != nil {
				return nil, "", fmt.Errorf("opening %s in bundle: %w", f.Name, err)
			}
			defer rc.Close()

			pdf, err := io.ReadAll(rc)
			if err != nil {
				return nil, "", fmt.Errorf("reading %s from bundle: %w", f.Name, err)
			}
			return pdf, f.Name, nil
		}
	}

	return nil, "", ErrNoPDF
}

// isPDF returns true if data starts with the PDF magic bytes %PDF.
func isPDF(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == "%PDF"
}
