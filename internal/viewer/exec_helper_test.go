package viewer

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// testCommand starts external fixture tools (ffmpeg, ffprobe) with a deadline and ends
// them with the test, so a stuck tool fails one test quickly instead of the whole run.
func testCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	return exec.CommandContext(ctx, name, args...)
}
