package viewer

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStorageOverviewPerCameraAndDay(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	if w := request(a, "GET", "/api/admin/storage", nil, nil, ""); w.Code != 401 {
		t.Fatal("anonymous access", w.Code)
	}
	for _, id := range []string{"front", "garden", "empty"} {
		if err := a.db.SaveCamera(Camera{ID: id, Name: "Kamera " + id, Folder: id, Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "Europe/Berlin"}); err != nil {
			t.Fatal(err)
		}
	}
	today := time.Now().In(mustBerlin()).Format("2006-01-02")
	from, _, err := dayBounds(today)
	if err != nil {
		t.Fatal(err)
	}
	yesterday, _, _ := dayBounds(from.AddDate(0, 0, -1).Format("2006-01-02"))
	old, _, _ := dayBounds(from.AddDate(0, 0, -90).Format("2006-01-02"))
	rows := []struct {
		id, camera, codec string
		start             float64
		size              int64
	}{
		{"a", "front", "h264", float64(from.Unix()), 1000},             // first second of today
		{"b", "front", "hevc", float64(from.Unix()) + 60, 2000},        // today
		{"c", "front", "h264", float64(yesterday.Unix()) + 3600, 4000}, // early yesterday
		{"d", "front", "h264", float64(old.Unix()) + 10, 8000},         // outside the daily window
		{"e", "front", "jpeg", float64(from.Unix()) + 5, 16000},        // legacy image rows are ignored
		{"f", "garden", "h264", float64(yesterday.Unix()) + 30, 500},
	}
	for _, row := range rows {
		if _, err = a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,30,?,?,1,?)`, row.id, row.camera, row.camera+"/"+row.id+".mp4", row.start, row.codec, row.size, row.id+".mp4"); err != nil {
			t.Fatal(err)
		}
	}
	w := request(a, "GET", "/api/admin/storage", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Cameras []storageCamera `json:"cameras"`
		Videos  int             `json:"videos"`
		Bytes   int64           `json:"bytes"`
		Days    int             `json:"days"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Videos != 5 || out.Bytes != 15500 || out.Days != storageDays || len(out.Cameras) != 3 {
		t.Fatal(out.Videos, out.Bytes, out.Days, len(out.Cameras))
	}
	byID := map[string]storageCamera{}
	for _, camera := range out.Cameras {
		if len(camera.Days) != storageDays || camera.Days[storageDays-1].Date != today {
			t.Fatal(camera.ID, len(camera.Days), camera.Days[storageDays-1].Date)
		}
		byID[camera.ID] = camera
	}
	front := byID["front"]
	if front.Videos != 4 || front.Bytes != 15000 || front.Oldest != float64(old.Unix())+10 || front.Newest != float64(from.Unix())+90 {
		t.Fatal(front.Videos, front.Bytes, front.Oldest, front.Newest)
	}
	if day := front.Days[storageDays-1]; day.Videos != 2 || day.Bytes != 3000 {
		t.Fatal("today", day)
	}
	if day := front.Days[storageDays-2]; day.Videos != 1 || day.Bytes != 4000 {
		t.Fatal("yesterday", day)
	}
	var windowBytes int64
	for _, day := range front.Days {
		windowBytes += day.Bytes
	}
	if windowBytes != 7000 {
		t.Fatal("daily window must exclude old recordings", windowBytes)
	}
	if garden := byID["garden"]; garden.Videos != 1 || garden.Bytes != 500 || garden.Days[storageDays-2].Bytes != 500 || garden.Days[storageDays-1].Bytes != 0 {
		t.Fatal(garden)
	}
	if empty := byID["empty"]; empty.Videos != 0 || empty.Bytes != 0 || empty.Oldest != 0 || empty.Name != "Kamera empty" {
		t.Fatal(empty)
	}
	// Viewers do not see storage details.
	if _, err = a.db.Exec(`UPDATE sessions SET role='viewer'`); err != nil {
		t.Fatal(err)
	}
	if w := request(a, "GET", "/api/admin/storage", nil, cookie, ""); w.Code != 403 {
		t.Fatal("viewer access", w.Code)
	}
}
