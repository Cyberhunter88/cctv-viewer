package viewer

import (
	"os"
	"path/filepath"
	"testing"
)

// Every chunk the frontend build writes must be served; an unlisted chunk would fail to
// load only when its page is opened. CI builds the frontend before the Go tests.
func TestBuiltChunksAreServed(t *testing.T) {
	chunks, e := filepath.Glob(filepath.Join("..", "..", "web", "dist", "chunks", "*.js"))
	if e != nil || len(chunks) == 0 {
		t.Skip("frontend not built")
	}
	for _, chunk := range chunks {
		name := "chunks/" + filepath.Base(chunk)
		if _, ok := staticAssetTypes[name]; !ok {
			t.Errorf("%s is built but not served; add it to staticAssetTypes", name)
		}
	}
	if _, e := os.Stat(filepath.Join("..", "..", "web", "dist", "app.js")); e != nil {
		t.Skip("frontend not built")
	}
}
