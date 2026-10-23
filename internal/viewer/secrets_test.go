package viewer

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecretsLegacyMigrationAndReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveCamera(Camera{ID: "camera", Name: "Synthetic", Folder: "camera", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err = os.Remove(filepath.Join(dir, "secrets.key")); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if err != nil {
		t.Fatal(err)
	}
	cameraSecret := "synthetic-legacy-camera-secret"
	for _, query := range []string{
		`INSERT INTO camera_live VALUES('camera','{"enabled":true,"username":"synthetic","secret":"synthetic-legacy-camera-secret","sources":{"fast":"rtsp://camera.example.org/sub"}}')`,
		`INSERT INTO live_share VALUES(1,'{"enabled":true,"username":"synthetic","secret":"synthetic-legacy-share-secret"}')`,
		`INSERT INTO settings VALUES(1,7,'{"oidcSecret":"synthetic-legacy-oidc-secret","cacheMB":64}')`,
		`INSERT INTO sessions VALUES('session','user','user','admin','local','synthetic-legacy-csrf',9999999999)`,
		`INSERT INTO oidc_flows VALUES('flow','synthetic-legacy-nonce','synthetic-legacy-verifier',9999999999)`,
	} {
		if _, err = legacy.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	legacy.Close()
	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	camera, err := store.liveCamera("camera")
	if err != nil || camera.Password != cameraSecret {
		t.Fatal("camera migration failed", err)
	}
	share, err := store.liveShare()
	if err != nil || share.Password != "synthetic-legacy-share-secret" {
		t.Fatal("share migration failed", err)
	}
	for _, column := range secretColumns {
		var value string
		if err = store.QueryRow("SELECT " + column.column + " FROM " + column.table).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(value, secretPrefix) || strings.Contains(value, "synthetic-legacy") {
			t.Fatal("plaintext remained in", column.table, column.column)
		}
		id := "1"
		if column.table == "camera_live" {
			id = "camera"
		}
		if column.table == "sessions" {
			id = "session"
		}
		if column.table == "oidc_flows" {
			id = "flow"
		}
		if _, err = store.openSecret(value, secretScope(column.table, id, column.column)); err != nil {
			t.Fatal(err)
		}
	}
	var payload string
	store.QueryRow("SELECT payload FROM settings WHERE id=1").Scan(&payload)
	plain, err := store.settingsPayload(payload)
	if err != nil || !json.Valid([]byte(plain)) || !strings.Contains(plain, "synthetic-legacy-oidc-secret") {
		t.Fatal("settings migration failed", err)
	}
	var version int
	store.QueryRow("SELECT version FROM settings").Scan(&version)
	if version != 7 {
		t.Fatal("migration changed settings version")
	}
	store.Close()
	data, err := os.ReadFile(filepath.Join(dir, "viewer.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("synthetic-legacy")) {
		t.Fatal("legacy values remain in database pages")
	}
	info, err := os.Stat(filepath.Join(dir, "secrets.key"))
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("key permissions", err)
	}
	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal("repeat migration failed", err)
	}
	store.Close()
}
func TestSecretsMissingWrongKeyAndTampering(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.sealSettings(`{"oidcSecret":"synthetic-secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Exec("INSERT INTO settings VALUES(1,1,?)", sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = store.openSecret(sealed, secretScope("camera_live", "camera", "payload")); err == nil {
		t.Fatal("ciphertext could be moved between records")
	}
	if _, err = store.openSecret(sealed[:len(sealed)-8]+"AAAAAAAA", secretScope("settings", "1", "payload")); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
	store.Close()
	keyPath := filepath.Join(dir, "secrets.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := OpenStore(dir); err == nil {
		unexpected.Close()
		t.Fatal("missing key silently replaced")
	}
	if _, err = os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("replacement key created")
	}
	if err = os.WriteFile(keyPath, bytes.Repeat([]byte{42}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := OpenStore(dir); err == nil {
		unexpected.Close()
		t.Fatal("wrong key accepted")
	}
	if err = os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal("original key could not restore database", err)
	}
	defer store.Close()
	var actual string
	store.QueryRow("SELECT payload FROM settings").Scan(&actual)
	if actual != sealed {
		t.Fatal("failed startup modified ciphertext")
	}
}
func TestSecretsMigrationIsAtomic(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Exec("INSERT INTO live_share VALUES(1,?)", `{"secret":"synthetic-plaintext"}`)
	store.Exec("INSERT INTO settings VALUES(1,1,?)", "enc:v1:broken")
	store.Close()
	if unexpected, err := OpenStore(dir); err == nil {
		unexpected.Close()
		t.Fatal("corrupted record accepted")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	db.QueryRow("SELECT payload FROM live_share").Scan(&value)
	if value != `{"secret":"synthetic-plaintext"}` {
		t.Fatal("partial migration committed")
	}
}

func TestSecretsInterruptedCleanupResumes(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Exec("INSERT INTO settings VALUES(1,1,?)", `{"oidcSecret":"synthetic-interrupted-secret"}`); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(dir, "secrets-migration.pending")
	if err = os.WriteFile(pending, []byte("v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.migrateSecrets(); err != nil || !changed {
		t.Fatal("conversion failed", err)
	}
	// Simulate termination after commit but before cleanup.
	store.Close()
	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err = os.Stat(pending); !os.IsNotExist(err) {
		t.Fatal("cleanup marker retained")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "viewer.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("synthetic-interrupted-secret")) {
		t.Fatal("restart retained plaintext pages")
	}
}
