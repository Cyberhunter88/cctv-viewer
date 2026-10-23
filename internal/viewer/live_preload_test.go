package viewer

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLiveWarmPolicy(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		mode  string
		users int
		last  time.Time
		want  bool
	}{
		{"always", 0, time.Time{}, true}, {"off", 2, now, false},
		{"idle", 0, time.Time{}, false}, {"idle", 1, time.Time{}, true},
		{"idle", 0, now.Add(-59 * time.Second), true}, {"idle", 0, now.Add(-60 * time.Second), false},
	} {
		if got := warmWanted(tc.mode, tc.users, tc.last, now); got != tc.want {
			t.Errorf("%s users=%d: %v", tc.mode, tc.users, got)
		}
	}
	c := liveCamera{}
	for _, key := range []string{"fast", "high", "zoomFast", "zoomHigh"} {
		if c.preloadMode(key) != "idle" {
			t.Fatal("old configuration lost default")
		}
	}
	c.Preload = map[string]string{"fast": "off", "high": "always", "zoomFast": "idle"}
	if validateLive(c) != nil {
		t.Fatal("independent modes rejected")
	}
	c.Preload["other"] = "always"
	if validateLive(c) == nil {
		t.Fatal("unknown source accepted")
	}
}
func TestLiveUsageReferenceCounting(t *testing.T) {
	s := &liveService{warm: map[string]*liveWarm{}, warmWake: make(chan struct{}, 1)}
	one := s.useSource("camera", "wide", "fast")
	two := s.useSource("camera", "wide", "fast")
	entry := s.warm[liveStreamName("camera", "wide", "fast")]
	one()
	one()
	if entry.users != 1 {
		t.Fatal("release was not idempotent")
	}
	two()
	if entry.users != 0 || entry.last.IsZero() {
		t.Fatal("last reader did not start idle period")
	}
}
func TestLiveHTTPSRelayTLSAndCredentials(t *testing.T) {
	seen := make(chan url.Values, 4)
	camera := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Query()
		w.Header().Set("Content-Type", "video/x-flv")
		_, _ = w.Write([]byte("FLVsynthetic"))
	}))
	camera.Config.ErrorLog = log.New(io.Discard, "", 0)
	camera.StartTLS()
	defer camera.Close()
	source := camera.URL + "/flv?stream=channel0_sub.bcs&user=synthetic&password=a%26b%23c"
	for _, skip := range []bool{false, true} {
		relay, closeRelay, e := liveHTTPSRelay(context.Background(), source, skip)
		if e != nil {
			t.Fatal(e)
		}
		response, e := http.Get(relay)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		closeRelay()
		if skip {
			if response.StatusCode != 200 || !strings.HasPrefix(string(body), "FLV") {
				t.Fatal(response.StatusCode, string(body))
			}
		} else if response.StatusCode != 526 {
			t.Fatal("untrusted certificate accepted", response.StatusCode)
		}
		if strings.Contains(string(body), "a&b#c") {
			t.Fatal("credential leaked")
		}
	}
	select {
	case q := <-seen:
		if q.Get("password") != "a&b#c" || q.Get("stream") != "channel0_sub.bcs" {
			t.Fatal("query encoding changed")
		}
	case <-time.After(time.Second):
		t.Fatal("no camera request")
	}
}
func TestLiveHTTPSRelayRejectsRedirectAndNonFLV(t *testing.T) {
	for _, status := range []int{200, 302, 401} {
		camera := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://other.example.org/flv")
			w.WriteHeader(status)
			_, _ = w.Write([]byte("<html>not a stream</html>"))
		}))
		relay, closeRelay, e := liveHTTPSRelay(context.Background(), camera.URL, true)
		if e != nil {
			t.Fatal(e)
		}
		response, e := http.Get(relay)
		if e != nil {
			t.Fatal(e)
		}
		want := status
		if status == 200 {
			want = 422
		}
		if status == 302 {
			want = 502
		}
		if response.StatusCode != want {
			t.Fatal(status, response.StatusCode)
		}
		response.Body.Close()
		closeRelay()
		camera.Close()
	}
}
func TestLiveHTTPSConfiguration(t *testing.T) {
	c := liveCamera{Enabled: true, Username: "synthetic", Password: "a&b#c", Sources: liveSources{Fast: "https://camera.example.org/flv?stream=channel0_sub.bcs"}}
	if err := validateLive(c); err != nil {
		t.Fatal(err)
	}
	raw, err := c.source("wide", "fast")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	if u.User != nil || u.Query().Get("password") != c.Password {
		t.Fatal("wrong authentication scheme")
	}
	c.Sources.Fast += "&password=secret"
	if validateLive(c) == nil {
		t.Fatal("secret query accepted in stored URL")
	}
	c.Sources.Fast = "http://camera.example.org/flv"
	if validateLive(c) == nil {
		t.Fatal("unrequested insecure source accepted")
	}
}
