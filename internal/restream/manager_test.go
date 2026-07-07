package restream

import (
	"context"
	"testing"
)

type fakeRunner struct {
	starts    int
	qualities []string
	options   []RunOptions
}

func (f *fakeRunner) Start(_ context.Context, session *Session, options RunOptions) {
	f.starts++
	f.qualities = append(f.qualities, options.QualityID)
	f.options = append(f.options, options)
	session.SetStatus(StatusReady, "Stream ready")
}

func TestManagerReusesSessionForSameURL(t *testing.T) {
	runner := &fakeRunner{}
	manager := NewManager(t.TempDir(), runner)

	first, created, err := manager.GetOrCreate(context.Background(), "https://www.youtube.com/watch?v=i6-j6_5aXL8")
	if err != nil {
		t.Fatalf("GetOrCreate first returned error: %v", err)
	}
	if !created {
		t.Fatal("first GetOrCreate did not report created")
	}
	second, created, err := manager.GetOrCreate(context.Background(), "https://www.youtube.com/watch?v=i6-j6_5aXL8")
	if err != nil {
		t.Fatalf("GetOrCreate second returned error: %v", err)
	}
	if created {
		t.Fatal("second GetOrCreate reported created")
	}

	if first != second {
		t.Fatal("GetOrCreate did not reuse the existing session")
	}
	if runner.starts != 1 {
		t.Fatalf("runner starts = %d, want 1", runner.starts)
	}
}

func TestSessionSnapshotIncludesShareAndStreamPaths(t *testing.T) {
	session := NewSession("abc123", "https://www.youtube.com/watch?v=i6-j6_5aXL8", "/tmp/session")
	snapshot := session.Snapshot()

	if snapshot.ID != "abc123" {
		t.Fatalf("snapshot ID = %q", snapshot.ID)
	}
	if snapshot.WatchPath != "/watch/abc123" {
		t.Fatalf("snapshot WatchPath = %q", snapshot.WatchPath)
	}
	if snapshot.PlaylistPath != "/hls/abc123/index.m3u8" {
		t.Fatalf("snapshot PlaylistPath = %q", snapshot.PlaylistPath)
	}
}

func TestSessionSnapshotIncludesQualityState(t *testing.T) {
	session := NewSession("abc123", "https://www.youtube.com/watch?v=i6-j6_5aXL8", "/tmp/session")
	session.SetQualityOptions([]QualityOption{
		{ID: "best", Label: "Auto"},
		{ID: "301", Label: "1080p60"},
	})
	if err := session.SelectQuality("301"); err != nil {
		t.Fatalf("SelectQuality returned error: %v", err)
	}

	snapshot := session.Snapshot()
	if snapshot.SelectedQualityID != "301" {
		t.Fatalf("snapshot SelectedQualityID = %q", snapshot.SelectedQualityID)
	}
	if len(snapshot.Qualities) != 2 {
		t.Fatalf("snapshot qualities length = %d", len(snapshot.Qualities))
	}
}

func TestManagerSetQualityRequiresHostToken(t *testing.T) {
	runner := &fakeRunner{}
	manager := NewManager(t.TempDir(), runner)
	session, _, err := manager.GetOrCreate(context.Background(), "https://www.youtube.com/watch?v=i6-j6_5aXL8")
	if err != nil {
		t.Fatalf("GetOrCreate returned error: %v", err)
	}
	session.SetQualityOptions([]QualityOption{{ID: "best", Label: "Auto"}, {ID: "301", Label: "1080p60"}})

	if err := manager.SetQuality(context.Background(), session.ID, "301", "wrong-token"); err == nil {
		t.Fatal("SetQuality accepted an invalid host token")
	}

	if err := manager.SetQuality(context.Background(), session.ID, "301", session.HostToken); err != nil {
		t.Fatalf("SetQuality returned error with host token: %v", err)
	}
	if runner.starts != 2 {
		t.Fatalf("runner starts = %d, want 2", runner.starts)
	}
	if runner.qualities[1] != "301" {
		t.Fatalf("restarted quality = %q, want 301", runner.qualities[1])
	}
}

func TestManagerPassesStoredYouTubeCookiesToRunner(t *testing.T) {
	store := NewMemoryCredentialStore(testEncryptionKey(t))
	info, err := store.EnsureBrowserSession(context.Background(), "")
	if err != nil {
		t.Fatalf("EnsureBrowserSession returned error: %v", err)
	}
	cookies := ".youtube.com\tTRUE\t/\tTRUE\t1893456000\tSID\tsecret"
	if err := store.SaveYouTubeCookies(context.Background(), info.ID, cookies); err != nil {
		t.Fatalf("SaveYouTubeCookies returned error: %v", err)
	}

	runner := &fakeRunner{}
	manager := NewManager(t.TempDir(), runner, store)
	if _, _, err := manager.GetOrCreate(context.Background(), "https://www.youtube.com/watch?v=i6-j6_5aXL8", CreateSessionOptions{BrowserSessionID: info.ID}); err != nil {
		t.Fatalf("GetOrCreate returned error: %v", err)
	}

	if runner.starts != 1 {
		t.Fatalf("runner starts = %d, want 1", runner.starts)
	}
	if runner.options[0].CookiesText != cookies {
		t.Fatalf("runner cookies = %q, want saved cookies", runner.options[0].CookiesText)
	}
}

func TestManagerRestartsFailedSessionOnCreate(t *testing.T) {
	runner := &fakeRunner{}
	manager := NewManager(t.TempDir(), runner)
	session, _, err := manager.GetOrCreate(context.Background(), "https://www.youtube.com/watch?v=i6-j6_5aXL8")
	if err != nil {
		t.Fatalf("GetOrCreate returned error: %v", err)
	}
	session.SetStatus(StatusFailed, "needs cookies")

	again, created, err := manager.GetOrCreate(context.Background(), "https://www.youtube.com/watch?v=i6-j6_5aXL8")
	if err != nil {
		t.Fatalf("second GetOrCreate returned error: %v", err)
	}
	if created {
		t.Fatal("failed session recreate reported created")
	}
	if again != session {
		t.Fatal("failed session recreate returned a different session")
	}
	if runner.starts != 2 {
		t.Fatalf("runner starts = %d, want 2", runner.starts)
	}
}
