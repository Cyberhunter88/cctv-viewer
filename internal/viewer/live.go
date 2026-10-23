package viewer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type liveSources struct {
	Fast     string `json:"fast"`
	High     string `json:"high"`
	ZoomFast string `json:"zoomFast"`
	ZoomHigh string `json:"zoomHigh"`
}
type liveCamera struct {
	Enabled       bool              `json:"enabled"`
	Username      string            `json:"username"`
	Password      string            `json:"-"`
	Sources       liveSources       `json:"sources"`
	Shared        bool              `json:"shared"`
	Preload       map[string]string `json:"preload,omitempty"`
	SkipTLSVerify map[string]bool   `json:"skipTLSVerify,omitempty"`
}
type storedLiveCamera struct {
	liveCamera
	Secret string `json:"secret"`
}
type liveInput struct {
	liveCamera
	Password      *string `json:"password"`
	ClearPassword bool    `json:"clearPassword"`
}
type liveCapability struct {
	Enabled  bool `json:"enabled"`
	High     bool `json:"high"`
	Zoom     bool `json:"zoom"`
	ZoomHigh bool `json:"zoomHigh"`
}

func (c liveCamera) capability() liveCapability {
	return liveCapability{c.Enabled, c.Sources.High != "", c.Sources.ZoomFast != "", c.Sources.ZoomHigh != ""}
}
func (s *Store) liveCamera(id string) (liveCamera, error) {
	var payload string
	err := s.QueryRow(`SELECT payload FROM camera_live WHERE camera_id=?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return liveCamera{}, nil
	}
	if err != nil {
		return liveCamera{}, err
	}
	payload, err = s.openSecret(payload, secretScope("camera_live", id, "payload"))
	if err != nil {
		return liveCamera{}, err
	}
	var stored storedLiveCamera
	err = json.Unmarshal([]byte(payload), &stored)
	stored.Password = stored.Secret
	return stored.liveCamera, err
}
func (s *Store) saveLiveCamera(id string, c liveCamera) error {
	b, err := json.Marshal(storedLiveCamera{c, c.Password})
	if err != nil {
		return err
	}
	payload, err := s.sealSecret(string(b), secretScope("camera_live", id, "payload"))
	if err != nil {
		return err
	}
	_, err = s.Exec(`INSERT INTO camera_live(camera_id,payload) VALUES(?,?) ON CONFLICT(camera_id) DO UPDATE SET payload=excluded.payload`, id, payload)
	return err
}
func validateLive(c liveCamera) error {
	if len(c.Username) > 256 || len(c.Password) > 1024 {
		return errors.New("Zugangsdaten zu lang")
	}
	for key, mode := range c.Preload {
		if !liveSourceKey(key) || (mode != "off" && mode != "idle" && mode != "always") {
			return errors.New("Ungültige Vorladeeinstellung")
		}
	}
	for key := range c.SkipTLSVerify {
		if !liveSourceKey(key) {
			return errors.New("Ungültige TLS-Einstellung")
		}
	}
	if c.Enabled && c.Sources.Fast == "" {
		return errors.New("Eine schnelle Livequelle ist erforderlich")
	}
	if c.Sources.ZoomHigh != "" && c.Sources.ZoomFast == "" {
		return errors.New("Für Zoom zuerst den schnellen Stream eintragen")
	}
	for _, raw := range []string{c.Sources.Fast, c.Sources.High, c.Sources.ZoomFast, c.Sources.ZoomHigh} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\x00#") || u.User != nil || u.Hostname() == "" || (u.Scheme != "rtsp" && u.Scheme != "rtsps" && u.Scheme != "https") || strings.ContainsAny(u.Host, " \t") {
			return errors.New("RTSP- oder HTTPS-FLV-Adresse ohne Zugangsdaten oder Fragment erforderlich")
		}
		if u.Scheme == "https" && (u.Query().Has("user") || u.Query().Has("password")) {
			return errors.New("Zugangsdaten über die separaten Felder eintragen")
		}
	}
	return nil
}
func (c liveCamera) source(view, quality string) (string, error) {
	var raw string
	switch view + "/" + quality {
	case "wide/fast":
		raw = c.Sources.Fast
	case "wide/high":
		raw = c.Sources.High
	case "zoom/fast":
		raw = c.Sources.ZoomFast
	case "zoom/high":
		raw = c.Sources.ZoomHigh
	default:
		return "", errors.New("Ungültige Liveansicht")
	}
	if !c.Enabled || raw == "" {
		return "", errors.New("Livequelle nicht eingerichtet")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if c.Username != "" || c.Password != "" {
		if u.Scheme == "https" {
			q := u.Query()
			q.Set("user", c.Username)
			q.Set("password", c.Password)
			u.RawQuery = q.Encode()
		} else {
			u.User = url.UserPassword(c.Username, c.Password)
		}
	}
	return u.String(), nil
}
func (a *App) registerLive(m *http.ServeMux) {
	m.HandleFunc("GET /api/live", a.protect(false, a.liveInfo))
	m.HandleFunc("GET /api/live/{id}/ws", a.protect(false, a.liveWebSocket))
	m.HandleFunc("GET /api/cameras/{id}/live", a.protect(true, a.cameraLive))
	m.HandleFunc("PUT /api/cameras/{id}/live", a.protect(true, a.cameraLive))
	m.HandleFunc("DELETE /api/cameras/{id}/live", a.protect(true, a.cameraLive))
	m.HandleFunc("POST /api/cameras/{id}/live/test", a.protect(true, a.testLive))
	m.HandleFunc("GET /api/cameras/{id}/live/status", a.protect(true, a.liveSourceStatus))
	m.HandleFunc("GET /api/admin/live-share", a.protect(true, a.liveShare))
	m.HandleFunc("PUT /api/admin/live-share", a.protect(true, a.liveShare))
}
func (a *App) liveInfo(w http.ResponseWriter, r *http.Request) {
	cams, err := a.db.Cameras()
	if err != nil {
		fail(w, 500, "Kameras nicht lesbar")
		return
	}
	capabilities := map[string]liveCapability{}
	for _, cam := range cams {
		c, e := a.db.liveCamera(cam.ID)
		if e != nil {
			fail(w, 500, "Livekonfiguration nicht lesbar")
			return
		}
		capabilities[cam.ID] = c.capability()
	}
	ready, message := a.live.status()
	w.Header().Set("Cache-Control", "no-store")
	jsonOut(w, 200, map[string]any{"ready": ready, "message": message, "cameras": capabilities})
}
func decodeLive(w http.ResponseWriter, r *http.Request, old liveCamera) (liveCamera, error) {
	var in liveInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return old, errors.New("Ungültige Livekonfiguration")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return old, errors.New("Ungültige Livekonfiguration")
	}
	c := in.liveCamera
	c.Password = old.Password
	if in.Password != nil {
		c.Password = *in.Password
	}
	if in.ClearPassword {
		c.Password = ""
	}
	return c, validateLive(c)
}
func (a *App) cameraLive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.db.Camera(id); err != nil {
		fail(w, 404, "Kamera nicht gefunden")
		return
	}
	c, err := a.db.liveCamera(id)
	if err != nil {
		fail(w, 500, "Livekonfiguration nicht lesbar")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodPut:
		c, err = decodeLive(w, r, c)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		if err = a.db.saveLiveCamera(id, c); err != nil {
			fail(w, 500, "Livekonfiguration nicht gespeichert")
			return
		}
		a.live.invalidate(id)
	case http.MethodDelete:
		if _, err = a.db.Exec(`DELETE FROM camera_live WHERE camera_id=?`, id); err != nil {
			fail(w, 500, "Livekonfiguration nicht entfernt")
			return
		}
		a.live.invalidate(id)
		c = liveCamera{}
	}
	jsonOut(w, 200, map[string]any{"enabled": c.Enabled, "username": c.Username, "hasPassword": c.Password != "", "sources": c.Sources, "shared": c.Shared, "preload": c.Preload, "skipTLSVerify": c.SkipTLSVerify})
}
func (a *App) testLive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.db.Camera(id); err != nil {
		fail(w, 404, "Kamera nicht gefunden")
		return
	}
	old, err := a.db.liveCamera(id)
	if err != nil {
		fail(w, 500, "Livekonfiguration nicht lesbar")
		return
	}
	c, err := decodeLive(w, r, old)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !a.live.probes.TryAcquire() {
		fail(w, 429, "Es läuft bereits ein Verbindungstest")
		return
	}
	defer a.live.probes.Release()
	result := map[string]any{}
	for key, raw := range map[string]string{"wide/fast": c.Sources.Fast, "wide/high": c.Sources.High, "zoom/fast": c.Sources.ZoomFast, "zoom/high": c.Sources.ZoomHigh} {
		if raw == "" {
			continue
		}
		parts := strings.Split(key, "/")
		c.Enabled = true
		source, e := c.source(parts[0], parts[1])
		if e != nil {
			fail(w, 400, "Ungültige Quelle")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		args := []string{"-v", "error", "-protocol_whitelist", "rtsp,rtsps,http,https,tcp,tls,rtp,udp,crypto"}
		var closeRelay func()
		if strings.HasPrefix(source, "https://") {
			source, closeRelay, e = liveHTTPSRelay(ctx, source, c.SkipTLSVerify[liveKey(parts[0], parts[1])])
			if e != nil {
				cancel()
				result[key] = map[string]any{"ok": false, "message": "HTTPS-Quelle nicht verfügbar"}
				continue
			}
		} else {
			args = append(args, "-rtsp_transport", "tcp")
		}
		args = append(args, "-show_entries", "stream=codec_type,codec_name,width,height", "-of", "json", source)
		cmd := exec.CommandContext(ctx, "ffprobe", args...)
		out, e := cmd.Output()
		if closeRelay != nil {
			closeRelay()
		}
		contextError := ctx.Err()
		cancel()
		if e != nil {
			code, message := liveProbeFailure(contextError, e)
			result[key] = map[string]any{"ok": false, "code": code, "message": message}
			continue
		}
		var codecs any
		if len(out) > 65536 || json.Unmarshal(out, &codecs) != nil {
			result[key] = map[string]any{"ok": false, "message": "Codec nicht erkennbar"}
			continue
		}
		result[key] = map[string]any{"ok": true, "codecs": codecs}
	}
	jsonOut(w, 200, result)
}
