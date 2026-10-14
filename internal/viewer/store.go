package viewer

import (
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct{ *sql.DB }
type Camera struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Folder   string `json:"folder"`
	Pattern  string `json:"pattern"`
	Timezone string `json:"timezone"`
	Offset   int    `json:"offset"`
	Format   string `json:"format"`
	Prefix   string `json:"prefix"`
}
type Recording struct {
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
	file.Close()
	db, err := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS cameras(id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,pattern TEXT NOT NULL,timezone TEXT NOT NULL,offset INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS recordings(id TEXT PRIMARY KEY,camera_id TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,path TEXT NOT NULL,start REAL NOT NULL,duration REAL NOT NULL,codec TEXT NOT NULL,size INTEGER NOT NULL,mod INTEGER NOT NULL,UNIQUE(camera_id,path));
 CREATE INDEX IF NOT EXISTS recordings_time ON recordings(camera_id,start);
 CREATE TABLE IF NOT EXISTS camera_scans(camera_id TEXT PRIMARY KEY REFERENCES cameras(id) ON DELETE CASCADE,attempt INTEGER NOT NULL,success INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS admins(username TEXT PRIMARY KEY,hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(hash TEXT PRIMARY KEY,subject TEXT NOT NULL,name TEXT NOT NULL,role TEXT NOT NULL,kind TEXT NOT NULL,csrf TEXT NOT NULL,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS oidc_flows(hash TEXT PRIMARY KEY,nonce TEXT NOT NULL,verifier TEXT NOT NULL,expires INTEGER NOT NULL);
 PRAGMA user_version=1;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Additive migrations preserve existing accounts and cameras.
	for _, migration := range []struct{ table, column, ddl string }{
		{"camera_scans", "complete", "ALTER TABLE camera_scans ADD COLUMN complete INTEGER NOT NULL DEFAULT 0"},
		{"cameras", "format", "ALTER TABLE cameras ADD COLUMN format TEXT NOT NULL DEFAULT 'custom'"},
		{"cameras", "prefix", "ALTER TABLE cameras ADD COLUMN prefix TEXT NOT NULL DEFAULT ''"},
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
 CREATE TABLE IF NOT EXISTS bookmarks(id TEXT PRIMARY KEY,owner TEXT NOT NULL,time REAL NOT NULL,cameras TEXT NOT NULL,note TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS bookmarks_owner ON bookmarks(owner,time);
 PRAGMA user_version=3;`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Cameras() ([]Camera, error) {
	rows, e := s.Query(`SELECT id,name,folder,pattern,timezone,offset,format,prefix FROM cameras ORDER BY name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Camera{}
	for rows.Next() {
		var c Camera
		if e = rows.Scan(&c.ID, &c.Name, &c.Folder, &c.Pattern, &c.Timezone, &c.Offset, &c.Format, &c.Prefix); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) Camera(id string) (Camera, error) {
	var c Camera
	e := s.QueryRow(`SELECT id,name,folder,pattern,timezone,offset,format,prefix FROM cameras WHERE id=?`, id).Scan(&c.ID, &c.Name, &c.Folder, &c.Pattern, &c.Timezone, &c.Offset, &c.Format, &c.Prefix)
	return c, e
}
func (s *Store) SaveCamera(c Camera) error {
	tx, e := s.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var folder, pattern, zone, format, prefix string
	var offset int
	e = tx.QueryRow(`SELECT folder,pattern,timezone,offset,format,prefix FROM cameras WHERE id=?`, c.ID).Scan(&folder, &pattern, &zone, &offset, &format, &prefix)
	if e == nil && (folder != c.Folder || pattern != c.Pattern || zone != c.Timezone || offset != c.Offset || format != cameraFormat(c)) {
		if _, e = tx.Exec(`DELETE FROM camera_days WHERE camera_id=?`, c.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM camera_scans WHERE camera_id=?`, c.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM recordings WHERE camera_id=?`, c.ID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(`INSERT INTO cameras(id,name,folder,pattern,timezone,offset,format,prefix) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,folder=excluded.folder,pattern=excluded.pattern,timezone=excluded.timezone,offset=excluded.offset,format=excluded.format,prefix=excluded.prefix`, c.ID, c.Name, c.Folder, c.Pattern, c.Timezone, c.Offset, cameraFormat(c), c.Prefix); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Recordings(camera string, from, to float64) ([]Recording, error) {
	rows, e := s.Query(`SELECT id,camera_id,path,start,duration,codec,size,mod FROM recordings WHERE camera_id=? AND start<? AND (start+duration>? OR (codec='jpeg' AND start>=?)) ORDER BY start,id`, camera, to, from, from)
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
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Recording(id string) (Recording, error) {
	var r Recording
	e := s.QueryRow(`SELECT id,camera_id,path,start,duration,codec,size,mod FROM recordings WHERE id=?`, id).Scan(&r.ID, &r.CameraID, &r.Path, &r.Start, &r.Duration, &r.Codec, &r.Size, &r.Mod)
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
