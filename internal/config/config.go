package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all remark-sync settings persisted to disk.
type Config struct {
	TaskAPI        TaskAPI         `yaml:"task_api"`
	OCR            OCRConfig       `yaml:"ocr"`
	ActionPatterns []ActionPattern `yaml:"action_patterns"`
	// PDFOutputDir is an optional directory path. When set, every downloaded
	// PDF is written to this directory mirroring the reMarkable folder structure.
	// Leave empty to disable PDF saving.
	PDFOutputDir string         `yaml:"pdf_output_dir,omitempty"`
	Obsidian     ObsidianConfig `yaml:"obsidian,omitempty"`
}

// ObsidianConfig configures the optional Obsidian Local REST API integration.
type ObsidianConfig struct {
	// URL is the address of the Obsidian Local REST API plugin,
	// e.g. "http://localhost:27123". Leave empty to disable.
	URL string `yaml:"url,omitempty"`
	// APIKey is the Bearer token set in the plugin settings.
	APIKey string `yaml:"api_key,omitempty"`
	// BaseFolder is the vault folder under which all reMarkable notes are
	// stored, mirroring the reMarkable folder structure beneath it.
	// Defaults to "reMarkable".
	BaseFolder string `yaml:"base_folder,omitempty"`
}

// ActionPattern defines how to detect one type of action item in OCR text.
type ActionPattern struct {
	// Name is the type label posted to TaskCentral, e.g. "ACTION".
	Name string `yaml:"name"`
	// Start is a regular expression matched (case-insensitively) at the
	// beginning of a line to open an action. Text on the same line after the
	// match becomes the first part of the description.
	Start string `yaml:"start"`
	// End is an optional regular expression matched at the end of a line to
	// close the action. The matched text and any trailing whitespace are
	// stripped from the description. When empty, a blank line closes the action.
	End string `yaml:"end,omitempty"`
}

// TaskAPI configures the local task-central HTTP endpoint.
type TaskAPI struct {
	URL     string            `yaml:"url"`
	Token   string            `yaml:"token"`              // optional Bearer token
	Headers map[string]string `yaml:"headers,omitempty"` // extra request headers
}

// OCRConfig controls the Azure Computer Vision OCR pipeline.
type OCRConfig struct {
	AzureEndpoint string `yaml:"azure_endpoint"` // e.g. "https://<resource>.cognitiveservices.azure.com"
	AzureKey      string `yaml:"azure_key"`      // Ocp-Apim-Subscription-Key
	Lang          string `yaml:"lang,omitempty"` // BCP-47 language hint, e.g. "en"; empty = auto-detect
}

// DefaultConfig returns a Config with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		TaskAPI: TaskAPI{
			URL: "http://localhost:3000/api/v1/tasks",
		},
		OCR: OCRConfig{},
		ActionPatterns: []ActionPattern{
			{Name: "ACTION", Start: `ACTION:`, End: `\*`},
		},
		Obsidian: ObsidianConfig{
			BaseFolder: "reMarkable",
		},
	}
}

// Dir returns the directory that holds the config file.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "remark-sync"), nil
}

// Path returns the full path to config.yaml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads config from disk. Returns defaults if the file does not yet exist.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// Save writes the config to disk, creating directories as needed.
func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	path, err := Path()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}
