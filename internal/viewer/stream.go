package viewer

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) streamOwner(r *http.Request) string {
	c, e := r.Cookie("ctv_session")
	if e != nil {
		return ""
	}
	return digest(c.Value)
}
func (a *App) createStream(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Recording string  `json:"recording"`
		Offset    float64 `json:"offset"`
		Profile   string  `json:"profile"`
		Speed     float64 `json:"speed"`
		Transport string  `json:"transport"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Profile == "" {
		input.Profile = "balanced"
	}
	if input.Speed == 0 {
		input.Speed = 1
	}
	if input.Transport == "" {
		input.Transport = "hls"
	}
	if (input.Profile != "balanced" && input.Profile != "fast" && input.Profile != "full") || (input.Transport != "hls" && input.Transport != "mp4") || !contains([]string{"0.25", "0.5", "1", "2", "4", "8", "16"}, strconv.FormatFloat(input.Speed, 'f', -1, 64)) {
		fail(w, 400, "Streamingprofil, Geschwindigkeit oder Transport ungültig")
		return
	}
	record, f, e := a.source(input.Recording)
	if e != nil {
		sourceFailure(w, e, "Aufnahme nicht verfügbar")
		return
	}
	defer f.Close()
	if record.Kind == "image" {
		fail(w, 400, "Einzelbilder sind keine Videos")
		return
	}
	if math.IsNaN(input.Offset) || math.IsInf(input.Offset, 0) || input.Offset < 0 || input.Offset >= record.Duration {
		fail(w, 400, "Startposition ungültig")
		return
	}
	// Only stream files count against this budget; previews are pruned in the
	// background, so starting many tiles at once does not walk the cache each time.
	a.pruneStreams()
	if a.streamUsage() >= a.config().HLSTempBytes*9/10 {
		fail(w, 503, "Temporärer Videospeicher ausgelastet")
		return
	}
	duration := math.Min(1800, record.Duration-input.Offset)
	profile := a.config().Balanced
	switch input.Profile {
	case "fast":
		profile = a.config().Fast
	case "full":
		profile = fullProfile
	}
	factor := math.Max(1, input.Speed)
	// attempt runs one encoder and answers the request. It reports true without
	// answering when a hardware encoder failed before the stream became playable.
	attempt := func(encoder string) bool {
		if !a.acquireEncoder() {
			fail(w, 429, "Alle Umwandlungsplätze sind belegt. Weniger Kameras oder Originalwiedergabe wählen.")
			return false
		}
		id := token()[:24]
		dir := filepath.Join(a.config().DataDir, "cache", "hls-"+id)
		if e := os.Mkdir(dir, 0700); e != nil {
			a.releaseEncoder()
			fail(w, 500, "Streaming konnte nicht gestartet werden")
			return false
		}
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
		s := &stream{id: id, owner: a.streamOwner(r), dir: dir, cancel: cancel, done: make(chan struct{}), last: time.Now(), created: time.Now()}
		args, output := streamArgs(encoder, a.config().VAAPIDevice, profile, input.Offset, duration, input.Speed, input.Transport, dir)
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)

		cmd.ExtraFiles = []*os.File{f}
		if e := cmd.Start(); e != nil {
			cancel()
			os.RemoveAll(dir)
			a.releaseEncoder()
			a.metrics.streamFailures.Add(1)
			slog.Error("ffmpeg konnte nicht gestartet werden", "error", e)
			fail(w, 500, "Video-Umwandlung konnte nicht gestartet werden")
			return false
		}
		a.mediaMu.Lock()
		a.streams[id] = s
		a.mediaMu.Unlock()
		a.metrics.streamsStarted.Add(1)
		a.work.Add(1)
		go func() {
			defer a.work.Done()
			e := cmd.Wait()
			if e != nil && ctx.Err() == nil {
				a.metrics.streamFailures.Add(1)
				slog.Warn("ffmpeg beendet mit Fehler", "stream", id, "encoder", encoder, "error", e)
			}
			cancel()
			changed := a.sourceChangedSince(record)
			if changed {
				slog.Info("Aufnahme wurde während der Umwandlung verändert", "stream", id)
			}
			a.mediaMu.Lock()
			s.err = e
			s.sourceChanged = changed
			a.mediaMu.Unlock()
			a.releaseEncoder()
			close(s.done)
		}()
		// failed removes the stream after an encoder error and decides whether
		// the software encoder should take over.
		failed := func() bool {
			a.removeStream(id)
			if encoder != "software" {
				return true
			}
			fail(w, 422, "Aufnahme konnte nicht umgewandelt werden")
			return false
		}
		// Wait for the first complete HLS segment, so clients never receive an empty playlist.
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(20 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case <-r.Context().Done():
				a.removeStream(id)
				return false
			case <-timeout.C:
				a.removeStream(id)
				fail(w, 504, "Video-Umwandlung dauert zu lange")
				return false
			case <-ticker.C:
				ready := false
				if input.Transport == "hls" {
					if b, e := os.ReadFile(filepath.Join(dir, output)); e == nil {
						ready = strings.Count(string(b), "#EXTINF:") >= neededSegments(input.Speed)
					}
				} else {
					if st, e := os.Stat(filepath.Join(dir, output)); e == nil {
						ready = st.Size() > 4096
					}
				}
				select {
				case <-s.done:
					a.mediaMu.Lock()
					encodeErr := s.err
					a.mediaMu.Unlock()
					if encodeErr != nil {
						return failed()
					}
					if input.Transport == "hls" {
						if b, e := os.ReadFile(filepath.Join(dir, output)); e == nil {
							ready = strings.Contains(string(b), "#EXTINF:")
						}
					} else if st, e := os.Stat(filepath.Join(dir, output)); e == nil && st.Size() > 0 {
						ready = true
					}
				default:
				}
				if ready {
					jsonOut(w, 201, map[string]any{"id": id, "url": "/api/streams/" + id + "/" + output, "offset": input.Offset, "duration": duration, "factor": factor})
					return false
				}
				select {
				case <-s.done:
					a.mediaMu.Lock()
					err := s.err
					a.mediaMu.Unlock()
					if err != nil {
						return failed()
					}
				default:
				}
			}
		}
	}
	encoder := a.playbackEncoder()
	if encoder != "software" && !a.hardwareDecodeRefused(record.Codec) {
		// Decoding and scaling on the GPU keeps a full-resolution H.265 conversion almost
		// free for the CPU. A codec the GPU cannot decode falls back to software decoding.
		if !attempt(encoder) {
			return
		}
		a.hardwareDecodeBlocked.Store(record.Codec, time.Now().Add(hardwareRetry).Unix())
		slog.Info("Hardware-Dekodierung nicht möglich; Software-Dekodierung übernimmt", "encoder", encoder, "codec", record.Codec)
	}
	if encoder != "software" {
		encoder += "-upload"
	}
	if attempt(encoder) {
		// Missing devices or drivers fail immediately. Skip the hardware encoder
		// for a while instead of paying for a failed start with every stream.
		a.metrics.encoderFallbacks.Add(1)
		a.hardwareBlocked.Store(time.Now().Add(hardwareRetry).Unix())
		slog.Warn("Hardware-Encoding fehlgeschlagen; Software-Encoding übernimmt", "encoder", encoder)
		attempt("software")
	}
}

var encoders = []string{"software", "vaapi", "qsv"}

const hardwareRetry = 10 * time.Minute

// hardwareDecodeRefused reports whether the GPU recently refused to decode the codec.
func (a *App) hardwareDecodeRefused(codec string) bool {
	until, ok := a.hardwareDecodeBlocked.Load(codec)
	return ok && time.Now().Unix() < until.(int64)
}

// playbackEncoder returns the configured encoder unless it failed recently.
func (a *App) playbackEncoder() string {
	encoder := a.config().Encoder
	if encoder == "" || time.Now().Unix() < a.hardwareBlocked.Load() {
		return "software"
	}
	return encoder
}

// fullProfile converts originals the browser cannot decode (often H.265) without
// reducing the resolution; it is not configurable because it mirrors the original.
var fullProfile = StreamProfile{Scale: 100, FPS: 0, Bitrate: 6000}

// streamArgs builds the ffmpeg command line of one compatible playback stream
// and returns it with the name of the output file inside dir. "vaapi" and "qsv"
// decode, scale and encode on the GPU. Their "-upload" variants decode in software
// and upload the scaled frames, which works for every codec the software path accepts.
func streamArgs(encoder, device string, profile StreamProfile, offset, duration, speed float64, transport, dir string) ([]string, string) {
	encoder, upload := strings.CutSuffix(encoder, "-upload")
	gpuDecode := encoder != "software" && !upload
	factor := math.Max(1, speed)
	// Full resolution in software needs more than one core to keep up with real time.
	threads := "1"
	if profile.Scale >= 100 {
		threads = "0"
	}
	args := []string{"-nostdin", "-v", "error", "-threads", threads}
	switch {
	case encoder == "vaapi" && gpuDecode:
		args = append(args, "-hwaccel", "vaapi", "-hwaccel_device", device, "-hwaccel_output_format", "vaapi")
	case encoder == "vaapi":
		args = append(args, "-vaapi_device", device)
	case encoder == "qsv" && gpuDecode:
		args = append(args, "-init_hw_device", "qsv=hw", "-filter_hw_device", "hw", "-hwaccel", "qsv", "-hwaccel_output_format", "qsv")
	case encoder == "qsv":
		args = append(args, "-init_hw_device", "qsv=hw", "-filter_hw_device", "hw")
	}
	args = append(args, "-ss", fmt.Sprintf("%.3f", offset), "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats)
	if speed >= 8 && offset == 0 {
		args = append(args, "-skip_frame", "nokey")
	}
	size := fmt.Sprintf("trunc(iw*%d/100/2)*2:trunc(ih*%d/100/2)*2", profile.Scale, profile.Scale)
	vf := fmt.Sprintf("setpts=(PTS-STARTPTS)/%.3f,", factor)
	switch {
	case encoder == "vaapi" && gpuDecode:
		vf += fmt.Sprintf("scale_vaapi=w=trunc(iw*%d/100/2)*2:h=trunc(ih*%d/100/2)*2:format=nv12", profile.Scale, profile.Scale)
	case encoder == "qsv" && gpuDecode:
		vf += fmt.Sprintf("scale_qsv=w=trunc(iw*%d/100/2)*2:h=trunc(ih*%d/100/2)*2", profile.Scale, profile.Scale)
	default:
		vf += "scale=" + size
	}
	// FPS 0 keeps the source frame rate (full profile).
	if profile.FPS > 0 {
		vf += fmt.Sprintf(",fps=%d", profile.FPS)
	}
	switch {
	case encoder == "vaapi" && !gpuDecode:
		vf += ",format=nv12,hwupload"
	case encoder == "qsv" && !gpuDecode:
		vf += ",format=nv12,hwupload=extra_hw_frames=64,format=qsv"
	}
	args = append(args, "-i", "/proc/self/fd/3", "-t", fmt.Sprintf("%.3f", duration/factor), "-map", "0:v:0", "-map", "0:a:0?", "-af", audioTempo(factor), "-c:a", "aac", "-b:a", "96k", "-vf", vf)
	rate := []string{"-maxrate", fmt.Sprintf("%dk", profile.Bitrate), "-bufsize", fmt.Sprintf("%dk", profile.Bitrate*2)}
	// B-frame reordering shifts the presentation origin in fragmented MP4 away from AAC.
	switch encoder {
	case "vaapi":
		args = append(args, "-c:v", "h264_vaapi", "-bf", "0", "-b:v", fmt.Sprintf("%dk", profile.Bitrate))
		args = append(args, rate...)
	case "qsv":
		args = append(args, "-c:v", "h264_qsv", "-bf", "0", "-preset", "veryfast", "-b:v", fmt.Sprintf("%dk", profile.Bitrate))
		args = append(args, rate...)
	default:
		crf := "25"
		if profile.Scale >= 100 {
			crf = "20"
		}
		args = append(args, "-c:v", "libx264", "-bf", "0", "-preset", "veryfast", "-threads", threads, "-crf", crf)
		args = append(args, rate...)
		args = append(args, "-pix_fmt", "yuv420p")
	}
	args = append(args, "-force_key_frames", "expr:gte(t,n_forced*1)")
	output := "index.m3u8"
	if transport == "hls" {
		args = append(args, "-f", "hls", "-hls_time", "1", "-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_segment_filename", filepath.Join(dir, "segment-%06d.ts"))
	} else {
		output = "video.mp4"
		args = append(args, "-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof")
	}
	return append(args, filepath.Join(dir, output)), output
}
