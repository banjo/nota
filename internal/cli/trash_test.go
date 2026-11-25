package cli

import (
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "12h": 12 * time.Hour, "1.5d": 36 * time.Hour} {
		if got, err := parseAge(in); err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "soon", "-1d", "d"} {
		if _, err := parseAge(in); err == nil {
			t.Errorf("parseAge(%q) accepted", in)
		}
	}
}

func TestBlobGCIntervalFromEnv(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "off": 0, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour} {
		t.Setenv(EnvServerBlobGC, in)
		cfg, err := serverConfigFromEnv()
		if err != nil || cfg.BlobGCInterval != want {
			t.Errorf("%q: %v, %v; want %v", in, cfg.BlobGCInterval, err, want)
		}
	}
	for _, in := range []string{"soon", "10s"} {
		t.Setenv(EnvServerBlobGC, in)
		if _, err := serverConfigFromEnv(); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
