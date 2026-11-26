package viewer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func addTestStream(t *testing.T, a *App, id string, files map[string]int) *stream {
	t.Helper()
	dir := filepath.Join(a.cfg.DataDir, "cache", "hls-"+id)
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for name, size := range files {
		if e := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0600); e != nil {
			t.Fatal(e)
		}
	}
	s := &stream{id: id, dir: dir, done: make(chan struct{}), cancel: func() {}, created: time.Now(), last: time.Now(), fetched: true}
	a.mediaMu.Lock()
	a.streams[id] = s
	a.mediaMu.Unlock()
	t.Cleanup(func() {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	})
	return s
}

// Finished segments are measured once; the newest segment and the playlist keep
// growing while FFmpeg writes and are measured again every time.
func TestStreamUsageRemembersOnlyFinishedSegments(t *testing.T) {
	a := testApp(t)
	s := addTestStream(t, a, "usage", map[string]int{"index.m3u8": 10, "segment-000000.ts": 100, "segment-000001.ts": 50})
	if got := a.streamUsage(); got != 160 {
		t.Fatal("initial usage", got)
	}
	a.usageMu.Lock()
	_, first := s.sizes["segment-000000.ts"]
	_, newest := s.sizes["segment-000001.ts"]
	_, playlist := s.sizes["index.m3u8"]
	a.usageMu.Unlock()
	if !first || newest || playlist {
		t.Fatal("unexpected remembered files", s.sizes)
	}
	write := func(name string, size int) {
		if e := os.WriteFile(filepath.Join(s.dir, name), make([]byte, size), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("segment-000001.ts", 120)
	write("segment-000002.ts", 30)
	write("index.m3u8", 20)
	if got := a.streamUsage(); got != 100+120+30+20 {
		t.Fatal("usage after growth", got)
	}
}

// Over budget, streams are removed without measuring the remaining ones again,
// and only as many as needed: finished streams first, then the oldest.
func TestStreamBudgetEvictsOnlyAsNeeded(t *testing.T) {
	a := testApp(t)
	a.cfgMu.Lock()
	a.cfg.HLSTempBytes = 250
	a.cfgMu.Unlock()
	old := addTestStream(t, a, "old", map[string]int{"segment-000000.ts": 100})
	old.created = time.Now().Add(-time.Minute)
	finished := addTestStream(t, a, "finished", map[string]int{"segment-000000.ts": 100})
	close(finished.done)
	addTestStream(t, a, "new", map[string]int{"segment-000000.ts": 100})
	a.enforceStreamBudget()
	a.mediaMu.Lock()
	_, keptOld := a.streams["old"]
	_, keptFinished := a.streams["finished"]
	_, keptNew := a.streams["new"]
	a.mediaMu.Unlock()
	if keptFinished || !keptOld || !keptNew {
		t.Fatal("wrong eviction", keptOld, keptFinished, keptNew)
	}
	if _, e := os.Stat(finished.dir); !os.IsNotExist(e) {
		t.Fatal("evicted stream files retained", e)
	}
}

// Active stream directories count against the cache budget without being walked,
// and their segments are never pruned as previews.
func TestCachePruneCountsActiveStreams(t *testing.T) {
	a := testApp(t)
	cache := filepath.Join(a.cfg.DataDir, "cache")
	a.cfgMu.Lock()
	a.cfg.CacheBytes = 4000
	a.cfgMu.Unlock()
	addTestStream(t, a, "active", map[string]int{"segment-000000.ts": 2000})
	preview := filepath.Join(cache, "preview.jpg")
	if e := os.WriteFile(preview, make([]byte, 1500), 0600); e != nil {
		t.Fatal(e)
	}
	a.pruneCache()
	if _, e := os.Stat(preview); !os.IsNotExist(e) {
		t.Fatal("stream bytes not counted against the cache budget", e)
	}
	if _, e := os.Stat(filepath.Join(cache, "hls-active", "segment-000000.ts")); e != nil {
		t.Fatal("active stream segment removed", e)
	}
}
