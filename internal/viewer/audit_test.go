package viewer

import (
	"encoding/json"
	"strings"
	"testing"
)

type auditResponse struct {
	Entries []AuditEntry `json:"entries"`
	HasMore bool         `json:"hasMore"`
}

func TestAuditRecordsLoginsAndAdminActionsWithoutSecrets(t *testing.T) {
	a := testApp(t)
	request(a, "POST", "/auth/local", map[string]string{"username": "test", "password": "wrong wrong wrong"}, nil, "")
	cookie, s := login(t, a)
	if w := request(a, "POST", "/api/admin/users", map[string]any{"username": "viewer1", "role": "viewer", "password": "another long password", "create": true}, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(a, "GET", "/api/admin/audit", nil, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(a, "GET", "/api/admin/audit", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "another long password") || strings.Contains(w.Body.String(), "a long secure password") {
		t.Fatal("audit must not contain passwords")
	}
	var out auditResponse
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, x := range out.Entries {
		seen[x.Action+":"+map[bool]string{true: "ok", false: "fail"}[x.OK]] = true
		if x.Action == "user.save" && x.Target != "viewer1" {
			t.Fatal(x)
		}
	}
	for _, want := range []string{"login.local:fail", "login.local:ok", "user.save:ok"} {
		if !seen[want] {
			t.Fatal("missing", want, seen)
		}
	}
}

func TestAuditCleanupRemovesOldEntries(t *testing.T) {
	a := testApp(t)
	a.db.Exec(`INSERT INTO audit_log(ts,actor,action,target,ip,ok) VALUES(1,'x','old','','',1)`)
	a.db.Cleanup(a.ctx)
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='old'`).Scan(&n)
	if n != 0 {
		t.Fatal(n)
	}
}
