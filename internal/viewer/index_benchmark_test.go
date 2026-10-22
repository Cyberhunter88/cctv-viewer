package viewer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func benchmarkArchive(b *testing.B) (*App, Camera) {
	return benchmarkArchiveSize(b, 10000)
}

func benchmarkArchiveSize(b *testing.B, size int) (*App, Camera) {
	b.Helper()
	dir := b.TempDir()
	store, err := OpenStore(filepath.Join(dir, "data"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	c := Camera{ID: "synthetic", Name: "Synthetic", Folder: "camera", Timezone: "Europe/Berlin", Format: "automatic", IndexingMode: "full"}
	if err := store.SaveCamera(c); err != nil {
		b.Fatal(err)
	}
	dual := c
	dual.ID = "synthetic-dual"
	dual.Folder = "dual"
	dual.ViewMode = "dual"
	dual.WideMarker = "_wide_"
	dual.ZoomMarker = "_zoom_"
	if err := store.SaveCamera(dual); err != nil {
		b.Fatal(err)
	}
	tx, err := store.Begin()
	if err != nil {
		b.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	defer statement.Close()
	for i := 0; i < size; i++ {
		camera, folder, marker := c.ID, c.Folder, "_wide_"
		if i%4 == 0 {
			camera, folder = dual.ID, dual.Folder
			marker = "_zoom_"
		}
		name := fmt.Sprintf("%s/video%s%07d.mp4", folder, marker, i)
		_, err = statement.Exec(fmt.Sprintf("%07d", i), camera, name, 1791950400+float64((i/2)*60), 30, "h264", 1000, 0, filepath.Base(name))
		if err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	rootDir := filepath.Join(dir, "recordings")
	if err := os.MkdirAll(filepath.Join(rootDir, "camera"), 0700); err != nil {
		b.Fatal(err)
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { root.Close() })
	return &App{db: store, root: root, ctx: context.Background()}, c
}

func BenchmarkDayRecordings(b *testing.B) {
	a, c := benchmarkArchive(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.db.DayRecordings([]Camera{c}, 1791950400, 1792036800); err != nil {
			b.Fatal(err)
		}
	}
}

// Setup and cursor construction are deliberately excluded from the timed runs.
func BenchmarkSearchRecordings(b *testing.B) {
	for _, size := range []int{10000, 100000, 1000000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			a, _ := benchmarkArchiveSize(b, size)
			for _, zoomOffset := range []int{0, -2} {
				if _, err := a.db.Exec(`UPDATE cameras SET zoom_offset=? WHERE id='synthetic-dual'`, zoomOffset); err != nil {
					b.Fatal(err)
				}
				b.Run(fmt.Sprintf("zoom=%d", zoomOffset), func(b *testing.B) {
					mid := 1791950400 + float64(size/4*60)
					raw, _ := json.Marshal(map[string]any{"start": mid, "id": fmt.Sprintf("%07d", size/2)})
					cursor := base64.RawURLEncoding.EncodeToString(raw)
					cases := []struct{ name, query string }{
						{"first", ""},
						{"first-asc", "order=asc"},
						{"camera", "camera=synthetic"},
						{"text", "q=000"},
						{"range", "camera=synthetic&from=" + strconv.FormatFloat(mid-3600, 'f', 0, 64) + "&to=" + strconv.FormatFloat(mid, 'f', 0, 64) + "&minDuration=20"},
						{"cursor-desc", "count=0&cursor=" + cursor},
						{"cursor-asc", "count=0&order=asc&cursor=" + cursor},
						{"camera-cursor", "camera=synthetic&count=0&cursor=" + cursor},
						{"offset", "offset=5000"},
					}
					for _, tc := range cases {
						b.Run(tc.name, func(b *testing.B) {
							b.ReportAllocs()
							for i := 0; i < b.N; i++ {
								r := httptest.NewRequest("GET", "/api/search?"+tc.query, nil)
								w := httptest.NewRecorder()
								a.searchRecordings(w, r)
								if w.Code != 200 {
									b.Fatal(w.Code, w.Body.String())
								}
							}
						})
					}
				})
			}
		})
	}
}

func BenchmarkWalkMedia(b *testing.B) {
	dir := b.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "camera"), 0700); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := os.WriteFile(filepath.Join(dir, "camera", fmt.Sprintf("%05d.mp4", i)), nil, 0600); err != nil {
			b.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer root.Close()
	a := &App{root: root, ctx: context.Background()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.walkMedia("camera", 0, func(string, os.DirEntry) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}
