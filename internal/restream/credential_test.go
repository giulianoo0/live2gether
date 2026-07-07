package restream

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testEncryptionKey(t *testing.T) []byte {
	t.Helper()
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestMemoryCredentialStorePersistsCookieMetadataWithoutReturningCookieValues(t *testing.T) {
	store := NewMemoryCredentialStore(testEncryptionKey(t))
	info, err := store.EnsureBrowserSession(t.Context(), "")
	if err != nil {
		t.Fatalf("EnsureBrowserSession returned error: %v", err)
	}
	if info.ID == "" {
		t.Fatal("session id is empty")
	}
	if info.HasYouTubeCookies {
		t.Fatal("new browser session unexpectedly has cookies")
	}

	cookies := ".youtube.com\tTRUE\t/\tTRUE\t1893456000\tSID\tsecret"
	if err := store.SaveYouTubeCookies(t.Context(), info.ID, cookies); err != nil {
		t.Fatalf("SaveYouTubeCookies returned error: %v", err)
	}

	info, err = store.EnsureBrowserSession(t.Context(), info.ID)
	if err != nil {
		t.Fatalf("EnsureBrowserSession existing returned error: %v", err)
	}
	if !info.HasYouTubeCookies {
		t.Fatal("saved cookie metadata did not persist")
	}

	got, ok, err := store.YouTubeCookies(t.Context(), info.ID)
	if err != nil {
		t.Fatalf("YouTubeCookies returned error: %v", err)
	}
	if !ok || got != cookies {
		t.Fatalf("YouTubeCookies = %q, %v; want saved cookies", got, ok)
	}
}

func TestCredentialStoreRejectsOversizedOrNonYouTubeCookies(t *testing.T) {
	store := NewMemoryCredentialStore(testEncryptionKey(t))
	info, err := store.EnsureBrowserSession(t.Context(), "")
	if err != nil {
		t.Fatalf("EnsureBrowserSession returned error: %v", err)
	}

	if err := store.SaveYouTubeCookies(t.Context(), info.ID, "example.com\tFALSE\t/\tFALSE\t0\tSID\tsecret"); err == nil {
		t.Fatal("SaveYouTubeCookies accepted non-youtube cookies")
	}
	if err := store.SaveYouTubeCookies(t.Context(), info.ID, string(bytes.Repeat([]byte("a"), maxCookieBytes+1))); err == nil {
		t.Fatal("SaveYouTubeCookies accepted oversized cookies")
	}
}

func TestBrowserSessionAPIStoresCookieProfileAndReturnsOnlyMetadata(t *testing.T) {
	store := NewMemoryCredentialStore(testEncryptionKey(t))
	manager := NewManager(t.TempDir(), &fakeRunner{}, store)
	server, err := NewServer(manager)
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	router := server.Router()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/browser-sessions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("create browser session status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var created BrowserSessionInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode browser session: %v", err)
	}
	if created.ID == "" {
		t.Fatal("browser session id is empty")
	}

	body := []byte(`{"cookiesText":".youtube.com\tTRUE\t/\tTRUE\t1893456000\tSID\tsecret"}`)
	save := httptest.NewRecorder()
	router.ServeHTTP(save, httptest.NewRequest(http.MethodPost, "/api/browser-sessions/"+created.ID+"/youtube-cookies", bytes.NewReader(body)))
	if save.Code != http.StatusOK {
		t.Fatalf("save cookies status = %d body=%s", save.Code, save.Body.String())
	}
	if bytes.Contains(save.Body.Bytes(), []byte("secret")) {
		t.Fatal("save response leaked cookie value")
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/browser-sessions/"+created.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get browser session status = %d body=%s", get.Code, get.Body.String())
	}
	if bytes.Contains(get.Body.Bytes(), []byte("secret")) {
		t.Fatal("metadata response leaked cookie value")
	}
	var info BrowserSessionInfo
	if err := json.Unmarshal(get.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if !info.HasYouTubeCookies {
		t.Fatal("metadata did not report stored youtube cookies")
	}

	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/browser-sessions/"+created.ID+"/youtube-cookies", nil))
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete cookies status = %d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	var deleted BrowserSessionInfo
	if err := json.Unmarshal(deleteRecorder.Body.Bytes(), &deleted); err != nil {
		t.Fatalf("decode deleted metadata: %v", err)
	}
	if deleted.HasYouTubeCookies {
		t.Fatal("metadata still reports youtube cookies after delete")
	}
}
