package viewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type liveFlushWriter struct{ http.ResponseWriter }

func (w liveFlushWriter) Write(data []byte) (int, error) {
	n, e := w.ResponseWriter.Write(data)
	_ = http.NewResponseController(w.ResponseWriter).Flush()
	return n, e
}

func TestLiveHTTPSFLVPreloadIntegration(t *testing.T) {
	binary := os.Getenv("CTV_TEST_GO2RTC")
	if binary == "" {
		t.Skip("CTV_TEST_GO2RTC required")
	}
	file := filepath.Join(t.TempDir(), "synthetic.flv")
	if output, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-t", "3", "-c:v", "libx264", "-threads", "1", "-g", "10", "-bf", "0", "-f", "flv", "-y", file).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	var connections atomic.Int32
	camera := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("user") != "synthetic" || r.URL.Query().Get("password") != "a&b#c" {
			w.WriteHeader(401)
			return
		}
		connections.Add(1)
		defer connections.Add(-1)
		w.Header().Set("Content-Type", "video/x-flv")
		command := exec.CommandContext(r.Context(), "ffmpeg", "-nostdin", "-v", "error", "-re", "-stream_loop", "-1", "-i", file, "-c", "copy", "-f", "flv", "pipe:1")
		command.Stdout = liveFlushWriter{w}
		_ = command.Run()
	}))
	t.Cleanup(camera.Close)
	a := testApp(t)
	a.live.close()
	a.cfg.Go2RTCPath = binary
	a.live = newLiveService(a)
	wait := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal(label)
	}
	wait("live service", func() bool { ready, _ := a.live.status(); return ready })
	liveTestCamera(t, a, "https-camera")
	config := liveCamera{Enabled: true, Username: "synthetic", Password: "a&b#c", Sources: liveSources{Fast: camera.URL + "/flv?stream=channel0_sub.bcs"}, Preload: map[string]string{"fast": "always"}, SkipTLSVerify: map[string]bool{"fast": true}}
	if err := a.db.saveLiveCamera("https-camera", config); err != nil {
		t.Fatal(err)
	}
	a.live.wakeWarm()
	name := liveStreamName("https-camera", "wide", "fast")
	wait("always mode did not connect without viewers", func() bool {
		a.live.mu.Lock()
		defer a.live.mu.Unlock()
		e := a.live.warm[name]
		return e != nil && e.state == "ready" && e.users == 0 && connections.Load() == 1
	})
	cookie, _ := login(t, a)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live/https-camera/ws?mode=mse", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {a.cfg.BaseURL}, "Cookie": {cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if err = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"mse","value":"avc1.640029"}`)); err != nil {
		t.Fatal(err)
	}
	for {
		kind, _, err := ws.Read(ctx)
		if err != nil {
			ws.CloseNow()
			t.Fatal(err)
		}
		if kind == websocket.MessageBinary {
			break
		}
	}
	ws.CloseNow()
	wait("viewer connection not released", func() bool { a.live.mu.Lock(); defer a.live.mu.Unlock(); return a.live.warm[name].users == 0 })
	if connections.Load() != 1 {
		t.Fatal("viewer did not share the warmed source", connections.Load())
	}
	// Apply idle mode, use the source once, then move its last-use timestamp
	// through the expiration boundary without a 60-second wall-clock sleep.
	config.Preload["fast"] = "idle"
	if err = a.db.saveLiveCamera("https-camera", config); err != nil {
		t.Fatal(err)
	}
	a.live.invalidate("https-camera")
	release := a.live.useSource("https-camera", "wide", "fast")
	release()
	wait("idle mode did not retain recently used source", func() bool {
		a.live.mu.Lock()
		defer a.live.mu.Unlock()
		e := a.live.warm[name]
		return e != nil && e.state == "ready"
	})
	a.live.mu.Lock()
	a.live.warm[name].last = time.Now().Add(-61 * time.Second)
	a.live.mu.Unlock()
	a.live.wakeWarm()
	wait("idle source did not expire", func() bool { a.live.mu.Lock(); defer a.live.mu.Unlock(); return a.live.warm[name] == nil })
	wait("expired source retained camera connection", func() bool { return connections.Load() == 0 })
	config.Preload["fast"] = "off"
	if err = a.db.saveLiveCamera("https-camera", config); err != nil {
		t.Fatal(err)
	}
	a.live.invalidate("https-camera")
	release = a.live.useSource("https-camera", "wide", "fast")
	release()
	a.live.wakeWarm()
	wait("off source created keeper", func() bool { a.live.mu.Lock(); defer a.live.mu.Unlock(); return a.live.warm[name] == nil })
}
