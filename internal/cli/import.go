package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/importer"
)

func (a *app) importCmd() *cobra.Command {
	var folder string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import <markdown|obsidian|apple-notes> [path]",
		Short: "Import notes from a folder, a zip, Obsidian or Apple Notes",
		Long: `Import notes into the vault.

  markdown     a folder or a zip of Markdown files, or one file: exports
               of Bear, Notion, Logseq and other apps
  obsidian     an Obsidian vault's folder (the same as markdown)
  apple-notes  every note in Apple Notes, with its folders and images
               (macOS; it asks for permission to read Notes)

Everything goes into one folder: by default a new one named after the
source, else --folder (/ for the top of the vault). An import never
replaces a file. A name that is taken gets " 2", a name with characters
Nota keeps out of file names is cleaned, and links between the imported
notes are rewritten to keep working. Hidden files, such as .obsidian,
are left out. Dates in frontmatter become created and updated, taken from
the files' times when a note has none.

--dry-run shows what would happen without writing anything.`,
		Example: `  nota import obsidian ~/Documents/Obsidian
  nota import markdown ~/Downloads/Bear.zip --folder Bear
  nota import apple-notes --dry-run`,
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: []string{"markdown", "obsidian", "apple-notes"},
		RunE: func(cmd *cobra.Command, args []string) error {
			var files []importer.File
			var name string
			switch args[0] {
			case "markdown", "obsidian":
				if len(args) != 2 {
					return usageErr("give the folder or zip to import: nota import %s <path>", args[0])
				}
				var err error
				files, name, err = importer.Open(args[1])
				switch {
				case errors.Is(err, fs.ErrNotExist):
					return &Error{Code: ExitNotFound, Kind: "not_found", Message: fmt.Sprintf("%s does not exist", args[1])}
				case errors.Is(err, importer.ErrNoFiles):
					return &Error{Code: ExitNotFound, Kind: "not_found", Message: fmt.Sprintf("%s has nothing to import", args[1])}
				case err != nil:
					return err
				}
			case "apple-notes":
				if len(args) != 1 {
					return usageErr("apple-notes takes no path: it reads Apple Notes")
				}
				fmt.Fprintln(a.stderr, "Reading Apple Notes…")
				notes, err := importer.AppleNotes(context.Background())
				if err != nil {
					return err
				}
				files, name = importer.AppleNotesFiles(notes), "Apple Notes"
			default:
				return usageErr("import from markdown, obsidian or apple-notes, not %q", args[0])
			}
			if err := a.openVault(false); err != nil {
				return err
			}
			if !cmd.Flags().Changed("folder") {
				var err error
				if folder, err = importer.DefaultFolder(a.vault, name); err != nil {
					return err
				}
			}
			r, err := importer.Run(a.vault, files, importer.Options{Folder: folder, DryRun: dryRun})
			if r != nil {
				a.attrs = append(a.attrs, "source", args[0], "notes", r.Notes, "files", r.Files, "skipped", r.Skipped)
			}
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(r)
			}
			a.printImport(r)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&folder, "folder", "", "the vault folder to import into; / for the top")
	fl.BoolVar(&dryRun, "dry-run", false, "show what would be imported without writing")
	return cmd
}

func (a *app) printImport(r *importer.Report) {
	where := r.Folder
	if where == "" {
		where = "the vault"
	}
	verb := "Imported"
	if r.DryRun {
		verb = "Would import"
	}
	what := plural(r.Notes, "note")
	if r.Files > 0 {
		what += " and " + plural(r.Files, "file")
	}
	fmt.Fprintf(a.stdout, "%s %s into %s\n", verb, what, where)
	for _, s := range []importer.Status{importer.Renamed, importer.Skipped} {
		first := true
		for _, it := range r.Items {
			if it.Status != s {
				continue
			}
			if first {
				fmt.Fprintf(a.stdout, "\n%s%s:\n", strings.ToUpper(string(s[:1])), s[1:])
				first = false
			}
			if it.Path != "" {
				fmt.Fprintf(a.stdout, "  %s → %s (%s)\n", it.Source, it.Path, it.Reason)
			} else {
				fmt.Fprintf(a.stdout, "  %s (%s)\n", it.Source, it.Reason)
			}
		}
	}
	first := true
	for _, it := range r.Items {
		if it.Detail == "" {
			continue
		}
		if first {
			fmt.Fprintln(a.stdout, "\nNotes:")
			first = false
		}
		fmt.Fprintf(a.stdout, "  %s: %s\n", it.Path, it.Detail)
	}
	if r.Links > 0 {
		fmt.Fprintf(a.stdout, "\nRewrote %s to keep them pointing at the imported notes.\n", plural(r.Links, "link"))
	}
}
