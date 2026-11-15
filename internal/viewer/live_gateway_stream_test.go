package viewer

import (
	"context"
	"encoding/json"
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

// TestLiveGatewayForwardsLargeCameraPackets reproduces a camera that sends RTP packets
// larger than one UDP datagram over RTSP/TCP. Home Assistant must still receive every
// frame without loss: the gateway asks go2rtc to repacketize without re-encoding.
func TestLiveGatewayForwardsLargeCameraPackets(t *testing.T) {
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
	encode := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=15", "-t", "30", "-c:v", "libx264", "-threads", "2", "-g", "15", "-bf", "0", "-b:v", "6M", "-maxrate", "6M", "-bufsize", "2M", "-y", source)
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
	defer cancel()
	camera := exec.CommandContext(ctx, binary, "-config", file)
	if e = camera.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); camera.Wait() }()
	time.Sleep(500 * time.Millisecond)
	publisher := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-re", "-stream_loop", "-1", "-i", source, "-c", "copy", "-f", "rtsp", "-rtsp_transport", "tcp", "-pkt_size", "8000", "rtsp://"+cameraAddress+"/camera")
	if e = publisher.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); publisher.Wait() }()

	a := testApp(t)
	a.live.close()
	a.cfg.Go2RTCPath = binary
	a.cfg.LiveRTSPAddress, _ = freeLocalAddress()
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
	// The simulated camera itself must deliver clean video, so a failure below is the gateway's.
	direct := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-rtsp_transport", "tcp", "-i", "rtsp://"+cameraAddress+"/camera", "-t", "3", "-f", "null", "-")
	if out, err := direct.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("simulated camera is not clean: %v\n%s", err, out)
	}
	u := &url.URL{Scheme: "rtsp", Host: a.cfg.LiveRTSPAddress, Path: "/live/camera/wide/fast", User: url.UserPassword("homeassistant", share.Password)}
	// Decode five seconds like Home Assistant's stream worker. A reader joins mid-GOP, so the
	// frames before the first keyframe may not decode; everything after it must arrive. Before
	// the fix the share delivered no frame at all.
	readCtx, readCancel := context.WithTimeout(ctx, 20*time.Second)
	defer readCancel()
	reader := exec.CommandContext(readCtx, "ffmpeg", "-nostdin", "-v", "error", "-rtsp_transport", "tcp", "-i", u.String(), "-t", "5", "-f", "null", "-progress", "pipe:1", "-")
	out, err := reader.Output()
	frames := 0
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(line, "frame="); ok {
			frames, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	// 5 s at 15 fps are 75 frames; at most one GOP (15 frames) may precede the first keyframe.
	if err != nil || frames < 58 {
		t.Fatalf("gateway delivered %d of 75 frames (%d packets dropped): %v", frames, a.metrics.liveDrops.Load(), err)
	}
	if drops := a.metrics.liveDrops.Load(); drops != 0 {
		t.Fatalf("gateway dropped %d packets", drops)
	}
}
