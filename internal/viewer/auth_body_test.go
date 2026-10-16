package viewer

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type delayedLoginBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *delayedLoginBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, io.EOF
}
func (b *delayedLoginBody) Close() error { return nil }

func TestSlowLoginDoesNotReserveAuthentication(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	var bodies []*delayedLoginBody
	done := make(chan struct{}, 2)
	defer func() {
		for _, b := range bodies {
			close(b.release)
		}
		for range bodies {
			<-done
		}
	}()
	for range 2 {
		b := &delayedLoginBody{started: make(chan struct{}), release: make(chan struct{})}
		bodies = append(bodies, b)
		r := httptest.NewRequest("POST", "/auth/local", b)
		r.Header.Set("Origin", a.config().BaseURL)
		go func() { a.Handler().ServeHTTP(httptest.NewRecorder(), r); done <- struct{}{} }()
		select {
		case <-b.started:
		case <-time.After(3 * time.Second):
			t.Fatal("login never reached body read")
		}
	}
	if len(a.limits.active) != 0 {
		t.Fatal("incomplete bodies reserved password hashing slots")
	}
	changed := make(chan error, 1)
	go func() { changed <- a.activateSettings(a.config(), nil, nil) }()
	select {
	case e := <-changed:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("incomplete bodies blocked authentication settings")
	}
	if w := request(a, "GET", "/api/session", nil, cookie, ""); w.Code != 200 {
		t.Fatal("existing session failed", w.Code)
	}
	w := request(a, "POST", "/auth/local", map[string]string{"username": "test", "password": "a long secure password"}, nil, "")
	if w.Code != 200 {
		t.Fatal("complete login failed while two bodies are pending", w.Code, w.Body.String())
	}
}

// Shorten only the transport deadline in this test; production uses ten seconds.
type shortBodyDeadline struct{ http.ResponseWriter }

func (w shortBodyDeadline) SetReadDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		deadline = time.Now().Add(100 * time.Millisecond)
	}
	return http.NewResponseController(w.ResponseWriter).SetReadDeadline(deadline)
}

func TestJSONBodyReadDeadline(t *testing.T) {
	a := testApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.Handler().ServeHTTP(shortBodyDeadline{w}, r)
	}))
	defer server.Close()
	for _, transfer := range []string{"length", "chunked", "trailing"} {
		t.Run(transfer, func(t *testing.T) {
			conn, e := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			framing, body := "Content-Length: 100", "{"
			want := 400
			if transfer == "chunked" {
				framing, body = "Transfer-Encoding: chunked", "1\r\n{\r\n"
			}
			if transfer == "trailing" {
				body, want = "{}", 401
			}
			fmt.Fprintf(conn, "POST /auth/local HTTP/1.1\r\nHost: localhost\r\nOrigin: %s\r\n%s\r\nConnection: close\r\n\r\n%s", a.config().BaseURL, framing, body)
			response, e := http.ReadResponse(bufio.NewReader(conn), nil)
			if e != nil {
				t.Fatal("slow body was not rejected within deadline", e)
			}
			defer response.Body.Close()
			if response.StatusCode != want {
				t.Fatal("slow body accepted", response.StatusCode)
			}
		})
	}
	if w := request(a, "POST", "/auth/local", map[string]string{"username": "missing", "password": "invalid"}, nil, ""); w.Code != 401 {
		t.Fatal("normal request changed", w.Code)
	}
}

func TestLoginRechecksSettingsAfterBody(t *testing.T) {
	a := testApp(t)
	if e := a.db.CreateAdmin("test", "a long secure password"); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/auth/local", nil)
	r.Header.Set("Origin", a.config().BaseURL)
	r.Body = &loginSettingsBody{Reader: strings.NewReader(`{"username":"test","password":"a long secure password"}`), disable: func() {
		c := a.config()
		c.LocalAdmin = false
		if e := a.activateSettings(c, nil, nil); e != nil {
			t.Error(e)
		}
	}}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 404 || len(w.Result().Cookies()) != 0 {
		t.Fatal("disabled login created session", w.Code)
	}
}

func TestLoginBodyValidationAndHashingLimit(t *testing.T) {
	a := testApp(t)
	for _, body := range []string{"{", `{"unknown":true}`, `{"password":"` + strings.Repeat("x", 17000) + `"}`} {
		r := httptest.NewRequest("POST", "/auth/local", strings.NewReader(body))
		r.Header.Set("Origin", a.config().BaseURL)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 400 || len(a.limits.active) != 0 {
			t.Fatal("invalid body used hashing capacity", w.Code)
		}
	}
	for range cap(a.limits.active) {
		a.limits.active <- struct{}{}
	}
	defer func() {
		for len(a.limits.active) > 0 {
			<-a.limits.active
		}
	}()
	if w := request(a, "POST", "/auth/local", map[string]string{"username": "missing", "password": "invalid"}, nil, ""); w.Code != 429 {
		t.Fatal("hashing concurrency limit bypassed", w.Code)
	}
}

type loginSettingsBody struct {
	*strings.Reader
	disable func()
	once    sync.Once
}

func (b *loginSettingsBody) Read(p []byte) (int, error) {
	b.once.Do(b.disable)
	return b.Reader.Read(p)
}
func (b *loginSettingsBody) Close() error { return nil }
