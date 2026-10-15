package viewer

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAudioTransports(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "audio", Name: "Audio", Folder: "Audio", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "Europe/Berlin"}
	if err := a.db.SaveCamera(c); err != nil {
		t.Fatal(err)
	}
	testVideo(t, filepath.Join(a.cfg.RecordingsDir, "Audio/2026/10/14/08-30-00.mp4"), true)
	a.scanDay("2026-10-14")
	records, err := a.db.Recordings(c.ID, 0, 2e10)
	if err != nil || len(records) != 1 {
		t.Fatalf("recordings: %v %v", records, err)
	}
	cookie, session := login(t, a)
	for _, transport := range []string{"mp4", "hls"} {
		for _, profile := range []string{"balanced", "fast"} {
			for _, speed := range []float64{0.25, 0.5, 1, 2, 4, 8, 16} {
				t.Run(fmt.Sprintf("%s/%s/%g", transport, profile, speed), func(t *testing.T) {
					w := request(a, "POST", "/api/streams", map[string]any{"recording": records[0].ID, "transport": transport, "profile": profile, "speed": speed, "offset": 0.25}, cookie, session.CSRF)
					if w.Code != 201 {
						t.Fatal(w.Code, w.Body.String())
					}
					var result struct{ ID string }
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					defer a.removeStream(result.ID)
					a.mediaMu.Lock()
					stream := a.streams[result.ID]
					a.mediaMu.Unlock()
					select {
					case <-stream.done:
					case <-time.After(10 * time.Second):
						t.Fatal("encoding timed out")
					}
					if stream.err != nil {
						t.Fatal(stream.err)
					}
					name := "video.mp4"
					if transport == "hls" {
						name = "index.m3u8"
					}
					out, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-show_packets", "-of", "json", filepath.Join(stream.dir, name)).CombinedOutput()
					if err != nil {
						t.Fatalf("probe: %v %s", err, out)
					}
					var probe struct {
						Packets []struct {
							CodecType string `json:"codec_type"`
							PTS       string `json:"pts_time"`
							Duration  string `json:"duration_time"`
						}
						Streams []struct {
							CodecType string `json:"codec_type"`
							CodecName string `json:"codec_name"`
						}
					}
					if err := json.Unmarshal(out, &probe); err != nil {
						t.Fatal(err)
					}
					audio, video := false, false
					audioStart, videoStart := math.Inf(1), math.Inf(1)
					audioEnd, videoEnd := math.Inf(-1), math.Inf(-1)
					for _, s := range probe.Streams {
						if s.CodecType == "audio" {
							audio = s.CodecName == "aac"
						}
						if s.CodecType == "video" {
							video = true
						}
					}
					for _, packet := range probe.Packets {
						pts, err := strconv.ParseFloat(packet.PTS, 64)
						if err != nil {
							t.Fatal(err)
						}
						duration, _ := strconv.ParseFloat(packet.Duration, 64)
						if packet.CodecType == "audio" {
							audioStart = math.Min(audioStart, pts)
							audioEnd = math.Max(audioEnd, pts+duration)
						}
						if packet.CodecType == "video" {
							videoStart = math.Min(videoStart, pts)
							videoEnd = math.Max(videoEnd, pts+duration)
						}
					}
					if !audio || !video || math.IsInf(audioStart, 0) || math.IsInf(videoStart, 0) {
						t.Fatalf("missing tracks: %s", out)
					}
					if math.Abs(audioStart-videoStart) > .15 || math.Abs(audioEnd-videoEnd) > .4 {
						t.Fatalf("A/V drift: audio %g..%g video %g..%g", audioStart, audioEnd, videoStart, videoEnd)
					}
					pcm, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-i", filepath.Join(stream.dir, name), "-vn", "-ac", "1", "-ar", "8000", "-f", "s16le", "pipe:1").Output()
					if err != nil {
						t.Fatal(err)
					}
					// Ignore AAC priming and the tail; the fixture is an 880 Hz tone.
					begin, end := 320, len(pcm)/2-160
					if end-begin < 100 {
						t.Fatal("audio too short to check pitch")
					}
					crossings := 0
					for i := begin + 1; i < end; i++ {
						previous := int16(binary.LittleEndian.Uint16(pcm[(i-1)*2:]))
						sample := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
						if previous <= 0 && sample > 0 {
							crossings++
						}
					}
					frequency := float64(crossings) * 8000 / float64(end-begin)
					if math.Abs(frequency-880) > 40 {
						t.Fatalf("pitch changed: %g Hz", frequency)
					}

					expected := (records[0].Duration - .25) / math.Max(1, speed)
					if math.Abs((videoEnd-videoStart)-expected) > .4 {
						t.Fatalf("duration %g, expected %g", videoEnd-videoStart, expected)
					}
				})
			}
		}
	}
}
