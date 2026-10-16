package viewer

import (
	"context"
	"database/sql"
	"errors"
	"math"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct{ *sql.DB }
type Camera struct {
	ViewMode         string         `json:"viewMode"`
	WideMarker       string         `json:"wideMarker"`
	ZoomMarker       string         `json:"zoomMarker"`
	ZoomOffset       float64        `json:"zoomOffset"`
	SupportsZoom     bool           `json:"supportsZoom"`
	Detection        *ViewDetection `json:"detection,omitempty"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Folder           string         `json:"folder"`
	Pattern          string         `json:"pattern"`
	Timezone         string         `json:"timezone"`
	Offset           float64        `json:"offset"`
	Format           string         `json:"format"`
	Prefix           string         `json:"prefix"`
	IndexingMode     string         `json:"indexingMode"`
	DirectoryPattern string         `json:"directoryPattern"`
	SourceStatus     string         `json:"sourceStatus"`
	LastScan         int64          `json:"lastScan"`
	IndexedVideos    int            `json:"indexedVideos"`
}
type Recording struct {
	View      string  `json:"view,omitempty"`
	ID        string  `json:"id"`
	CameraID  string  `json:"cameraId"`
	Path      string  `json:"-"`
	Start     float64 `json:"start"`
	Duration  float64 `json:"duration"`
	Codec     string  `json:"codec"`
	Container string  `json:"container"`
	Kind      string  `json:"kind"`
	Size      int64   `json:"-"`
	Mod       int64   `json:"-"`
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "viewer.db"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	st, statErr := file.Stat()
	if statErr != nil {
		file.Close()
		return nil, statErr
	}
	fresh := st.Size() == 0
	file.Close()
	db, err := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// VACUUM INTO includes committed WAL pages and produces a standalone rollback copy.
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version > 4 {
		db.Close()
		return nil, errors.New("Datenbankschema neuer als diese Anwendung")
	}
	if !fresh && version < 4 {
		backup := filepath.Join(dir, "viewer-before-v4.db")
		if _, statErr := os.Stat(backup); os.IsNotExist(statErr) {
			if _, err = db.Exec("VACUUM INTO ?", backup); err != nil {
				db.Close()
				return nil, err
			}
			if err = os.Chmod(backup, 0600); err != nil {
				db.Close()
				return nil, err
			}
		} else if statErr != nil {
			db.Close()
			return nil, statErr
		}
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS cameras(id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,pattern TEXT NOT NULL,timezone TEXT NOT NULL,offset INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS recordings(id TEXT PRIMARY KEY,camera_id TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,path TEXT NOT NULL,start REAL NOT NULL,duration REAL NOT NULL,codec TEXT NOT NULL,size INTEGER NOT NULL,mod INTEGER NOT NULL,UNIQUE(camera_id,path));
 CREATE INDEX IF NOT EXISTS recordings_time ON recordings(camera_id,start);
 CREATE TABLE IF NOT EXISTS camera_scans(camera_id TEXT PRIMARY KEY REFERENCES cameras(id) ON DELETE CASCADE,attempt INTEGER NOT NULL,success INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS admins(username TEXT PRIMARY KEY,hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(hash TEXT PRIMARY KEY,subject TEXT NOT NULL,name TEXT NOT NULL,role TEXT NOT NULL,kind TEXT NOT NULL,csrf TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS user_preferences(owner TEXT PRIMARY KEY,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS oidc_flows(hash TEXT PRIMARY KEY,nonce TEXT NOT NULL,verifier TEXT NOT NULL,expires INTEGER NOT NULL);
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Additive migrations preserve existing accounts and cameras.
	for _, migration := range []struct{ table, column, ddl string }{
		{"cameras", "view_mode", "ALTER TABLE cameras ADD COLUMN view_mode TEXT NOT NULL DEFAULT 'single'"},
		{"cameras", "wide_marker", "ALTER TABLE cameras ADD COLUMN wide_marker TEXT NOT NULL DEFAULT '_00_'"},
		{"cameras", "zoom_marker", "ALTER TABLE cameras ADD COLUMN zoom_marker TEXT NOT NULL DEFAULT '_01_'"},
		{"cameras", "zoom_offset", "ALTER TABLE cameras ADD COLUMN zoom_offset REAL NOT NULL DEFAULT 0"},
		{"cameras", "supports_zoom", "ALTER TABLE cameras ADD COLUMN supports_zoom INTEGER NOT NULL DEFAULT 1"},
		{"camera_scans", "complete", "ALTER TABLE camera_scans ADD COLUMN complete INTEGER NOT NULL DEFAULT 0"},
		{"cameras", "format", "ALTER TABLE cameras ADD COLUMN format TEXT NOT NULL DEFAULT 'custom'"},
		{"cameras", "prefix", "ALTER TABLE cameras ADD COLUMN prefix TEXT NOT NULL DEFAULT ''"},
		{"cameras", "indexing_mode", "ALTER TABLE cameras ADD COLUMN indexing_mode TEXT NOT NULL DEFAULT 'partitioned'"},
		{"cameras", "directory_pattern", "ALTER TABLE cameras ADD COLUMN directory_pattern TEXT NOT NULL DEFAULT '{YYYY}/{MM}/{DD}'"},
		{"recordings", "filename", "ALTER TABLE recordings ADD COLUMN filename TEXT NOT NULL DEFAULT ''"},
		{"recordings", "settled", "ALTER TABLE recordings ADD COLUMN settled INTEGER NOT NULL DEFAULT 0"},
		{"admins", "role", "ALTER TABLE admins ADD COLUMN role TEXT NOT NULL DEFAULT 'admin'"},
		{"admins", "disabled", "ALTER TABLE admins ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0"},
	} {
		rows, e := db.Query("PRAGMA table_info(" + migration.table + ")")
		if e != nil {
			db.Close()
			return nil, e
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if e = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); e != nil {
				rows.Close()
				db.Close()
				return nil, e
			}
			found = found || name == migration.column
		}
		rows.Close()
		if !found {
			if _, e = db.Exec(migration.ddl); e != nil {
				db.Close()
				return nil, e
			}
		}
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS settings(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,payload TEXT NOT NULL); CREATE TABLE IF NOT EXISTS camera_days(camera_id TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,date TEXT NOT NULL,checked INTEGER NOT NULL,complete INTEGER NOT NULL,PRIMARY KEY(camera_id,date));

CREATE TABLE IF NOT EXISTS scan_schedule(camera_id TEXT PRIMARY KEY REFERENCES cameras(id) ON DELETE CASCADE,next_run INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS recordings_path ON recordings(camera_id,path);
 CREATE INDEX IF NOT EXISTS recordings_start ON recordings(start);
`); err != nil {
		db.Close()
		return nil, err
	}
	if !fresh && version < 4 && os.Getenv("CTV_MAX_TRANSCODERS") == "" {
		if _, err = db.Exec(`INSERT INTO settings(id,version,payload) VALUES(1,1,'{"maxTranscoders":1}') ON CONFLICT(id) DO UPDATE SET payload=json_set(payload,'$.maxTranscoders',COALESCE(json_extract(payload,'$.maxTranscoders'),1))`); err != nil {
			db.Close()
			return nil, err
		}
	}
	if version < 4 {
		for {
			rows, e := db.Query(`SELECT id,path FROM recordings WHERE filename='' ORDER BY id LIMIT 1000`)
			if e != nil {
				db.Close()
				return nil, e
			}
			type entry struct{ id, path string }
			batch := []entry{}
			for rows.Next() {
				var item entry
				if e = rows.Scan(&item.id, &item.path); e != nil {
					rows.Close()
					db.Close()
					return nil, e
				}
				batch = append(batch, item)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				db.Close()
				return nil, e
			}
			if len(batch) == 0 {
				break
			}
			tx, e := db.Begin()
			if e != nil {
				db.Close()
				return nil, e
			}
			for _, item := range batch {
				if _, e = tx.Exec(`UPDATE recordings SET filename=? WHERE id=?`, filepath.Base(item.path), item.id); e != nil {
					tx.Rollback()
					db.Close()
					return nil, e
				}
			}
			if e = tx.Commit(); e != nil {
				db.Close()
				return nil, e
			}
		}
	}
	if _, err = db.Exec(`PRAGMA user_version=4`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Cameras() ([]Camera, error) {
	rows, e := s.Query(`SELECT id,name,folder,pattern,timezone,offset,format,prefix,indexing_mode,directory_pattern,view_mode,wide_marker,zoom_marker,zoom_offset,supports_zoom FROM cameras ORDER BY name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Camera{}
	for rows.Next() {
		var c Camera
		if e = rows.Scan(&c.ID, &c.Name, &c.Folder, &c.Pattern, &c.Timezone, &c.Offset, &c.Format, &c.Prefix, &c.IndexingMode, &c.DirectoryPattern, &c.ViewMode, &c.WideMarker, &c.ZoomMarker, &c.ZoomOffset, &c.SupportsZoom); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Camera(id string) (Camera, error) {
	var c Camera
	e := s.QueryRow(`SELECT id,name,folder,pattern,timezone,offset,format,prefix,indexing_mode,directory_pattern,view_mode,wide_marker,zoom_marker,zoom_offset,supports_zoom FROM cameras WHERE id=?`, id).Scan(&c.ID, &c.Name, &c.Folder, &c.Pattern, &c.Timezone, &c.Offset, &c.Format, &c.Prefix, &c.IndexingMode, &c.DirectoryPattern, &c.ViewMode, &c.WideMarker, &c.ZoomMarker, &c.ZoomOffset, &c.SupportsZoom)
	return c, e
}
func (s *Store) SaveCamera(c Camera) error {
	if c.ViewMode == "" {
		c.ViewMode = "single"
	}
	if c.IndexingMode == "" {
		c.IndexingMode = "partitioned"
	}
	if c.DirectoryPattern == "" {
		c.DirectoryPattern = "{YYYY}/{MM}/{DD}"
	}
	tx, e := s.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old Camera
	e = tx.QueryRow(`SELECT folder,pattern,timezone,offset,format,indexing_mode,directory_pattern FROM cameras WHERE id=?`, c.ID).Scan(&old.Folder, &old.Pattern, &old.Timezone, &old.Offset, &old.Format, &old.IndexingMode, &old.DirectoryPattern)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	changed := e == nil && (old.Folder != c.Folder || old.Pattern != c.Pattern || old.Timezone != c.Timezone || old.Format != cameraFormat(c) || old.IndexingMode != c.IndexingMode || old.DirectoryPattern != c.DirectoryPattern)
	if changed {
		for _, table := range []string{"camera_days", "camera_scans", "recordings", "scan_schedule"} {
			if _, e = tx.Exec(`DELETE FROM `+table+` WHERE camera_id=?`, c.ID); e != nil {
				return e
			}
		}
	} else if e == nil && old.Offset != c.Offset {
		if _, e = tx.Exec(`UPDATE recordings SET start=start+? WHERE camera_id=?`, c.Offset-old.Offset, c.ID); e != nil {
			return e
		}
	}
	_, e = tx.Exec(`INSERT INTO cameras(id,name,folder,pattern,timezone,offset,format,prefix,indexing_mode,directory_pattern,view_mode,wide_marker,zoom_marker,zoom_offset,supports_zoom) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,folder=excluded.folder,pattern=excluded.pattern,timezone=excluded.timezone,offset=excluded.offset,format=excluded.format,prefix=excluded.prefix,indexing_mode=excluded.indexing_mode,directory_pattern=excluded.directory_pattern,view_mode=excluded.view_mode,wide_marker=excluded.wide_marker,zoom_marker=excluded.zoom_marker,zoom_offset=excluded.zoom_offset,supports_zoom=excluded.supports_zoom`, c.ID, c.Name, c.Folder, c.Pattern, c.Timezone, c.Offset, cameraFormat(c), c.Prefix, c.IndexingMode, c.DirectoryPattern, c.ViewMode, c.WideMarker, c.ZoomMarker, c.ZoomOffset, c.SupportsZoom)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Recordings(camera string, from, to float64) ([]Recording, error) {
	c, err := s.Camera(camera)
	if err != nil {
		return nil, err
	}
	margin := math.Abs(c.ZoomOffset)
	rows, e := s.Query(`SELECT id,camera_id,path,start,duration,codec,size,mod FROM recordings WHERE camera_id=? AND start<? AND start+duration>? AND codec!='jpeg' ORDER BY start,id`, camera, to+margin, from-margin)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Recording{}
	for rows.Next() {
		var r Recording
		if e = rows.Scan(&r.ID, &r.CameraID, &r.Path, &r.Start, &r.Duration, &r.Codec, &r.Size, &r.Mod); e != nil {
			return nil, e
		}
		r.Container = strings.ToLower(filepath.Ext(r.Path))
		r.Kind = "video"
		if r.Codec == "jpeg" {
			r.Kind = "image"
		}
		r.View = recordingView(c, r.Path)
		if r.View == "zoom" {
			r.Start += c.ZoomOffset
		}
		if r.Start < to && r.Start+r.Duration > from {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}
func (s *Store) Recording(id string) (Recording, error) {
	var r Recording
	e := s.QueryRow(`SELECT id,camera_id,path,start,duration,codec,size,mod FROM recordings WHERE id=? AND codec!='jpeg'`, id).Scan(&r.ID, &r.CameraID, &r.Path, &r.Start, &r.Duration, &r.Codec, &r.Size, &r.Mod)
	r.Container = strings.ToLower(filepath.Ext(r.Path))
	r.Kind = "video"
	if r.Codec == "jpeg" {
		r.Kind = "image"
	}
	return r, e
}
func (s *Store) Cleanup(ctx context.Context) {
	s.ExecContext(ctx, `DELETE FROM sessions WHERE expires<?`, time.Now().Unix())
	s.ExecContext(ctx, `DELETE FROM oidc_flows WHERE expires<?`, time.Now().Unix())
}
