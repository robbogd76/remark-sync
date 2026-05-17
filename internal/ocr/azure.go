// Package ocr sends documents to Azure Computer Vision for text extraction.
package ocr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config controls the OCR pipeline.
type Config struct {
	// AzureEndpoint is the Azure Cognitive Services resource URL,
	// e.g. "https://<resource>.cognitiveservices.azure.com".
	AzureEndpoint string

	// AzureKey is the Ocp-Apim-Subscription-Key for the Azure resource.
	AzureKey string

	// Lang is an optional BCP-47 language hint (e.g. "en", "fr").
	// Leave empty to let Azure auto-detect the language.
	Lang string
}

var azureHTTPClient = &http.Client{Timeout: 120 * time.Second}

// OCRPdf submits a PDF to the Azure Computer Vision Read API and returns the
// extracted text. The Read API is asynchronous: the PDF is posted, then the
// operation result is polled until it succeeds.
func OCRPdf(pdfData []byte, cfg Config) (string, error) {
	if cfg.AzureEndpoint == "" {
		return "", fmt.Errorf("ocr.azure_endpoint is not configured")
	}
	if cfg.AzureKey == "" {
		return "", fmt.Errorf("ocr.azure_key is not configured")
	}

	operationURL, err := submitRead(pdfData, cfg)
	if err != nil {
		return "", err
	}
	return pollRead(operationURL, cfg.AzureKey)
}

// submitRead POSTs the PDF bytes to /vision/v3.2/read/analyze and returns the
// Operation-Location URL to poll for results.
func submitRead(data []byte, cfg Config) (string, error) {
	u := strings.TrimRight(cfg.AzureEndpoint, "/") + "/vision/v3.2/read/analyze"
	if lang := normalizeLang(cfg.Lang); lang != "" {
		u += "?language=" + lang
	}

	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("building Azure request: %w", err)
	}
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set("Ocp-Apim-Subscription-Key", cfg.AzureKey)
	req.ContentLength = int64(len(data))

	resp, err := azureHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Azure OCR submit: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("Azure OCR submit failed (HTTP %d): %s", resp.StatusCode, body)
	}

	opURL := resp.Header.Get("Operation-Location")
	if opURL == "" {
		return "", fmt.Errorf("Azure OCR: missing Operation-Location header")
	}
	return opURL, nil
}

// pollRead polls the operation URL until the read operation succeeds or fails.
// It backs off from 1 s up to 4 s between polls, timing out after 2 minutes.
func pollRead(operationURL, key string) (string, error) {
	const timeout = 2 * time.Minute
	deadline := time.Now().Add(timeout)
	delay := time.Second

	for time.Now().Before(deadline) {
		time.Sleep(delay)
		if delay < 4*time.Second {
			delay *= 2
		}

		req, err := http.NewRequest(http.MethodGet, operationURL, nil)
		if err != nil {
			return "", fmt.Errorf("building poll request: %w", err)
		}
		req.Header.Set("Ocp-Apim-Subscription-Key", key)

		resp, err := azureHTTPClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("Azure OCR poll: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("reading Azure poll response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("Azure OCR poll failed (HTTP %d): %s", resp.StatusCode, body)
		}

		var result readOperationResult
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("parsing Azure poll response: %w", err)
		}

		switch result.Status {
		case "succeeded":
			return extractText(result), nil
		case "failed":
			return "", fmt.Errorf("Azure OCR operation failed")
		}
		// "running" or "notStarted" — keep polling
	}

	return "", fmt.Errorf("Azure OCR timed out after %s", timeout)
}

// ── response types ────────────────────────────────────────────────────────────

// readOperationResult is the JSON body returned when polling the operation URL.
type readOperationResult struct {
	Status        string `json:"status"` // "notStarted" | "running" | "succeeded" | "failed"
	AnalyzeResult struct {
		ReadResults []struct {
			Lines []struct {
				Text string `json:"text"`
			} `json:"lines"`
		} `json:"readResults"`
	} `json:"analyzeResult"`
}

// extractText concatenates all recognised lines across all pages.
func extractText(r readOperationResult) string {
	var sb strings.Builder
	for _, page := range r.AnalyzeResult.ReadResults {
		for _, line := range page.Lines {
			sb.WriteString(line.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// ── language normalisation ────────────────────────────────────────────────────

// normalizeLang converts a language tag to a BCP-47 code accepted by Azure.
// Tesseract ISO 639-3 codes (e.g. "eng") are mapped to BCP-47 equivalents.
// Already-valid BCP-47 codes pass through unchanged.
// Unrecognised values return "" so the language parameter is omitted and
// Azure auto-detects the language.
func normalizeLang(lang string) string {
	if lang == "" {
		return ""
	}
	// Handle compound Tesseract codes like "eng+fra" — use the first part only.
	if idx := strings.Index(lang, "+"); idx >= 0 {
		lang = lang[:idx]
	}
	iso6393 := map[string]string{
		"eng": "en", "fra": "fr", "deu": "de", "spa": "es",
		"ita": "it", "por": "pt", "nld": "nl", "rus": "ru",
		"jpn": "ja", "kor": "ko", "ara": "ar", "pol": "pl",
		"ces": "cs", "swe": "sv", "dan": "da", "nor": "no",
		"fin": "fi", "hun": "hu", "tur": "tr",
		"zho": "zh-Hans", "chi_sim": "zh-Hans", "chi_tra": "zh-Hant",
	}
	if bcp47, ok := iso6393[lang]; ok {
		return bcp47
	}
	// Already looks like BCP-47 (short, no underscores) — pass through.
	if len(lang) <= 8 && !strings.Contains(lang, "_") {
		return lang
	}
	// Unrecognised — omit so Azure auto-detects.
	return ""
}
