// Package state persists runtime state (last sync time) separately from
// user configuration.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mtlgro/remark-sync/internal/config"
	"gopkg.in/yaml.v3"
)

// State holds runtime values that change on every sync run.
type State struct {
	// LastSync is the UTC start time of the most recent successful sync.
	// Zero value means no sync has been recorded yet (first run).
	LastSync time.Time `yaml:"last_sync,omitempty"`
}

// path returns the full path to state.yaml.
func path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.yaml"), nil
}

// Load reads state from disk. Returns an empty State if the file does not
// exist yet (first run).
func Load() (*State, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}
	return &s, nil
}

// Save writes the state to disk.
func (s *State) Save() error {
	p, err := path()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	return os.WriteFile(p, data, 0o600)
}
