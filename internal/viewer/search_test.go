package viewer

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

type searchHit struct {
	ID    string  `json:"id"`
	Start float64 `json:"start"`
}

func TestSearchClockCorrectionAndPagination(t *testing.T) {
	for _, offset := range []float64{0, -20, 20} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			cameras := []Camera{
				{ID: "single", Name: "Single", Folder: "single", ViewMode: "single", ZoomMarker: "_zoom_", ZoomOffset: 99},
				{ID: "dual", Name: "Dual", Folder: "dual", ViewMode: "dual", WideMarker: "_wide_", ZoomMarker: "_zoom_", ZoomOffset: offset},
			}
			for _, camera := range cameras {
				if err := store.SaveCamera(camera); err != nil {
					t.Fatal(err)
				}
			}
			type fixture struct {
				searchHit
				camera string
			}
			fixtures := []fixture{}
			for i := 0; i < 230; i++ {
				camera := cameras[i%2]
				marker := "_wide_"
				if i%3 == 0 {
					marker = "_zoom_"
				}
				if i%5 == 0 {
					marker = "_wide_zoom_"
				} // Wide takes precedence over zoom.
				file := filepath.Join(camera.Folder, fmt.Sprintf("video%s%03d.mp4", marker, i))
				start := 1000 + float64(i/3)
				effective := start
				if camera.ViewMode == "dual" && marker == "_zoom_" {
					effective += offset
				}
				id := fmt.Sprintf("record-%03d", i)
				_, err := store.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,30,'h264',1,1,?)`, id, camera.ID, file, start, filepath.Base(file))
				if err != nil {
					t.Fatal(err)
				}
				fixtures = append(fixtures, fixture{searchHit{id, effective}, camera.ID})
			}
			app := &App{db: store}
			for _, camera := range []string{"", "single", "dual", "missing"} {
				for _, order := range []string{"asc", "desc"} {
					for _, filtered := range []bool{false, true} {
						name := fmt.Sprintf("%s/%s/filter=%v", camera, order, filtered)
						t.Run(name, func(t *testing.T) {
							params := url.Values{"camera": {camera}, "order": {order}}
							want := []searchHit{}
							for _, f := range fixtures {
								if camera != "" && camera != f.camera {
									continue
								}
								if filtered && (f.Start+30 < 1030 || f.Start > 1050) {
									continue
								}
								want = append(want, f.searchHit)
							}
							if filtered {
								params.Set("from", "1030")
								params.Set("to", "1050")
								params.Set("q", "video")
								params.Set("minDuration", "30")
							}
							sort.Slice(want, func(i, j int) bool {
								if want[i].Start == want[j].Start {
									if order == "asc" {
										return want[i].ID < want[j].ID
									}
									return want[i].ID > want[j].ID
								}
								if order == "asc" {
									return want[i].Start < want[j].Start
								}
								return want[i].Start > want[j].Start
							})
							read := func() struct {
								Results    []searchHit
								Total      int
								HasMore    bool
								NextCursor string
							} { t.Helper(); w := httptest.NewRecorder(); app.searchRecordings(w, httptest.NewRequest("GET", "/api/search?"+params.Encode(), nil)); if w.Code != 200 {
								t.Fatal(w.Code, w.Body.String())
							}; var page struct {
								Results    []searchHit
								Total      int
								HasMore    bool
								NextCursor string
							}; if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
								t.Fatal(err)
							}; return page }
							got := []searchHit{}
							for pageNumber := 0; ; pageNumber++ {
								if pageNumber > 5 {
									t.Fatal("pagination did not finish")
								}
								page := read()
								if pageNumber == 0 && page.Total != len(want) {
									t.Fatalf("total %d want %d", page.Total, len(want))
								}
								if pageNumber > 0 && page.Total != 0 {
									t.Fatal("count=0 ignored")
								}
								got = append(got, page.Results...)
								if !page.HasMore {
									break
								}
								params.Set("cursor", page.NextCursor)
								params.Set("count", "0")
							}
							if !reflect.DeepEqual(got, want) {
								t.Fatalf("cursor results differ: got %v want %v", got, want)
							}
							params.Del("cursor")
							params.Del("count")
							params.Set("offset", strconv.Itoa(len(want)/2))
							page := read()
							end := len(want)/2 + 100
							if end > len(want) {
								end = len(want)
							}
							if !reflect.DeepEqual(page.Results, want[len(want)/2:end]) {
								t.Fatal("offset results differ")
							}
						})
					}
				}
			}
		})
	}
}

func TestSearchQueryPlans(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, expression := range []string{"r.start", correctedSearchStart} {
		rows, err := store.Query(`EXPLAIN QUERY PLAN SELECT r.id,`+expression+` FROM recordings r JOIN cameras c ON c.id=r.camera_id WHERE r.codec!='jpeg' AND r.camera_id=? ORDER BY `+expression+` DESC,r.id DESC LIMIT 101`, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		t.Log("sort expression:", expression)
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			t.Log(detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}
