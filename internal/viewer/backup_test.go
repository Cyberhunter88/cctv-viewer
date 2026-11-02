package viewer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRoundTripOmitsSecretsAndRestoresCameras(t *testing.T) {
	a := testApp(t)
	os.Mkdir(filepath.Join(a.cfg.RecordingsDir, "front"), 0700)
	cookie, s := login(t, a)
	if w := request(a, "GET", "/api/admin/backup", nil, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	cam := Camera{Name: "Front", Folder: "front", Pattern: "{YYYY}{MM}{DD}-{hh}{mm}{ss}.mp4", Timezone: "Europe/Berlin"}
	if w := request(a, "POST", "/api/cameras", cam, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request(a, "GET", "/api/admin/backup", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), "a long secure password") {
		t.Fatal("backup must not contain credentials")
	}
	var b configBackup
	if e := json.Unmarshal(w.Body.Bytes(), &b); e != nil || len(b.Cameras) != 1 || len(b.Users) != 1 {
		t.Fatal(e, w.Body.String())
	}
	if w = request(a, "DELETE", "/api/cameras/"+b.Cameras[0].ID, nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(a, "POST", "/api/admin/backup", b, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cams, _ := a.db.Cameras()
	if len(cams) != 1 || cams[0].Name != "Front" {
		t.Fatal(cams)
	}
	b.Format = 99
	if w = request(a, "POST", "/api/admin/backup", b, cookie, s.CSRF); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
