package sync

import (
	"context"
	"testing"
	"time"

	"github.com/banjo/nota/internal/protocol"
)

func TestClientSettings(t *testing.T) {
	h := newHarness(t)
	tok, _, err := h.srv.Store().CreateToken(context.Background(), h.user, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{BaseURL: h.ts.URL, Token: tok}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	revs := make(chan int64, 4)
	done := make(chan error, 1)
	go func() { done <- c.SettingsEvents(ctx, func(rev int64) { revs <- rev }) }()
	if rev := <-revs; rev != 0 {
		t.Fatalf("first event rev %d", rev)
	}

	s, err := c.UpdateSettings(ctx, protocol.SettingsPatch{Set: map[string]string{"theme": "dark"}})
	if err != nil || s.Rev != 1 || s.Settings["theme"] != "dark" {
		t.Fatalf("UpdateSettings = %+v, %v", s, err)
	}
	if rev := <-revs; rev != 1 {
		t.Fatalf("event after update: rev %d", rev)
	}
	if s, err = c.Settings(ctx); err != nil || s.Rev != 1 || s.Settings["theme"] != "dark" {
		t.Fatalf("Settings = %+v, %v", s, err)
	}
	if _, err := c.UpdateSettings(ctx, protocol.SettingsPatch{}); !IsCode(err, protocol.CodeBadRequest) {
		t.Fatalf("empty patch: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("SettingsEvents after cancel: %v", err)
	}
}
