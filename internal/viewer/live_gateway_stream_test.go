package viewer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gatewayFixture runs a second go2rtc as a camera that a publisher feeds with 720p video
// in RTP packets of packetSize bytes, and a viewer whose Home Assistant share is enabled.
type gatewayFixture struct {
	app      *App
	ctx      context.Context
	password string
}

func newGatewayFixture(t *testing.T, packetSize int, configure func(*App)) gatewayFixture {
	t.Helper()
	binary := os.Getenv("CTV_TEST_GO2RTC")
	if binary == "" {
		t.Skip("CTV_TEST_GO2RTC required for real RTSP integration")
	}
	cameraAddress, e := freeLocalAddress()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "camera.mp4")
	// Large keyframes at 720p: a single frame spans many packets, as with a camera main stream.
	encode := testCommand(t, "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=15", "-t", "30", "-c:v", "libx264", "-threads", "2", "-g", "15", "-bf", "0", "-b:v", "6M", "-maxrate", "6M", "-bufsize", "2M", "-y", source)
	if out, e := encode.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	// A second go2rtc plays the camera: it accepts the publisher and forwards its packets unchanged.
	config := map[string]any{"modules": []string{"rtsp"}, "api": map[string]string{"listen": ""}, "rtsp": map[string]string{"listen": cameraAddress}, "streams": map[string]any{"camera": nil}, "log": map[string]string{"level": "error"}}
	b, _ := json.Marshal(config)
	file := filepath.Join(dir, "camera.json")
	if e = os.WriteFile(file, b, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	camera := exec.CommandContext(ctx, binary, "-config", file)
	if e = camera.Start(); e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() { cancel(); camera.Wait() })
	time.Sleep(500 * time.Millisecond)
	publisher := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-re", "-stream_loop", "-1", "-i", source, "-c", "copy", "-f", "rtsp", "-rtsp_transport", "tcp", "-pkt_size", strconv.Itoa(packetSize), "rtsp://"+cameraAddress+"/camera")
	if e = publisher.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cancel(); publisher.Wait() })

	a := testApp(t)
	a.live.close()
	a.cfg.Go2RTCPath = binary
	a.cfg.LiveRTSPAddress, _ = freeLocalAddress()
	if configure != nil {
		configure(a)
	}
	a.live = newLiveService(a)
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if ready, _ := a.live.status(); ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("live service not ready")
		}
	}
	cookie, session := login(t, a)
	liveTestCamera(t, a, "camera")
	if e = a.db.saveLiveCamera("camera", liveCamera{Enabled: true, Shared: true, Sources: liveSources{Fast: "rtsp://" + cameraAddress + "/camera"}}); e != nil {
		t.Fatal(e)
	}
	w := request(a, "PUT", "/api/admin/live-share", map[string]bool{"enabled": true}, cookie, session.CSRF)
	var share struct {
		Password string `json:"password"`
	}
	if json.Unmarshal(w.Body.Bytes(), &share); share.Password == "" || w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	// The simulated camera itself must deliver clean video, so a failure later is the gateway's.
	direct := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-rtsp_transport", "tcp", "-i", "rtsp://"+cameraAddress+"/camera", "-t", "3", "-f", "null", "-")
	if out, err := direct.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("simulated camera is not clean: %v\n%s", err, out)
	}
	return gatewayFixture{app: a, ctx: ctx, password: share.Password}
}

// frames decodes five seconds from the share like Home Assistant's stream worker and
// returns the number of decoded frames. A reader joins mid-GOP, so the frames before the
// first keyframe may not decode; everything after it must arrive.
func (f gatewayFixture) frames(t *testing.T, scheme string, options ...string) (int, error) {
	t.Helper()
	// Home Assistant retries a stream that could not be opened; so does this reader, once.
	frames, err := f.read(scheme, options...)
	if frames == 0 && err != nil && strings.Contains(err.Error(), "Error opening input") {
		t.Log("first open failed, retrying like Home Assistant:", err)
		frames, err = f.read(scheme, options...)
	}
	return frames, err
}

func (f gatewayFixture) read(scheme string, options ...string) (int, error) {
	u := &url.URL{Scheme: scheme, Host: f.app.cfg.LiveRTSPAddress, Path: "/live/camera/wide/fast", User: url.UserPassword("homeassistant", f.password)}
	readCtx, readCancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer readCancel()
	args := append([]string{"-nostdin", "-v", "error", "-rtsp_transport", "tcp"}, options...)
	reader := exec.CommandContext(readCtx, "ffmpeg", append(args, "-i", u.String(), "-t", "5", "-f", "null", "-progress", "pipe:1", "-")...)
	var stderr strings.Builder
	reader.Stderr = &stderr
	out, err := reader.Output()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.ReplaceAll(stderr.String(), f.password, "[redacted]"))
	}
	frames := 0
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(line, "frame="); ok {
			frames, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	return frames, err
}

// TestLiveGatewayForwardsLargeCameraPackets reproduces a camera that sends RTP packets
// larger than one UDP datagram over RTSP/TCP. Home Assistant must still receive every
// frame without loss: the gateway asks go2rtc to repacketize without re-encoding.
func TestLiveGatewayForwardsLargeCameraPackets(t *testing.T) {
	f := newGatewayFixture(t, 8000, nil)
	// 5 s at 15 fps are 75 frames; at most one GOP (15 frames) may precede the first keyframe.
	// Before the fix the share delivered no frame at all.
	if frames, err := f.frames(t, "rtsp"); err != nil || frames < 58 {
		t.Fatalf("gateway delivered %d of 75 frames (%d packets dropped): %v", frames, f.app.metrics.liveDrops.Load(), err)
	}
	if drops := f.app.metrics.liveDrops.Load(); drops != 0 {
		t.Fatalf("gateway dropped %d packets", drops)
	}
}

// TestLiveGatewaySpeaksRTSPSWithCertificate checks the encrypted share: with a certificate
// the listener accepts RTSPS only, and Home Assistant receives the same clean video.
func TestLiveGatewaySpeaksRTSPSWithCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "share.crt"), filepath.Join(dir, "share.key")
	writeTestCertificate(t, certFile, keyFile)
	f := newGatewayFixture(t, 1400, func(a *App) { a.cfg.LiveRTSPSCert, a.cfg.LiveRTSPSKey = certFile, keyFile })
	// ffmpeg verifies the certificate like Home Assistant; the test trusts its own CA.
	if frames, err := f.frames(t, "rtsps", "-tls_verify", "1", "-ca_file", certFile); err != nil || frames < 58 {
		t.Fatalf("RTSPS share delivered %d of 75 frames: %v", frames, err)
	}
	// A plain RTSP request reaches a TLS listener and must not get an RTSP answer.
	conn, err := net.DialTimeout("tcp", f.app.cfg.LiveRTSPAddress, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	conn.Write([]byte("OPTIONS rtsp://localhost/ RTSP/1.0\r\nCSeq: 1\r\n\r\n"))
	answer := make([]byte, 64)
	if n, _ := conn.Read(answer); strings.HasPrefix(string(answer[:n]), "RTSP/1.0") {
		t.Fatal("unencrypted RTSP was answered while RTSPS is configured")
	}
}

func writeTestCertificate(t *testing.T, certFile, keyFile string) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalECPrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600)
}
