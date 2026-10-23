package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/liberrors"
)

type probeAuthFixture struct {
	user, password string
	accepted       atomic.Bool
}

func (fixture *probeAuthFixture) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !ctx.Conn.VerifyCredentials(ctx.Request, fixture.user, fixture.password) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	fixture.accepted.Store(true)
	return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
}
func TestLiveProbeEncodedCredentials(t *testing.T) {
	for _, password := range []string{"synthetic-plain", "synthetic@special#pass!"} {
		t.Run(password, func(t *testing.T) {
			address, err := freeLocalAddress()
			if err != nil {
				t.Fatal(err)
			}
			fixture := &probeAuthFixture{user: "synthetic-user", password: password}
			server := &gortsplib.Server{RTSPAddress: address, Handler: fixture}
			if err = server.Start(); err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			target := &url.URL{Scheme: "rtsp", Host: address, Path: "/synthetic", User: url.UserPassword(fixture.user, password)}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rtsp_transport", "tcp", "-protocol_whitelist", "rtsp,rtsps,tcp,tls,rtp,udp,crypto", "-show_entries", "stream=codec_type,codec_name,width,height", "-of", "json", target.String())
			_, _ = command.Output()
			if !fixture.accepted.Load() {
				t.Fatal("RTSP credentials were not accepted")
			}
		})
	}
}

func TestLiveProbeFailureMessagesNeverExposeCredentials(t *testing.T) {
	for _, test := range []struct{ diagnostic, code string }{
		{"method DESCRIBE failed: 401 Unauthorized", "authentication"},
		{"Server returned 403 Forbidden", "forbidden"},
		{"Server returned 404 Not Found", "not-found"},
		{"Server returned 461 Unsupported Transport", "transport"},
		{"Server returned 453 Not Enough Bandwidth", "camera-busy"},
		{"Server returned 503 Service Unavailable", "camera-busy"},
		{"Connection refused", "connection-refused"},
		{"No route to host", "network"},
		{"Failed to resolve hostname", "dns"},
		{"Connection timed out", "timeout"},
		{"certificate verify failed", "tls"},
		{"Unrecognized option protocol_whitelist", "probe-configuration"},
		{"unspecified synthetic failure", "stream-error"},
	} {
		t.Run(test.code, func(t *testing.T) {
			secret := "synthetic-private-credential"
			err := &exec.ExitError{Stderr: []byte(test.diagnostic + "\nrtsp://synthetic:" + secret + "@camera.example.org/stream")}
			code, message := liveProbeFailure(nil, err)
			if code != test.code || message == "" || strings.Contains(message, secret) || strings.Contains(message, "rtsp://") {
				t.Fatal(code, message)
			}
		})
	}
	for _, test := range []struct {
		ctxErr, err error
		code        string
	}{
		{context.DeadlineExceeded, errors.New("signal killed"), "timeout"},
		{context.Canceled, errors.New("signal killed"), "cancelled"},
		{nil, exec.ErrNotFound, "probe-unavailable"},
		{nil, errors.New("synthetic process error"), "probe-failed"},
	} {
		code, _ := liveProbeFailure(test.ctxErr, test.err)
		if code != test.code {
			t.Fatal(code, test.code)
		}
	}
}

func TestLiveProbeAPISeparatesAuthenticationAndPathFailures(t *testing.T) {
	address, err := freeLocalAddress()
	if err != nil {
		t.Fatal(err)
	}
	fixture := &probeAuthFixture{user: "synthetic-user", password: "synthetic@special#pass!"}
	server := &gortsplib.Server{RTSPAddress: address, Handler: fixture}
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	app := testApp(t)
	cookie, session := login(t, app)
	liveTestCamera(t, app, "probe-camera")
	source := "rtsp://" + address + "/synthetic"
	old := liveCamera{Enabled: true, Username: fixture.user, Password: fixture.password, Sources: liveSources{Fast: source}}
	if err = app.db.saveLiveCamera("probe-camera", old); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		password *string
		code     string
	}{
		{nil, "not-found"},
		{func() *string { password := "synthetic-wrong-password"; return &password }(), "authentication"},
	} {
		input := map[string]any{"enabled": true, "username": fixture.user, "sources": liveSources{Fast: source}}
		if test.password != nil {
			input["password"] = *test.password
		}
		response := request(app, http.MethodPost, "/api/cameras/probe-camera/live/test", input, cookie, session.CSRF)
		var result map[string]struct {
			OK      bool   `json:"ok"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal(response.Code, response.Body.String())
		}
		if result["wide/fast"].Code != test.code || result["wide/fast"].OK {
			t.Fatal(result)
		}
		for _, secret := range []string{fixture.password, "synthetic-wrong-password", fixture.user, source} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("probe response leaked source or credentials")
			}
		}
	}
}
