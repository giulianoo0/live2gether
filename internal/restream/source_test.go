package restream

import "testing"

func TestNormalizeSourceAcceptsHTTPSYouTubeURL(t *testing.T) {
	got, err := NormalizeSource("  https://www.youtube.com/watch?v=i6-j6_5aXL8  ")
	if err != nil {
		t.Fatalf("NormalizeSource returned error: %v", err)
	}

	if got != "https://www.youtube.com/watch?v=i6-j6_5aXL8" {
		t.Fatalf("NormalizeSource() = %q", got)
	}
}

func TestNormalizeSourceRejectsNonHTTPURL(t *testing.T) {
	_, err := NormalizeSource("file:///etc/passwd")
	if err == nil {
		t.Fatal("NormalizeSource accepted file URL")
	}
}

func TestNormalizeSourceRejectsLocalhostURL(t *testing.T) {
	_, err := NormalizeSource("http://127.0.0.1:9000/playlist.m3u8")
	if err == nil {
		t.Fatal("NormalizeSource accepted loopback URL")
	}
}

func TestNormalizeSourceRejectsNonYouTubeURL(t *testing.T) {
	_, err := NormalizeSource("https://example.com/watch?v=i6-j6_5aXL8")
	if err == nil {
		t.Fatal("NormalizeSource accepted a non-youtube URL")
	}
}

func TestStableSessionIDIsDeterministicURLSafeAndCompact(t *testing.T) {
	first := StableSessionID("https://www.youtube.com/watch?v=i6-j6_5aXL8")
	second := StableSessionID("https://www.youtube.com/watch?v=i6-j6_5aXL8")

	if first != second {
		t.Fatalf("StableSessionID not deterministic: %q != %q", first, second)
	}
	if len(first) != 22 {
		t.Fatalf("StableSessionID length = %d, want 22", len(first))
	}
	for _, r := range first {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			t.Fatalf("StableSessionID contains non-url-safe rune %q in %q", r, first)
		}
	}
}
