package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/banjo/nota/internal/export"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/publish"
)

// ExportRequest is what to export: a note's path, a folder's, or "" for
// the vault, in a format (html, md, zip), and the look of HTML.
type ExportRequest struct {
	Path   string               `json:"path"`
	Format string               `json:"format"`
	Theme  *protocol.ShareTheme `json:"theme,omitempty"`
}

func (s *Vault) export(req ExportRequest) (*export.Export, error) {
	if s.v == nil {
		return nil, ErrNoVault
	}
	if req.Theme != nil {
		if err := publish.CheckTheme(*req.Theme); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return export.New(s.v, s.ix, req.Path, export.Options{
		Format: export.Format(req.Format),
		Theme:  req.Theme,
		Name:   filepath.Base(s.v.Root()),
	})
}

// saveAs asks where to save a file named name, starting in Downloads,
// and returns the path, "" when the user cancels.
func saveAs(ctx context.Context, title, name string) (string, error) {
	dir, _ := os.UserHomeDir()
	if d := filepath.Join(dir, "Downloads"); dirExists(d) {
		dir = d
	}
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:            mygo.CallerWindow(ctx),
		Title:             title,
		DefaultPath:       filepath.Join(dir, name),
		ButtonLabel:       "Export",
		CreateDirectories: true,
		Filters:           []mygo.FileFilter{{Name: strings.ToUpper(ext), Extensions: []string{ext}}},
	})
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Export exports a note, a folder or the vault (export.New) to a file
// the user picks, and returns its path; "" when they cancel.
func (s *Vault) Export(ctx context.Context, req ExportRequest) (string, error) {
	e, err := s.export(req)
	if err != nil {
		return "", err
	}
	dest, err := saveAs(ctx, "Export", e.Name)
	if err != nil || dest == "" {
		return "", err
	}
	_, err = e.Save(dest)
	return dest, err
}

// page loads a note's HTML export into a new window and waits for it.
func (s *Vault) page(path string, theme *protocol.ShareTheme, show bool) (*mygo.Window, string, error) {
	e, err := s.export(ExportRequest{Path: path, Format: string(export.HTML), Theme: theme})
	if err != nil {
		return nil, "", err
	}
	var html bytes.Buffer
	if _, err := e.WriteTo(&html); err != nil {
		return nil, "", err
	}
	name := strings.TrimSuffix(e.Name, ".html")
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:  name,
		Width:  820,
		Height: 1000,
		Hidden: !show,
	})
	loaded := make(chan struct{}, 1)
	off := w.Page().OnDidFinishLoad(func() {
		select {
		case loaded <- struct{}{}:
		default:
		}
	})
	defer off()
	w.Page().LoadHTML(html.String(), "")
	select {
	case <-loaded:
		return w, name, nil
	case <-time.After(15 * time.Second):
		w.Destroy()
		return nil, "", errors.New("the page of the note didn't load")
	}
}

// ExportPDF prints a note to a PDF the user saves, and returns its path;
// "" when they cancel. The paper is Letter where that is the custom, A4
// elsewhere.
func (s *Vault) ExportPDF(ctx context.Context, path string, theme *protocol.ShareTheme) (string, error) {
	w, name, err := s.page(path, theme, false)
	if err != nil {
		return "", err
	}
	size := mygo.PageA4
	if letterRegion(mygo.App.Region().Locale) {
		size = mygo.PageLetter
	}
	pdf, err := w.Page().PrintToPDF(mygo.PDFOptions{PageSize: size, Background: true,
		Margins: &mygo.Margins{Top: 0.6, Right: 0.7, Bottom: 0.6, Left: 0.7}})
	w.Destroy()
	if err != nil {
		return "", err
	}
	dest, err := saveAs(ctx, "Export as PDF", name+".pdf")
	if err != nil || dest == "" {
		return "", err
	}
	return dest, os.WriteFile(dest, pdf, 0o644)
}

// Print shows a note's page in a window of its own, with the system's
// print dialog on it.
func (s *Vault) Print(path string, theme *protocol.ShareTheme) error {
	w, _, err := s.page(path, theme, true)
	if err != nil {
		return err
	}
	w.Focus()
	w.Page().Print()
	return nil
}

// letterRegion reports whether a locale ("en_US", "es-MX") is of a
// country that prints on Letter paper.
func letterRegion(locale string) bool {
	locale = strings.ReplaceAll(locale, "-", "_")
	_, region, _ := strings.Cut(locale, "_")
	region, _, _ = strings.Cut(region, "@")
	switch strings.ToUpper(region) {
	case "US", "CA", "MX", "PH", "CL", "CO", "VE", "GT", "PR":
		return true
	}
	return false
}
