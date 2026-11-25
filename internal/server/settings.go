package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/settings"
)

// The account layer of the user's settings (docs/api.md, "Settings"):
// what the desktop and web apps write when the user changes a setting
// while signed in, so the choice follows them to every device. The
// server stores keys it does not know too, as long as they look like
// keys: a newer app may write them.

// AccountSettings returns the settings the user's account sets.
func (s *Store) AccountSettings(ctx context.Context, userID int64) (protocol.AccountSettings, error) {
	return accountSettings(ctx, s.db, userID)
}

func accountSettings(ctx context.Context, q querier, userID int64) (protocol.AccountSettings, error) {
	out := protocol.AccountSettings{Settings: map[string]string{}}
	var data string
	err := q.QueryRowContext(ctx, `SELECT data, rev FROM user_settings WHERE user_id = ?`, userID).Scan(&data, &out.Rev)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(data), &out.Settings); err != nil {
		return out, fmt.Errorf("server: settings of user %d: %w", userID, err)
	}
	return out, nil
}

// checkPatch refuses keys that cannot be settings and values that are
// too long.
func checkPatch(p protocol.SettingsPatch) error {
	if len(p.Set)+len(p.Unset) == 0 {
		return badRequest("nothing to change: give set or unset")
	}
	for k, v := range p.Set {
		if !settings.ValidName(k) {
			return badRequest("%q is not a setting key", k)
		}
		if len(v) > settings.MaxValueLen {
			return badRequest("the value of %s is longer than %d bytes", k, settings.MaxValueLen)
		}
	}
	for _, k := range p.Unset {
		if !settings.ValidName(k) {
			return badRequest("%q is not a setting key", k)
		}
	}
	return nil
}

// UpdateAccountSettings applies a patch: Set keys first, then Unset. A
// patch that changes nothing keeps the revision.
func (s *Store) UpdateAccountSettings(ctx context.Context, userID int64, p protocol.SettingsPatch) (protocol.AccountSettings, bool, error) {
	if err := checkPatch(p); err != nil {
		return protocol.AccountSettings{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.AccountSettings{}, false, err
	}
	defer tx.Rollback()
	cur, err := accountSettings(ctx, tx, userID)
	if err != nil {
		return cur, false, err
	}
	changed := false
	for k, v := range p.Set {
		if old, ok := cur.Settings[k]; !ok || old != v {
			cur.Settings[k] = v
			changed = true
		}
	}
	for _, k := range p.Unset {
		if _, ok := cur.Settings[k]; ok {
			delete(cur.Settings, k)
			changed = true
		}
	}
	if !changed {
		return cur, false, nil
	}
	if len(cur.Settings) > settings.MaxKeys {
		return protocol.AccountSettings{}, false, badRequest("an account keeps at most %d settings", settings.MaxKeys)
	}
	data, err := json.Marshal(cur.Settings)
	if err != nil {
		return cur, false, err
	}
	cur.Rev++
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, data, rev, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET data = excluded.data, rev = excluded.rev, updated_at = excluded.updated_at`,
		userID, string(data), cur.Rev, ms(s.now())); err != nil {
		return cur, false, err
	}
	return cur, true, tx.Commit()
}

// userKey is a user's broker topic, apart from the vaults' ids.
func userKey(userID int64) string { return "user:" + strconv.FormatInt(userID, 10) }

func (s *Server) settingsRoutes() {
	s.route("GET /v1/account/settings", routeOpts{auth: authAny}, s.getSettings)
	s.route("PATCH /v1/account/settings", routeOpts{auth: authAny}, s.patchSettings)
	s.route("GET /v1/account/events", routeOpts{auth: authAny}, s.accountEvents)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.store.AccountSettings(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) patchSettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	var req protocol.SettingsPatch
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	out, changed, err := s.store.UpdateAccountSettings(r.Context(), p.UserID, req)
	if err != nil {
		return err
	}
	info(r).attrs = append(info(r).attrs, "settings_rev", out.Rev, "settings_changed", changed)
	if changed {
		s.broker.publish(userKey(p.UserID), out.Rev)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// accountEvents streams "settings" nudges: the current revision first,
// then one per change, from any device.
func (s *Server) accountEvents(w http.ResponseWriter, r *http.Request, p Principal) error {
	cur, err := s.store.AccountSettings(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	ch, cancel := s.broker.subscribe(userKey(p.UserID))
	defer cancel()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(rev int64) error {
		data, _ := json.Marshal(protocol.SettingsEvent{Rev: rev})
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", protocol.EventSettings, data); err != nil {
			return err
		}
		return rc.Flush()
	}
	if send(cur.Rev) != nil {
		return nil
	}
	tick := time.NewTicker(s.cfg.KeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-s.broker.done:
			return nil
		case rev := <-ch:
			if send(rev) != nil {
				return nil
			}
		case <-tick.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil || rc.Flush() != nil {
				return nil
			}
		}
	}
}
