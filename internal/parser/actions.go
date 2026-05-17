// Package parser extracts structured action items from OCR'd text using
// configurable patterns.
//
// Each pattern specifies:
//   - A start regex matched at the beginning of a line to open an action.
//   - An optional end regex matched at the end of a line to close the action.
//
// Continuation lines (non-blank lines that don't start a new action) are
// joined with a space into the action's description. A blank line closes any
// open action when no end regex is configured.
//
// Example with the default ACTION pattern (start: "ACTION:", end: `\*`):
//
//	ACTION: Follow up with Sarah about the
//	Q4 budget and deadline *
//
// Produces: Action{Type: "ACTION", Description: "Follow up with Sarah about the Q4 budget and deadline"}
package parser

import (
	"fmt"
	"regexp"
	"strings"
)

// Action is a single extracted action item.
type Action struct {
	// Type is the pattern's name label, e.g. "ACTION".
	Type string
	// Description is the full text of the action with continuation lines
	// joined by a space and the end marker stripped.
	Description string
}

// Pattern is a compiled, ready-to-use action detection rule.
type Pattern struct {
	name    string
	startRe *regexp.Regexp // anchored to line start; group 1 = text after prefix
	endRe   *regexp.Regexp // anchored to line end; nil means blank-line termination only
}

// CompilePattern compiles a single pattern from its name, start regex string,
// and optional end regex string.
//
// If start ends with a literal colon (e.g. "ACTION:"), optional whitespace
// before the colon is accepted automatically. This handles the common OCR
// artefact where a space is inserted before the colon ("ACTION : text").
func CompilePattern(name, start, end string) (*Pattern, error) {
	if name == "" {
		return nil, fmt.Errorf("pattern name must not be empty")
	}

	// Allow optional whitespace before a trailing colon to absorb OCR spacing
	// artefacts such as "ACTION :" instead of "ACTION:".
	normalised := start
	if strings.HasSuffix(normalised, ":") {
		normalised = normalised[:len(normalised)-1] + `\s*:`
	}

	// Anchor to start of line, case-insensitive. Capture the description text
	// that follows the prefix on the same line (group 1).
	startRe, err := regexp.Compile(`(?i)^` + normalised + `\s*(.+)`)
	if err != nil {
		return nil, fmt.Errorf("invalid start regex %q: %w", start, err)
	}

	var endRe *regexp.Regexp
	if end != "" {
		// Anchor to end of line so the marker must be the last non-space
		// content; case-insensitive.
		endRe, err = regexp.Compile(`(?i)` + end + `\s*$`)
		if err != nil {
			return nil, fmt.Errorf("invalid end regex %q: %w", end, err)
		}
	}

	return &Pattern{name: name, startRe: startRe, endRe: endRe}, nil
}

// ParseActions extracts all action items from text using the given patterns.
// Lines are processed in order; continuation lines are joined with a space.
func ParseActions(text string, patterns []*Pattern) []Action {
	var actions []Action
	var cur *Pattern   // pattern that opened the current action
	var parts []string // accumulated description fragments

	flush := func() {
		if cur == nil {
			return
		}
		desc := strings.TrimSpace(strings.Join(parts, " "))
		if desc != "" {
			actions = append(actions, Action{Type: cur.name, Description: desc})
		}
		cur = nil
		parts = nil
	}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)

		// ── Check whether a new action starts on this line ────────────────────
		started := false
		for _, p := range patterns {
			m := p.startRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			flush()
			cur = p
			initial := strings.TrimSpace(m[1])
			initial, done := stripEnd(initial, p.endRe)
			if initial != "" {
				parts = []string{initial}
			}
			if done {
				flush()
			}
			started = true
			break
		}
		if started {
			continue
		}

		// ── Blank line ────────────────────────────────────────────────────────
		if line == "" {
			flush()
			continue
		}

		// ── Continuation line ─────────────────────────────────────────────────
		if cur != nil {
			cont, done := stripEnd(line, cur.endRe)
			if cont != "" {
				parts = append(parts, cont)
			}
			if done {
				flush()
			}
		}
	}
	flush()

	return actions
}

// stripEnd checks whether line matches the end regex. If so, it returns the
// text before the match (trimmed) and done=true. Otherwise it returns the
// original line and done=false. A nil endRe means never strip.
func stripEnd(line string, endRe *regexp.Regexp) (text string, done bool) {
	if endRe == nil {
		return line, false
	}
	loc := endRe.FindStringIndex(line)
	if loc == nil {
		return line, false
	}
	return strings.TrimSpace(line[:loc[0]]), true
}
