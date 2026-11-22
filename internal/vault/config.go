package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// EnvVault is the environment variable that points at the vault.
const EnvVault = "NOTA_VAULT"

// DefaultInbox is the folder new notes land in when none is given.
const DefaultInbox = "Inbox"

// Config is the user configuration in ~/.config/nota/config.toml.
type Config struct {
	// Vault is the vault directory. A leading "~/" means the home directory.
	Vault string `toml:"vault"`
	// Inbox is the vault-relative folder for unsorted notes.
	Inbox string `toml:"inbox"`
}

// ConfigPath returns the config file location: $XDG_CONFIG_HOME/nota/config.toml,
// or ~/.config/nota/config.toml. The same path is used on every OS so the
// CLI is configured the same way on macOS, Linux and Windows.
func ConfigPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "nota", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("vault: find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "nota", "config.toml"), nil
}

// LoadConfig reads the config file at path. A missing file is not an error
// and yields the zero Config.
func LoadConfig(path string) (Config, error) {
	var c Config
	_, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("vault: read config %s: %w", path, err)
	}
	return c, nil
}

// Location is the result of vault discovery.
type Location struct {
	// Dir is the absolute vault directory.
	Dir string
	// Source says where Dir came from: "flag", "env", "config" or "default".
	Source string
	// Config is the loaded user configuration.
	Config Config
}

// Discover finds the vault directory. The order is: the flag value (if
// not empty), $NOTA_VAULT, the `vault` key in the config file, ~/Notes.
// The directory does not have to exist yet.
func Discover(flag string) (Location, error) {
	cfgPath, err := ConfigPath()
	if err != nil {
		return Location{}, err
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return Location{}, err
	}
	loc := Location{Config: cfg}
	switch {
	case flag != "":
		loc.Dir, loc.Source = flag, "flag"
	case os.Getenv(EnvVault) != "":
		loc.Dir, loc.Source = os.Getenv(EnvVault), "env"
	case cfg.Vault != "":
		loc.Dir, loc.Source = cfg.Vault, "config"
	default:
		loc.Dir, loc.Source = "~/Notes", "default"
	}
	if loc.Dir, err = expandHome(loc.Dir); err != nil {
		return Location{}, err
	}
	if loc.Dir, err = filepath.Abs(loc.Dir); err != nil {
		return Location{}, fmt.Errorf("vault: %w", err)
	}
	return loc, nil
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("vault: find home directory: %w", err)
	}
	return filepath.Join(home, p[1:]), nil
}
