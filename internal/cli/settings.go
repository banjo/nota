package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/settings"
)

func (a *app) settingsCmd() *cobra.Command {
	var withAccount bool
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Show your settings and where each one comes from",
		Long: `Show the settings that follow you to every device (theme, palette,
note mode, properties) and where each value comes from.

The highest layer that sets a key wins:

  file     ~/.config/nota/settings.toml, yours to write and keep in dotfiles
  account  your account on the sync server, written by the app when signed in
  device   the app's own storage on one machine (not visible from here)
  default  Nota's default

A key in settings.toml is locked: the app shows it but does not change
it, and the app never writes the file. With --account, the account's
values are fetched too (the vault's remote, or $NOTA_SERVER_URL).`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settings.Path()
			if err != nil {
				return err
			}
			file, warnings, err := settings.Load(path)
			if err != nil {
				return err
			}
			for i, w := range warnings {
				warnings[i] = settings.File + ": " + w
			}
			layers := []settings.Layer{}
			if withAccount {
				acc, err := a.accountValues(cmd.Context())
				if err != nil {
					return err
				}
				checked, more := settings.Check(acc)
				for _, w := range more {
					warnings = append(warnings, "account: "+w)
				}
				layers = append(layers, settings.Layer{Source: settings.SourceAccount, Values: checked})
			}
			layers = append(layers, settings.Layer{Source: settings.SourceFile, Values: file})
			vals := settings.Merge(layers...)
			if a.json {
				if warnings == nil {
					warnings = []string{}
				}
				return a.printJSON(map[string]any{"path": path, "settings": vals, "warnings": warnings})
			}
			for _, w := range warnings {
				fmt.Fprintf(a.stderr, "nota: %s\n", w)
			}
			rows := make([][]string, len(vals))
			for i, v := range vals {
				rows[i] = []string{v.Key, v.Value, string(v.Source)}
			}
			return table(a.stdout, rows)
		},
	}
	cmd.Flags().BoolVar(&withAccount, "account", false, "fetch the account's settings from the server too")
	cmd.AddCommand(a.settingsPathCmd(), a.settingsEditCmd())
	return cmd
}

// accountValues fetches the settings the signed-in account sets.
func (a *app) accountValues(ctx context.Context) (settings.Values, error) {
	acc, err := a.resolveAccount("", "")
	if err != nil {
		return nil, err
	}
	c, err := a.accountClient(acc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := c.Settings(ctx)
	if err != nil {
		return nil, syncErr(err)
	}
	return s.Settings, nil
}

func (a *app) settingsPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print where settings.toml is",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := settings.Path()
			if err != nil {
				return err
			}
			if a.json {
				_, err := os.Stat(path)
				return a.printJSON(map[string]any{"path": path, "exists": err == nil})
			}
			fmt.Fprintln(a.stdout, path)
			return nil
		},
	}
}

func (a *app) settingsEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open settings.toml in $VISUAL or $EDITOR",
		Long: `Open settings.toml in $VISUAL, else $EDITOR, else vi (notepad on
Windows). A missing file is created first, with every key commented out
and a #:schema line, so editors that know TOML can complete the keys.
The file is checked when the editor closes.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := settings.Path()
			if err != nil {
				return err
			}
			if err := createTemplate(path); err != nil {
				return err
			}
			editor := strings.Fields(firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), defaultEditor()))
			c := exec.Command(editor[0], append(editor[1:], path)...)
			c.Stdin, c.Stdout, c.Stderr = a.stdin, a.stdout, a.stderr
			if a.json {
				c.Stdout = a.stderr
			}
			if err := c.Run(); err != nil {
				return fmt.Errorf("editor %s: %w", editor[0], err)
			}
			_, warnings, err := settings.Load(path)
			if err != nil {
				return err
			}
			if a.json {
				if warnings == nil {
					warnings = []string{}
				}
				return a.printJSON(map[string]any{"path": path, "warnings": warnings})
			}
			for _, w := range warnings {
				fmt.Fprintf(a.stderr, "nota: %s: %s\n", settings.File, w)
			}
			return nil
		},
	}
}

// createTemplate writes settings.Template to path unless a file is there.
func createTemplate(path string) error {
	if _, err := os.Stat(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(settings.Template()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
