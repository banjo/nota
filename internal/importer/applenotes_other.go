//go:build !darwin

package importer

import (
	"context"
	"errors"
)

// AppleNotesSupported reports whether this system has Apple Notes.
const AppleNotesSupported = false

// AppleNotes reads Apple Notes, which only macOS has.
func AppleNotes(context.Context) ([]AppleNote, error) {
	return nil, errors.New("Apple Notes can only be imported on a Mac")
}
