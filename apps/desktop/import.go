package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/importer"
)

// ImportSupport says what this system can import from besides folders
// and zips.
type ImportSupport struct {
	AppleNotes bool `json:"appleNotes"`
}

// ImportSources lists the import sources this system has.
func (s *Vault) ImportSources() ImportSupport {
	return ImportSupport{AppleNotes: importer.AppleNotesSupported}
}

// ImportFiles asks for a folder (zip false) or a zip of Markdown notes
// and imports it into a new folder named after it (importer.Run).
// It returns nil when the user cancels.
func (s *Vault) ImportFiles(ctx context.Context, zip bool) (*importer.Report, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	opts := mygo.OpenDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "Import",
		ButtonLabel: "Import",
		Directory:   !zip,
	}
	if zip {
		opts.Filters = []mygo.FileFilter{{Name: "Zip", Extensions: []string{"zip"}}}
	} else {
		opts.Message = "Choose a folder of Markdown notes, such as an Obsidian vault."
	}
	if home, err := os.UserHomeDir(); err == nil {
		opts.DefaultPath = home
		if d := filepath.Join(home, "Downloads"); zip && dirExists(d) {
			opts.DefaultPath = d
		}
	}
	paths, err := mygo.Dialog.Open(opts)
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	files, name, err := importer.Open(paths[0])
	if err != nil {
		return nil, err
	}
	return s.runImport(files, name)
}

// ImportAppleNotes imports every note in Apple Notes (macOS), which
// asks the user to allow it the first time.
func (s *Vault) ImportAppleNotes(ctx context.Context) (*importer.Report, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	notes, err := importer.AppleNotes(ctx)
	if err != nil {
		return nil, err
	}
	return s.runImport(importer.AppleNotesFiles(notes), "Apple Notes")
}

func (s *Vault) runImport(files []importer.File, name string) (*importer.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	folder, err := importer.DefaultFolder(s.v, name)
	if err != nil {
		return nil, err
	}
	return importer.Run(s.v, files, importer.Options{Folder: folder})
}
