package viewer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"sort"
)

func (a *App) cleanupCandidates(camera string) ([]string, string, error) {
	c, err := a.db.Camera(camera)
	if err != nil {
		return nil, "", err
	}
	st, err := a.root.Stat(c.Folder)
	if err != nil || !st.IsDir() {
		return nil, "", errors.New("Kameraquelle nicht erreichbar")
	}
	seen := map[string]bool{}
	count := 0
	err = a.walkMedia(c.Folder, 0, func(name string, d fs.DirEntry) error {
		count++
		if count > 2000000 {
			return errors.New("zu viele Einträge")
		}
		if !d.IsDir() && supportedMedia(name) {
			seen[name] = true
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	rows, err := a.db.QueryContext(a.ctx, `SELECT id,path FROM recordings WHERE camera_id=?`, camera)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, "", err
		}
		if !seen[name] {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	sort.Strings(ids)
	h := sha256.New()
	h.Write([]byte(camera))
	for _, id := range ids {
		h.Write([]byte{0})
		h.Write([]byte(id))
	}
	return ids, hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) lockCleanup() bool {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.index.cleanup || a.index.status.Running || len(a.index.queue) > 0 || a.ctx.Err() != nil {
		return false
	}
	a.index.cleanup = true
	return true
}
func (a *App) unlockCleanup() { a.index.mu.Lock(); a.index.cleanup = false; a.index.mu.Unlock() }

func (a *App) previewIndexCleanup(w http.ResponseWriter, r *http.Request) {
	if !a.lockCleanup() {
		fail(w, 409, "Indexierung läuft")
		return
	}
	defer a.unlockCleanup()
	ids, digest, err := a.cleanupCandidates(r.URL.Query().Get("camera"))
	if err != nil {
		fail(w, 400, "Quelle nicht vollständig lesbar oder Kamera unbekannt")
		return
	}
	jsonOut(w, 200, map[string]any{"camera": r.URL.Query().Get("camera"), "count": len(ids), "digest": digest})
}
func (a *App) applyIndexCleanup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Camera string `json:"camera"`
		Digest string `json:"digest"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !a.lockCleanup() {
		fail(w, 409, "Indexierung läuft")
		return
	}
	defer a.unlockCleanup()
	ids, digest, err := a.cleanupCandidates(input.Camera)
	if err != nil {
		fail(w, 400, "Quelle nicht vollständig lesbar oder Kamera unbekannt")
		return
	}
	if input.Digest == "" || digest != input.Digest {
		fail(w, 409, "Bereinigungsvorschau ist veraltet")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Bereinigung fehlgeschlagen")
		return
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM recordings WHERE id=? AND camera_id=?`, id, input.Camera); err != nil {
			fail(w, 500, "Bereinigung fehlgeschlagen")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "Bereinigung fehlgeschlagen")
		return
	}
	jsonOut(w, 200, map[string]int{"removed": len(ids)})
}
