// Package tasks posts extracted action items to the TaskCentral REST API.
//
// API base: http://localhost:3000/api/v1
// Endpoint: POST /api/v1/tasks
package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CreateTaskRequest is the JSON body sent to POST /api/v1/tasks.
// Only Title is required; all other fields are optional.
type CreateTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`   // pending | in-progress | completed
	Priority    string `json:"priority,omitempty"` // low | medium | high
	Type        string `json:"type,omitempty"`     // free-form category (e.g. "TODO", "MEETING")
	DueDate     string `json:"due_date,omitempty"` // YYYY-MM-DD
	URL         string `json:"url,omitempty"`
}

// taskResponse is the envelope returned by the TaskCentral API on success.
type taskResponse struct {
	Data json.RawMessage `json:"data"`
}

// listResponse is the envelope returned by GET /api/v1/tasks.
type listResponse struct {
	Data []struct {
		Title string `json:"title"`
	} `json:"data"`
}


// validationError holds one field error from a 400 response.
type validationError struct {
	Path string `json:"path"`
	Msg  string `json:"msg"`
}

// errorResponse is the envelope for 400/404/500 responses.
type errorResponse struct {
	Error  string            `json:"error"`
	Errors []validationError `json:"errors"`
}

// Client posts tasks to the TaskCentral REST API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a Client.
// baseURL should be the full URL to the tasks endpoint
// (e.g. "http://localhost:3000/api/v1/tasks").
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// PostTask creates a new task in TaskCentral.
func (c *Client) PostTask(req CreateTaskRequest) error {
	if c.baseURL == "" {
		return fmt.Errorf("task_api.url is not configured")
	}
	if req.Title == "" {
		return fmt.Errorf("task title must not be empty")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("posting task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		return nil // success
	}

	// Parse the error body for a useful message.
	raw, _ := io.ReadAll(resp.Body)
	var errBody errorResponse
	if json.Unmarshal(raw, &errBody) == nil {
		if len(errBody.Errors) > 0 {
			msgs := make([]string, len(errBody.Errors))
			for i, e := range errBody.Errors {
				msgs[i] = fmt.Sprintf("%s: %s", e.Path, e.Msg)
			}
			return fmt.Errorf("TaskCentral validation error (HTTP %d): %s",
				resp.StatusCode, strings.Join(msgs, "; "))
		}
		if errBody.Error != "" {
			return fmt.Errorf("TaskCentral error (HTTP %d): %s", resp.StatusCode, errBody.Error)
		}
	}
	return fmt.Errorf("TaskCentral returned HTTP %d", resp.StatusCode)
}

// TaskExists reports whether an active (non-completed) task with exactly the
// given title already exists in TaskCentral. The search is case-insensitive.
func (c *Client) TaskExists(title string) (bool, error) {
	if c.baseURL == "" {
		return false, fmt.Errorf("task_api.url is not configured")
	}

	u := c.baseURL + "?status=active&limit=200&search=" + url.QueryEscape(title)
	httpReq, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return false, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("checking task existence: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("TaskCentral returned HTTP %d: %s", resp.StatusCode, raw)
	}

	var result listResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decoding task list: %w", err)
	}

	normalised := strings.ToLower(strings.TrimSpace(title))
	for _, t := range result.Data {
		if strings.ToLower(strings.TrimSpace(t.Title)) == normalised {
			return true, nil
		}
	}
	return false, nil
}
