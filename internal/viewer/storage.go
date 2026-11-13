package viewer

import (
	"net/http"
	"time"
)

const storageDays = 30

type storageDay struct {
	Date   string `json:"date"`
	Videos int    `json:"videos"`
	Bytes  int64  `json:"bytes"`
}

type storageCamera struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Videos int          `json:"videos"`
	Bytes  int64        `json:"bytes"`
	Oldest float64      `json:"oldest"`
	Newest float64      `json:"newest"`
	Days   []storageDay `json:"days"`
}

// storageOverview reports the indexed recording volume per camera. Sizes come
// from the index, so the recordings themselves are not read. Days follow the
// Berlin calendar, like the day view, and count a recording on its start day.
func (a *App) storageOverview(w http.ResponseWriter, r *http.Request) {
	cameras, err := a.db.Cameras()
	if err != nil {
		fail(w, 500, "Speicherübersicht nicht lesbar")
		return
	}
	out := make([]storageCamera, len(cameras))
	position := make(map[string]int, len(cameras))
	for i, camera := range cameras {
		out[i] = storageCamera{ID: camera.ID, Name: camera.Name, Days: make([]storageDay, storageDays)}
		position[camera.ID] = i
	}
	rows, err := a.db.reader().QueryContext(r.Context(), `SELECT camera_id,COUNT(*),COALESCE(SUM(size),0),MIN(start),MAX(start+duration) FROM recordings WHERE codec!='jpeg' GROUP BY camera_id`)
	if err != nil {
		fail(w, 500, "Speicherübersicht nicht lesbar")
		return
	}
	var totalVideos int
	var totalBytes int64
	for rows.Next() {
		var id string
		var item storageCamera
		if err = rows.Scan(&id, &item.Videos, &item.Bytes, &item.Oldest, &item.Newest); err != nil {
			break
		}
		if i, ok := position[id]; ok {
			out[i].Videos, out[i].Bytes, out[i].Oldest, out[i].Newest = item.Videos, item.Bytes, item.Oldest, item.Newest
			totalVideos += item.Videos
			totalBytes += item.Bytes
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(w, 500, "Speicherübersicht nicht lesbar")
		return
	}
	today := time.Now().In(mustBerlin())
	for day := 0; day < storageDays; day++ {
		date := today.AddDate(0, 0, day-storageDays+1).Format("2006-01-02")
		from, to, err := dayBounds(date)
		if err != nil {
			fail(w, 500, "Speicherübersicht nicht lesbar")
			return
		}
		for i := range out {
			out[i].Days[day].Date = date
		}
		rows, err := a.db.reader().QueryContext(r.Context(), `SELECT camera_id,COUNT(*),COALESCE(SUM(size),0) FROM recordings WHERE start>=? AND start<? AND codec!='jpeg' GROUP BY camera_id`, float64(from.Unix()), float64(to.Unix()))
		if err != nil {
			fail(w, 500, "Speicherübersicht nicht lesbar")
			return
		}
		for rows.Next() {
			var id string
			var videos int
			var bytes int64
			if err = rows.Scan(&id, &videos, &bytes); err != nil {
				break
			}
			if i, ok := position[id]; ok {
				out[i].Days[day].Videos, out[i].Days[day].Bytes = videos, bytes
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			fail(w, 500, "Speicherübersicht nicht lesbar")
			return
		}
	}
	jsonOut(w, 200, map[string]any{"cameras": out, "videos": totalVideos, "bytes": totalBytes, "days": storageDays})
}
