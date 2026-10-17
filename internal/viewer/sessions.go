package viewer

import (
	"crypto/subtle"
	"net/http"
	"time"
)

// sessionManagementID is a domain-separated, irreversible identifier. Neither
// the cookie nor the database's authentication hash leaves the server.
func sessionManagementID(hash string) string { return digest("session-management\x00" + hash) }

type managedSession struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Role    string `json:"role"`
	Expires int64  `json:"expires"`
	Current bool   `json:"current"`
}

func (a *App) registerSessions(m *http.ServeMux) {
	m.HandleFunc("GET /api/sessions", a.protect(false, a.listSessions(false)))
	m.HandleFunc("GET /api/admin/sessions", a.protect(true, a.listSessions(true)))
	m.HandleFunc("DELETE /api/sessions/{id}", a.protect(false, a.revokeSession(false)))
	m.HandleFunc("DELETE /api/admin/sessions/{id}", a.protect(true, a.revokeSession(true)))
	m.HandleFunc("DELETE /api/sessions", a.protect(false, a.revokeOwnSessions))
}

func (a *App) listSessions(all bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := r.Context().Value(sessionKey).(Session)
		query := `SELECT hash,name,kind,role,expires FROM sessions WHERE expires>?`
		args := []any{time.Now().Unix()}
		if !all {
			query += ` AND kind=? AND subject=?`
			args = append(args, s.Kind, s.Subject)
		}
		query += ` ORDER BY expires DESC,hash`
		rows, err := a.db.QueryContext(r.Context(), query, args...)
		if err != nil {
			fail(w, 500, "Sitzungen nicht lesbar")
			return
		}
		defer rows.Close()
		cookie, _ := r.Cookie("ctv_session")
		currentHash := digest(cookie.Value)
		out := []managedSession{}
		for rows.Next() {
			var entry managedSession
			var hash string
			if err = rows.Scan(&hash, &entry.Name, &entry.Kind, &entry.Role, &entry.Expires); err != nil {
				fail(w, 500, "Sitzungen nicht lesbar")
				return
			}
			entry.ID = sessionManagementID(hash)
			entry.Current = subtle.ConstantTimeCompare([]byte(hash), []byte(currentHash)) == 1
			out = append(out, entry)
		}
		if rows.Err() != nil {
			fail(w, 500, "Sitzungen nicht lesbar")
			return
		}
		jsonOut(w, 200, out)
	}
}

func (a *App) revokeSession(all bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := r.Context().Value(sessionKey).(Session)
		query := `SELECT hash FROM sessions WHERE expires>?`
		args := []any{time.Now().Unix()}
		if !all {
			query += ` AND kind=? AND subject=?`
			args = append(args, s.Kind, s.Subject)
		}
		rows, err := a.db.QueryContext(r.Context(), query, args...)
		if err != nil {
			fail(w, 500, "Sitzung nicht lesbar")
			return
		}
		target := ""
		for rows.Next() {
			var hash string
			if err = rows.Scan(&hash); err != nil {
				break
			}
			if subtle.ConstantTimeCompare([]byte(sessionManagementID(hash)), []byte(r.PathValue("id"))) == 1 {
				target = hash
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil || rowErr != nil {
			fail(w, 500, "Sitzung nicht lesbar")
			return
		}
		if target == "" {
			fail(w, 404, "Sitzung nicht gefunden")
			return
		}
		// Repeat ownership and expiry checks in the mutation itself.
		query = `DELETE FROM sessions WHERE hash=? AND expires>?`
		args = []any{target, time.Now().Unix()}
		if !all {
			query += ` AND kind=? AND subject=?`
			args = append(args, s.Kind, s.Subject)
		}
		result, err := a.db.ExecContext(r.Context(), query, args...)
		if err != nil {
			fail(w, 500, "Sitzung konnte nicht beendet werden")
			return
		}
		n, err := result.RowsAffected()
		if err != nil {
			fail(w, 500, "Sitzung konnte nicht beendet werden")
			return
		}
		if n == 0 {
			fail(w, 404, "Sitzung nicht gefunden")
			return
		}
		cookie, _ := r.Cookie("ctv_session")
		if target == digest(cookie.Value) {
			a.cookie(w, "ctv_session", "", -1)
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	}
}

func (a *App) revokeOwnSessions(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey).(Session)
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE kind=? AND subject=?`, s.Kind, s.Subject); err != nil {
		fail(w, 500, "Sitzungen konnten nicht beendet werden")
		return
	}
	a.cookie(w, "ctv_session", "", -1)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
