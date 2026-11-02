package viewer

import (
	"strings"
	"testing"
	"time"
)

func FuzzParseTimestamp(f *testing.F) {
	f.Add("{YYYY}{MM}{DD}-{hh}{mm}{ss}.mp4", "20260101-120000.mp4", "Europe/Berlin", 0.0)
	f.Add("{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", "2026/03/29/02-30-00.mp4", "Europe/Berlin", 1.5)
	f.Add("../x{YYYY}", "", "UTC", 0.0)
	f.Fuzz(func(t *testing.T, pattern, name, zone string, offset float64) {
		got, err := ParseTimestamp(pattern, name, zone, offset)
		if err == nil && got.Location() != time.UTC {
			t.Fatalf("result must be UTC, got %v", got.Location())
		}
	})
}

func FuzzPartitionStaysInsideFolder(f *testing.F) {
	f.Add("cam", "{YYYY}/{MM}/{DD}", "{YYYY}{MM}{DD}-{hh}{mm}{ss}.mp4", "automatic", "partitioned")
	f.Add("cam", "..", "x", "custom", "full")
	f.Fuzz(func(t *testing.T, folder, directory, pattern, format, mode string) {
		c := Camera{Folder: folder, DirectoryPattern: directory, Pattern: pattern, Format: format, IndexingMode: mode}
		p, err := partition(c, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
		if err == nil && !validRelative(p) && mode != "full" {
			t.Fatalf("partition %q is not a valid relative path", p)
		}
	})
}

func FuzzListTrimsAndDropsEmpty(f *testing.F) {
	f.Add("a, b,,c ")
	f.Fuzz(func(t *testing.T, s string) {
		for _, item := range list(s) {
			if item == "" || item != strings.TrimSpace(item) || strings.Contains(item, ",") {
				t.Fatalf("bad item %q", item)
			}
		}
	})
}
