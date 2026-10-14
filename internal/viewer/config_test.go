package viewer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaultsAndValidation(t *testing.T) {
	for _, key := range []string{"CTV_BASE_URL", "CTV_DEV_HTTP", "CTV_LOCAL_ADMIN", "CTV_OIDC_ISSUER", "CTV_OIDC_CLIENT_ID", "CTV_OIDC_CLIENT_SECRET", "CTV_OIDC_CLIENT_SECRET_FILE", "CTV_MAX_TRANSCODERS", "CTV_CACHE_MB", "CTV_SCAN_INTERVAL_SECONDS", "CTV_TRUSTED_PROXIES"} {
		t.Setenv(key, "")
	}
	if _, e := LoadConfig(); e == nil {
		t.Fatal("plaintext must require explicit developer flag")
	}
	t.Setenv("CTV_BASE_URL", "https://cctv.example.org")
	c, e := LoadConfig()
	if e != nil || c.MaxTranscoders != 1 || c.CacheBytes != 512<<20 {
		t.Fatal(c, e)
	}
	for _, entry := range []struct{ key, value string }{{"CTV_MAX_TRANSCODERS", "0"}, {"CTV_CACHE_MB", "1"}, {"CTV_SCAN_INTERVAL_SECONDS", "1"}, {"CTV_TRUSTED_PROXIES", "invalid"}, {"CTV_BASE_URL", "https://cctv.example.org/subpath"}, {"CTV_OIDC_ISSUER", "http://example.org"}} {
		t.Run(entry.key, func(t *testing.T) {
			t.Setenv(entry.key, entry.value)
			if _, e := LoadConfig(); e == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	t.Setenv("CTV_LOCAL_ADMIN", "false")
	if _, e := LoadConfig(); e == nil {
		t.Fatal("no login configured")
	}
}
func TestSecretFileAndPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(p, []byte("secret\n"), 0600)
	t.Setenv("CTV_TEST_SECRET_FILE", p)
	t.Setenv("CTV_TEST_SECRET", "")
	s, e := Secret("CTV_TEST_SECRET")
	if e != nil || s != "secret" {
		t.Fatal(s, e)
	}
	t.Setenv("CTV_TEST_SECRET", "other")
	if _, e = Secret("CTV_TEST_SECRET"); e == nil {
		t.Fatal("conflicting secret sources accepted")
	}
	dir := t.TempDir()
	db, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	st, _ := os.Stat(filepath.Join(dir, "viewer.db"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("database permissions", st.Mode())
	}
	if _, e := HashPassword("😀😀😀😀"); e == nil {
		t.Fatal("password must have 14 characters, not just bytes")
	}
}
