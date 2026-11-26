package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/export"
)

func (a *app) exportCmd() *cobra.Command {
	var format, out string
	var force bool
	cmd := &cobra.Command{
		Use:   "export [note|folder]",
		Short: "Export a note, a folder or the vault as HTML, Markdown or a zip",
		Long: `Export a note, a folder, or (without an argument) the whole vault.

  html  a web page in Reading mode's look, images inside it; a folder is
        a zip of pages that link to each other
  md    Markdown for apps that don't read Nota's syntax: wikilinks become
        Markdown links, %%comments%%, block ids and inline footnotes go;
        a note that shows images, and a folder, are a zip with the images
  zip   the notes and the images they show as they are, at their paths
        in the vault

A link to a note that is not part of the export is left as its text.

The file is written to the current folder under the note's or folder's
name, or to -o (a file, or a folder to write into; - is standard
output). An existing file is only replaced with --force.`,
		Example: `  nota export Projekt/Plan.md
  nota export Projekt --format md -o ~/Desktop
  nota export --format zip -o backup.zip
  nota export Plan --format html -o - | open -f -a Safari`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			f := export.Format(format)
			if !slices.Contains(export.Formats, f) {
				return usageErr("--format must be html, md or zip")
			}
			if err := a.openVault(false); err != nil {
				return err
			}
			ix, err := a.index()
			if err != nil {
				return err
			}
			target := ""
			if len(args) == 1 {
				target = strings.Trim(args[0], "/")
				if folders, err := a.vault.Folders(); err != nil {
					return err
				} else if !slices.Contains(folders, target) {
					if target, err = a.resolve(args[0]); err != nil {
						return err
					}
				}
			}
			e, err := export.New(a.vault, ix, target, export.Options{
				Format: f,
				Name:   filepath.Base(a.vault.Root()),
			})
			if errors.Is(err, export.ErrNotFound) {
				return &Error{Code: ExitNotFound, Kind: "not_found", Message: err.Error()}
			} else if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "format", format, "notes", e.Notes)

			if out == "-" {
				_, err := e.WriteTo(a.stdout)
				return err
			}
			dest := e.Name
			if out != "" {
				dest = out
				if st, err := os.Stat(out); err == nil && st.IsDir() {
					dest = filepath.Join(out, e.Name)
				}
			}
			if _, err := os.Stat(dest); err == nil && !force {
				return &Error{Code: ExitConflict, Kind: "exists", Message: dest + " exists; pass --force to replace it"}
			}
			n, err := e.Save(dest)
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(struct {
					Path  string `json:"path"`
					Notes int    `json:"notes"`
					Bytes int64  `json:"bytes"`
				}{dest, e.Notes, n})
			}
			fmt.Fprintf(a.stderr, "Exported %s to %s\n", plural(e.Notes, "note"), dest)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&format, "format", "html", "html, md or zip")
	fl.StringVarP(&out, "output", "o", "", "the file or folder to write to; - for standard output")
	fl.BoolVar(&force, "force", false, "replace an existing file")
	return cmd
}
