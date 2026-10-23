package viewer

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// Each relay owns its TLS policy and credentials. Only its opaque loopback URL
// is given to media tools, so diagnostics cannot expose camera query secrets.
func liveHTTPSRelay(parent context.Context, source string, insecure bool) (string, func(), error) {
	ctx, cancel := context.WithCancel(parent)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		return "", nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} // explicitly selected per source
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.TLSHandshakeTimeout = 8 * time.Second
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	path := "/" + token()
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		request, e := http.NewRequestWithContext(r.Context(), "GET", source, nil)
		if e != nil {
			http.Error(w, "Quelle nicht verfügbar", 502)
			return
		}
		response, e := client.Do(request)
		if e != nil {
			code := 502
			var verification *tls.CertificateVerificationError
			if errors.As(e, &verification) {
				code = 526
			}
			http.Error(w, "HTTPS-Verbindung fehlgeschlagen", code)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			code := response.StatusCode
			if code < 400 || code > 599 {
				code = 502
			}
			http.Error(w, "Kamera lehnt die Anfrage ab", code)
			return
		}
		// Bound stalled streams, including the initial FLV header, without buffering
		// the entire body or allocating a growing queue.
		timer := time.AfterFunc(12*time.Second, func() { response.Body.Close() })
		defer timer.Stop()
		reader := bufio.NewReaderSize(response.Body, 32*1024)
		header, e := reader.Peek(3)
		if e != nil || string(header) != "FLV" {
			http.Error(w, "Kein FLV-Stream", 422)
			return
		}
		w.Header().Set("Content-Type", "video/x-flv")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		controller := http.NewResponseController(w)
		_ = controller.Flush()
		buffer := make([]byte, 32*1024)
		for {
			n, e := reader.Read(buffer)
			if n > 0 {
				timer.Reset(12 * time.Second)
				_ = controller.SetWriteDeadline(time.Now().Add(12 * time.Second))
				if _, err := w.Write(buffer[:n]); err != nil {
					return
				}
				if controller.Flush() != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	})
	go func() { _ = server.Serve(listener) }()
	go func() { <-ctx.Done(); _ = server.Close() }()
	closeRelay := func() { cancel(); _ = server.Close(); transport.CloseIdleConnections() }
	// server.Close is idempotent; the cancellation also stops active upstream reads.
	return "http://" + listener.Addr().String() + path, closeRelay, nil
}

func liveSourceKey(key string) bool {
	return key == "fast" || key == "high" || key == "zoomFast" || key == "zoomHigh"
}
func liveKey(view, quality string) string {
	if view == "zoom" {
		if quality == "high" {
			return "zoomHigh"
		}
		return "zoomFast"
	}
	return quality
}
func liveParts(key string) (string, string) {
	if strings.HasPrefix(key, "zoom") {
		if key == "zoomHigh" {
			return "zoom", "high"
		}
		return "zoom", "fast"
	}
	return "wide", key
}
func (c liveCamera) preloadMode(key string) string {
	if mode := c.Preload[key]; mode != "" {
		return mode
	}
	return "idle"
}
