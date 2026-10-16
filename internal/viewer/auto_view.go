package viewer

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
)

// ViewDetection is returned with camera setup and preview results. It is not
// stored: the inferred marker order itself is the only lasting change.
type ViewDetection struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Pairs   int    `json:"pairs"`
}

type viewCandidate struct {
	name  string
	start time.Time
}

type viewPair struct{ wide, zoom string }

// sampleViewPairs limits both the directory walk and the number of videos
// decoded. Timestamp proximity is only a way to find comparable pictures;
// it never decides which perspective is wide.
func sampleViewPairs(c Camera, root *os.Root) []viewPair {
	wide, zoom := []viewCandidate{}, []viewCandidate{}
	seen := 0
	_ = fs.WalkDir(root.FS(), c.Folder, func(name string, d fs.DirEntry, err error) error {
		if err != nil || seen >= 10000 {
			return fs.SkipAll
		}
		seen++
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !supportedMedia(name) || strings.EqualFold(path.Ext(name), ".jpg") || strings.EqualFold(path.Ext(name), ".jpeg") {
			return nil
		}
		view := recordingView(c, name)
		if view != "wide" && view != "zoom" {
			return nil
		}
		if view == "wide" && len(wide) >= 24 || view == "zoom" && len(zoom) >= 24 {
			return nil
		}
		relative := strings.TrimPrefix(name, c.Folder+"/")
		stamp, e := cameraTimestamp(c, relative)
		if e != nil {
			return nil
		}
		item := viewCandidate{name: name, start: stamp}
		if view == "wide" {
			wide = append(wide, item)
		} else {
			zoom = append(zoom, item)
		}
		if len(wide) >= 24 && len(zoom) >= 24 {
			return fs.SkipAll
		}
		return nil
	})
	var pairs []viewPair
	used := map[int]bool{}
	for _, w := range wide {
		best, distance := -1, 30*time.Second
		for i, z := range zoom {
			if used[i] {
				continue
			}
			delta := w.start.Sub(z.start.Add(time.Duration(c.ZoomOffset * float64(time.Second))))
			if delta < 0 {
				delta = -delta
			}
			if delta < distance {
				best, distance = i, delta
			}
		}
		if best >= 0 {
			used[best] = true
			pairs = append(pairs, viewPair{w.name, zoom[best].name})
		}
		if len(pairs) == 3 {
			break
		}
	}
	return pairs
}

func viewFrame(ctx context.Context, root *os.Root, name string) (image.Image, error) {
	f, err := openSource(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	frameCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(frameCtx, "ffmpeg", "-nostdin", "-v", "error", "-threads", "1", "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-frames:v", "1", "-vf", "scale=160:90", "-f", "image2pipe", "-vcodec", "png", "pipe:1")
	cmd.ExtraFiles = []*os.File{f}
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(data))
}

// cropScore asks whether narrow is a crop of wide. The reverse score is
// measured separately, so equal or unrelated views cannot trigger a swap.
func cropScore(wide, narrow image.Image) float64 {
	w, h := 160, 90
	gray := func(img image.Image) []float64 {
		out := make([]float64, w*h)
		b := img.Bounds()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				px := b.Min.X + x*b.Dx()/w
				py := b.Min.Y + y*b.Dy()/h
				g := color.GrayModel.Convert(img.At(px, py)).(color.Gray)
				out[y*w+x] = float64(g.Y)
			}
		}
		return out
	}
	a, b := gray(wide), gray(narrow)
	best := -1.0
	for _, scale := range []float64{0.4, 0.5, 0.6, 0.7, 0.8} {
		cw, ch := int(float64(w)*scale), int(float64(h)*scale)
		for top := 0; top <= h-ch; top += 2 {
			for left := 0; left <= w-cw; left += 2 {
				var sa, sb, saa, sbb, sab float64
				const count = 30 * 18
				for iy := 0; iy < 18; iy++ {
					for ix := 0; ix < 30; ix++ {
						av := a[(top+iy*ch/18)*w+left+ix*cw/30]
						bv := b[(iy*h/18)*w+ix*w/30]
						sa += av
						sb += bv
						saa += av * av
						sbb += bv * bv
						sab += av * bv
					}
				}
				va, vb := saa-sa*sa/count, sbb-sb*sb/count
				if va < 10000 || vb < 10000 {
					continue
				}
				if score := (sab - sa*sb/count) / math.Sqrt(va*vb); score > best {
					best = score
				}
			}
		}
	}
	return best
}

func detectView(ctx context.Context, c *Camera, root *os.Root) ViewDetection {
	if c.ViewMode != "dual" {
		return ViewDetection{}
	}
	pairs := sampleViewPairs(*c, root)
	if len(pairs) == 0 {
		return ViewDetection{Status: "unclear", Message: "Keine zeitlich passenden Beispielvideos beider Ansichten gefunden; Kennungen unverändert."}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	votes := []float64{}
	for _, pair := range pairs {
		w, ew := viewFrame(ctx, root, pair.wide)
		z, ez := viewFrame(ctx, root, pair.zoom)
		if ew != nil || ez != nil {
			continue
		}
		forward, backward := cropScore(w, z), cropScore(z, w)
		if math.Max(forward, backward) >= 0.8 {
			votes = append(votes, forward-backward)
		}
	}
	if len(votes) == 0 {
		return ViewDetection{Status: "unclear", Message: "Beispielbilder konnten nicht verglichen werden; Kennungen unverändert."}
	}
	sort.Float64s(votes)
	median := votes[len(votes)/2]
	if math.Abs(median) < 0.16 || len(votes) > 1 && math.Signbit(votes[0]) != math.Signbit(votes[len(votes)-1]) {
		return ViewDetection{Status: "unclear", Message: "Bildvergleich nicht eindeutig; Kennungen unverändert.", Pairs: len(votes)}
	}
	if median < 0 {
		c.WideMarker, c.ZoomMarker = c.ZoomMarker, c.WideMarker
		return ViewDetection{Status: "swapped", Message: "Weitwinkel und Zoom anhand der Bildausschnitte automatisch getauscht.", Pairs: len(votes)}
	}
	return ViewDetection{Status: "confirmed", Message: "Weitwinkel und Zoom anhand der Bildausschnitte bestätigt.", Pairs: len(votes)}
}
