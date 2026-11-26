package importer

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

//go:embed applenotes.js
var appleNotesJS string

// AppleNotesSupported reports whether this system has Apple Notes.
const AppleNotesSupported = true

// AppleNotes reads every note in Apple Notes through its scripting, which
// macOS asks the user to allow the first time.
func AppleNotes(ctx context.Context) ([]AppleNote, error) {
	cmd := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-")
	cmd.Stdin = strings.NewReader(appleNotesJS)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "-1743") || strings.Contains(msg, "Not authorized") || strings.Contains(msg, "-1744") {
			return nil, ErrNotAllowed
		}
		return nil, fmt.Errorf("read Apple Notes: %v: %s", err, msg)
	}
	var notes []AppleNote
	if err := json.Unmarshal(out, &notes); err != nil {
		return nil, fmt.Errorf("read Apple Notes: %w", err)
	}
	for i := range notes {
		notes[i].Created = notes[i].Created.Local()
		notes[i].Modified = notes[i].Modified.Local()
	}
	return notes, nil
}
