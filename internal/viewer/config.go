package viewer

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type StreamProfile struct {
	Scale   int `json:"scale"`
	FPS     int `json:"fps"`
	Bitrate int `json:"bitrate"`
}
type Config struct {
	AutoScan                                             bool
	AutoScanMinutes                                      int
	HLSTempBytes                                         int64
	Balanced, Fast                                       StreamProfile
	Address, DataDir, RecordingsDir, BaseURL             string
	LocalAdmin                                           bool
	OIDCIssuer, OIDCClientID, OIDCSecret, OIDCGroupClaim string
	AllowedUsers, AllowedGroups, AdminUsers, AdminGroups []string
	TrustedProxies                                       []netip.Prefix
	SessionTTL                                           time.Duration
	ScanInterval                                         time.Duration
	CacheBytes                                           int64
	MaxTranscoders                                       int
	DevHTTP                                              bool
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func list(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func Secret(name string) (string, error) {
	value, path := os.Getenv(name), os.Getenv(name+"_FILE")
	if value != "" && path != "" {
		return "", fmt.Errorf("%s und %s_FILE schließen sich aus", name, name)
	}
	if path != "" {
		b, e := os.ReadFile(path)
		return strings.TrimSpace(string(b)), e
	}
	return value, nil
}
func LoadConfig() (Config, error) {
	c := Config{AutoScanMinutes: 60, HLSTempBytes: 256 << 20, Balanced: StreamProfile{50, 15, 1200}, Fast: StreamProfile{30, 8, 450}, Address: env("CTV_ADDRESS", ":8080"), DataDir: env("CTV_DATA_DIR", "/data"), RecordingsDir: env("CTV_RECORDINGS_DIR", "/recordings"), BaseURL: env("CTV_BASE_URL", "http://localhost:8080"), LocalAdmin: env("CTV_LOCAL_ADMIN", "true") == "true", OIDCIssuer: os.Getenv("CTV_OIDC_ISSUER"), OIDCClientID: os.Getenv("CTV_OIDC_CLIENT_ID"), OIDCGroupClaim: env("CTV_OIDC_GROUP_CLAIM", "groups"), AllowedUsers: list(os.Getenv("CTV_OIDC_ALLOWED_SUBJECTS")), AllowedGroups: list(os.Getenv("CTV_OIDC_ALLOWED_GROUPS")), AdminUsers: list(os.Getenv("CTV_OIDC_ADMIN_SUBJECTS")), AdminGroups: list(os.Getenv("CTV_OIDC_ADMIN_GROUPS")), SessionTTL: 8 * time.Hour, ScanInterval: 5 * time.Minute, DevHTTP: env("CTV_DEV_HTTP", "false") == "true"}
	var err error
	c.OIDCSecret, err = Secret("CTV_OIDC_CLIENT_SECRET")
	if err != nil {
		return c, err
	}
	for _, p := range list(os.Getenv("CTV_TRUSTED_PROXIES")) {
		n, e := netip.ParsePrefix(p)
		if e != nil {
			return c, e
		}
		c.TrustedProxies = append(c.TrustedProxies, n)
	}
	minutes, err := strconv.Atoi(env("CTV_SESSION_MINUTES", "480"))
	if err != nil || minutes < 15 || minutes > 1440 {
		return c, errors.New("CTV_SESSION_MINUTES muss zwischen 15 und 1440 liegen")
	}
	c.SessionTTL = time.Duration(minutes) * time.Minute
	seconds, err := strconv.Atoi(env("CTV_SCAN_INTERVAL_SECONDS", "300"))
	if err != nil || seconds < 30 || seconds > 86400 {
		return c, errors.New("CTV_SCAN_INTERVAL_SECONDS muss zwischen 30 und 86400 liegen")
	}
	c.ScanInterval = time.Duration(seconds) * time.Second
	n, err := strconv.Atoi(env("CTV_MAX_TRANSCODERS", "0"))
	if err != nil || n < 0 || n > 64 {
		return c, errors.New("CTV_MAX_TRANSCODERS muss zwischen 0 und 64 liegen")
	}
	c.MaxTranscoders = n
	hlsMB, e := strconv.ParseInt(env("CTV_HLS_TEMP_MB", "256"), 10, 64)
	if e != nil || hlsMB < 16 || hlsMB > 4096 {
		return c, errors.New("CTV_HLS_TEMP_MB muss zwischen 16 und 4096 liegen")
	}
	c.HLSTempBytes = hlsMB << 20
	mb, err := strconv.ParseInt(env("CTV_CACHE_MB", "512"), 10, 64)
	if err != nil || mb < 32 || mb > 65536 {
		return c, errors.New("CTV_CACHE_MB muss zwischen 32 und 65536 liegen")
	}
	c.CacheBytes = mb << 20
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, errors.New("CTV_BASE_URL muss eine absolute URL ohne Unterpfad sein")
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	if u.Scheme != "https" && !(u.Scheme == "http" && c.DevHTTP) {
		return c, errors.New("HTTPS erforderlich; nur für lokale Entwicklung CTV_DEV_HTTP=true setzen")
	}
	if c.OIDCIssuer != "" {
		issuer, e := url.Parse(c.OIDCIssuer)
		if e != nil || issuer.Scheme != "https" || issuer.Host == "" {
			return c, errors.New("OIDC-Issuer muss HTTPS verwenden")
		}
		if c.OIDCClientID == "" || c.OIDCSecret == "" {
			return c, errors.New("OIDC-Client-ID und Secret fehlen")
		}
	}
	if !c.LocalAdmin && c.OIDCIssuer == "" {
		return c, errors.New("mindestens eine Anmeldemethode ist erforderlich")
	}
	return c, nil
}
