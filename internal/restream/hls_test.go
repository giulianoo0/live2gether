package restream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeHLSNameAcceptsExpectedFiles(t *testing.T) {
	for _, name := range []string{"index.m3u8", "segment_00001.ts", "init_00001.m4s"} {
		if _, err := SafeHLSName(name); err != nil {
			t.Fatalf("SafeHLSName(%q) returned error: %v", name, err)
		}
	}
}

func TestSafeHLSNameRejectsTraversal(t *testing.T) {
	if _, err := SafeHLSName("../secret"); err == nil {
		t.Fatal("SafeHLSName accepted traversal")
	}
}

func TestSafeHLSNameRejectsUnexpectedExtension(t *testing.T) {
	if _, err := SafeHLSName("segment_00001.txt"); err == nil {
		t.Fatal("SafeHLSName accepted unexpected extension")
	}
}

func TestPrepareHLSDirRemovesStalePlaylist(t *testing.T) {
	dir := t.TempDir()
	stalePath := filepath.Join(dir, "index.m3u8")
	if err := os.WriteFile(stalePath, []byte("#EXTM3U\n#EXTINF:2,\nold.ts\n"), 0o644); err != nil {
		t.Fatalf("write stale playlist: %v", err)
	}

	if err := prepareHLSDir(dir); err != nil {
		t.Fatalf("prepareHLSDir returned error: %v", err)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale playlist still exists, stat err: %v", err)
	}
}
