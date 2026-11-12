package viewer

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

const auditRetention = 180 * 24 * time.Hour

type auditKey struct{}

// auditTarget lets a handler report the object it acted on (for example a username).
type auditTarget struct{ value string }

// AuditEntry is one security-relevant event. It never contains secrets.
type AuditEntry struct {
	ID     int64  `json:"id"`
	Time   int64  `json:"time"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	IP     string `json:"ip"`
	OK     bool   `json:"ok"`
}

// recordAudit stores an event; failures to write must never break the request.
func (a *App) recordAudit(ctx context.Context, actor, action, target, ip string, ok bool) {
	if len(actor) > 200 {
		actor = actor[:200]
	}
	if len(target) > 200 {
		target = target[:200]
	}
	a.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO audit_log(ts,actor,action,target,ip,ok) VALUES(?,?,?,?,?,?)`, time.Now().Unix(), actor, action, target, ip, ok)
}

func setAuditTarget(r *http.Request, value string) {
	if t, ok := r.Context().Value(auditKey{}).(*auditTarget); ok {
		t.value = value
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int)        { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// audit wraps a handler that runs behind protect and records its outcome.
func (a *App) audit(action string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := &auditTarget{value: r.PathValue("name")}
		if t.value == "" {
			t.value = r.PathValue("id")
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		h(rec, r.WithContext(context.WithValue(r.Context(), auditKey{}, t)))
		actor := ""
		if s, ok := r.Context().Value(sessionKey).(Session); ok {
			actor = s.Name
		}
		a.recordAudit(r.Context(), actor, action, t.value, a.clientIP(r), rec.status < 400)
	}
}

func (a *App) auditList(w http.ResponseWriter, r *http.Request) {
	limit, offset := 100, 0
	if v, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && v > 0 && v <= 500 {
		limit = v
	}
	if v, e := strconv.Atoi(r.URL.Query().Get("offset")); e == nil && v > 0 {
		offset = v
	}
	rows, e := a.db.reader().QueryContext(r.Context(), `SELECT id,ts,actor,action,target,ip,ok FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`, limit+1, offset)
	if e != nil {
		fail(w, 500, "Protokoll nicht lesbar")
		return
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var x AuditEntry
		if e = rows.Scan(&x.ID, &x.Time, &x.Actor, &x.Action, &x.Target, &x.IP, &x.OK); e != nil {
			fail(w, 500, "Protokoll nicht lesbar")
			return
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		fail(w, 500, "Protokoll nicht lesbar")
		return
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	jsonOut(w, 200, map[string]any{"entries": out, "hasMore": more})
}
