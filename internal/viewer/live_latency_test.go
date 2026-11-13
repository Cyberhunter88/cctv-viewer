package viewer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLiveInfoReportsWebRTCAvailabilityAndRejectsUnusableMode(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	liveTestCamera(t, a, "camera")
	var info struct {
		WebRTC *bool `json:"webrtc"`
	}
	response := request(a, "GET", "/api/live", nil, cookie, "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &info) != nil || info.WebRTC == nil || *info.WebRTC != webrtcAvailable() {
		t.Fatal("live info does not report WebRTC availability", response.Code, response.Body.String())
	}
	if webrtcAvailable() {
		t.Skip("WebRTC media listener configured")
	}
	r := httptest.NewRequest("GET", "/api/live/camera/ws?mode=webrtc", nil)
	r.Header.Set("Origin", a.cfg.BaseURL)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatal("WebRTC mode was accepted without a media path", w.Code)
	}
}

func TestLiveConfigurationCacheIsShortAndInvalidated(t *testing.T) {
	a := testApp(t)
	liveTestCamera(t, a, "camera")
	if err := a.db.saveLiveCamera("camera", liveCamera{Enabled: true, Sources: liveSources{Fast: "rtsp://192.0.2.20/one"}}); err != nil {
		t.Fatal(err)
	}
	_, first, err := a.live.cameraConfig("camera")
	if err != nil || first.Sources.Fast != "rtsp://192.0.2.20/one" {
		t.Fatal("configuration was not read", err)
	}
	if err = a.db.saveLiveCamera("camera", liveCamera{Enabled: true, Sources: liveSources{Fast: "rtsp://192.0.2.20/two"}}); err != nil {
		t.Fatal(err)
	}
	if _, cached, _ := a.live.cameraConfig("camera"); cached.Sources.Fast != "rtsp://192.0.2.20/one" {
		t.Fatal("configuration was not reused")
	}
	a.live.invalidate("camera")
	if _, fresh, _ := a.live.cameraConfig("camera"); fresh.Sources.Fast != "rtsp://192.0.2.20/two" {
		t.Fatal("invalidation kept stale configuration")
	}
	a.live.mu.Lock()
	entry := a.live.configs["camera"]
	entry.at = time.Now().Add(-2 * liveConfigTTL)
	a.live.configs["camera"] = entry
	a.live.mu.Unlock()
	if err = a.db.saveLiveCamera("camera", liveCamera{Enabled: true, Sources: liveSources{Fast: "rtsp://192.0.2.20/three"}}); err != nil {
		t.Fatal(err)
	}
	if _, expired, _ := a.live.cameraConfig("camera"); expired.Sources.Fast != "rtsp://192.0.2.20/three" {
		t.Fatal("expired configuration was reused")
	}
}

func TestGatewayShareSettingsAreCachedBriefly(t *testing.T) {
	a := testApp(t)
	g := &liveGateway{live: a.live}
	share, err := g.shareSettings()
	if err != nil || share.Enabled {
		t.Fatal("unexpected default share", share, err)
	}
	payload, _ := json.Marshal(map[string]any{"enabled": true, "username": "homeassistant", "secret": "synthetic-share-secret", "serverUrl": ""})
	sealed, err := a.db.sealSecret(string(payload), secretScope("live_share", "1", "payload"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec(`INSERT INTO live_share(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, sealed); err != nil {
		t.Fatal(err)
	}
	if share, _ = g.shareSettings(); share.Enabled {
		t.Fatal("share settings were read again inside the cache interval")
	}
	g.shareMu.Lock()
	g.shareAt = time.Now().Add(-2 * gatewayShareTTL)
	g.shareMu.Unlock()
	if share, _ = g.shareSettings(); !share.Enabled || share.Password != "synthetic-share-secret" {
		t.Fatal("expired share settings were not refreshed", share.Enabled)
	}
}

func TestGatewayRoutesLingerAccordingToPreloadMode(t *testing.T) {
	for mode, want := range map[string]time.Duration{"off": gatewayLingerOff, "idle": gatewayLingerIdle, "always": gatewayLingerIdle} {
		if got := gatewayLinger(mode); got != want {
			t.Fatal(mode, got)
		}
	}
	if (&gatewayRoute{}).idleLimit() != gatewayLingerOff || (&gatewayRoute{linger: gatewayLingerIdle}).idleLimit() != gatewayLingerIdle {
		t.Fatal("route idle limit does not follow its linger setting")
	}
}

func TestLiveGatewayDropCounterIsExported(t *testing.T) {
	a := testApp(t)
	a.metrics.liveDrops.Add(3)
	for _, m := range a.metricValues() {
		if m.name == "cctv_live_gateway_write_errors_total" {
			if m.value != 3 || !m.counter || !strings.HasPrefix(m.name, "cctv_") {
				t.Fatal("unexpected drop metric", m)
			}
			return
		}
	}
	t.Fatal("drop counter is not exported")
}
