package viewer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A stream whose creating request was abandoned is never fetched; it must not hold an
// encoder slot until the 90 s idle limit.
func TestPruneStreamsEndsUnfetchedStreams(t *testing.T) {
	a := testApp(t)
	created := time.Now().Add(-unfetchedStreamTTL - time.Second)
	for _, id := range []string{"orphan", "watched", "fresh"} {
		dir := filepath.Join(a.cfg.DataDir, "cache", "hls-"+id)
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		done := make(chan struct{})
		close(done)
		s := &stream{id: id, dir: dir, done: done, cancel: func() {}, created: created, last: created}
		switch id {
		case "watched":
			s.fetched, s.last = true, time.Now()
		case "fresh":
			s.created, s.last = time.Now(), time.Now()
		}
		a.mediaMu.Lock()
		a.streams[id] = s
		a.mediaMu.Unlock()
	}
	a.pruneStreams()
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if a.streams["orphan"] != nil {
		t.Fatal("unfetched stream outlived its grace period")
	}
	if a.streams["watched"] == nil || a.streams["fresh"] == nil {
		t.Fatal("fetched or new stream was removed", a.streams)
	}
}
