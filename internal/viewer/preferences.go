package viewer

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
)

// Preference fields are explicitly bounded; this is not arbitrary user storage.
func validPreferences(values map[string]json.RawMessage) bool {
	if values == nil {
		return false
	}
	enums := map[string][]string{
		"layout":  {"auto", "1x1", "2x1", "2x2", "3x2", "3x3", "4x3", "4x4", "hotspot", "custom"},
		"quality": {"native", "balanced", "fast"}, "speed": {"0.25", "0.5", "1", "2", "4", "8", "16"},
		"preload": {"metadata", "auto"}, "aspect": {"4/3", "16/9", "auto", "fill"},
		"density": {"comfortable", "compact"}, "theme": {"system", "light", "dark"},
		"liveLayout": {"auto", "1x1", "2x2", "3x3", "4x4"},
	}
	for key, raw := range values {
		if string(raw) == "null" {
			if key == "railCollapsed" {
				continue
			}
			return false
		}
		if allowed, ok := enums[key]; ok {
			var value string
			if json.Unmarshal(raw, &value) != nil || !slices.Contains(allowed, value) {
				return false
			}
			continue
		}
		switch key {
		case "cameras", "order", "liveCameras", "liveOrder":
			var ids []string
			if json.Unmarshal(raw, &ids) != nil || ids == nil || len(ids) > 1024 {
				return false
			}
			for _, id := range ids {
				if len(id) > 256 {
					return false
				}
			}
		case "hotspot":
			var id string
			if json.Unmarshal(raw, &id) != nil || len(id) > 256 {
				return false
			}
		case "autoHotspot", "collapsed", "railCollapsed", "showNames", "showTimestamps", "showCodec", "showOverview", "showThumbnails", "shortcuts":
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return false
			}
		case "columns", "rows", "timelineHeight", "seekStep", "originalQualityVersion":
			var value float64
			if json.Unmarshal(raw, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return false
			}
			switch key {
			case "columns", "rows":
				if value != math.Trunc(value) || value < 1 || value > 8 {
					return false
				}
			case "timelineHeight":
				if value < 100 || value > 650 {
					return false
				}
			case "seekStep":
				if value != 5 && value != 10 && value != 30 {
					return false
				}
			case "originalQualityVersion":
				if value != 1 {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func (a *App) preferences(w http.ResponseWriter, r *http.Request) {
	owner := preferenceOwner(r.Context().Value(sessionKey).(Session))
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		var payload string
		err := a.db.QueryRowContext(r.Context(), `SELECT payload FROM user_preferences WHERE owner=?`, owner).Scan(&payload)
		if errors.Is(err, sql.ErrNoRows) {
			jsonOut(w, 200, nil)
			return
		}
		if err != nil {
			fail(w, 500, "Persönliche Einstellungen nicht lesbar")
			return
		}
		jsonOut(w, 200, json.RawMessage(payload))
		return
	}
	var changes map[string]json.RawMessage
	if !decodeLimit(w, r, &changes, 64*1024) {
		return
	}
	if !validPreferences(changes) {
		fail(w, 400, "Ungültige persönliche Einstellungen")
		return
	}
	// One SQL statement merges only changed fields. SQLite serializes writers,
	// so concurrent devices cannot overwrite unrelated preferences.
	payload, _ := json.Marshal(changes)
	if r.Header.Get("If-None-Match") == "*" {
		result, err := a.db.ExecContext(r.Context(), `INSERT INTO user_preferences(owner,payload) VALUES(?,?) ON CONFLICT(owner) DO NOTHING`, owner, string(payload))
		if err != nil {
			fail(w, 500, "Persönliche Einstellungen nicht gespeichert")
			return
		}
		count, err := result.RowsAffected()
		if err != nil {
			fail(w, 500, "Persönliche Einstellungen nicht gespeichert")
			return
		}
		if count == 0 {
			fail(w, 412, "Persönliche Einstellungen bereits vorhanden")
			return
		}
	} else {
		_, err := a.db.ExecContext(r.Context(), `INSERT INTO user_preferences(owner,payload) VALUES(?,?) ON CONFLICT(owner) DO UPDATE SET payload=json_patch(user_preferences.payload,excluded.payload)`, owner, string(payload))
		if err != nil {
			fail(w, 500, "Persönliche Einstellungen nicht gespeichert")
			return
		}
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
