package restream

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSessionReturnsHostTokenOnlyWhenCreated(t *testing.T) {
	manager := NewManager(t.TempDir(), &fakeRunner{})
	server, err := NewServer(manager)
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	router := server.Router()
	body := []byte(`{"url":"https://www.youtube.com/watch?v=i6-j6_5aXL8"}`)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body)))
	if first.Code != http.StatusOK {
		t.Fatalf("first create status = %d body=%s", first.Code, first.Body.String())
	}
	var firstPayload createSessionResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode first payload: %v", err)
	}
	if firstPayload.HostToken == "" {
		t.Fatal("first create did not return host token")
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(body)))
	if second.Code != http.StatusOK {
		t.Fatalf("second create status = %d body=%s", second.Code, second.Body.String())
	}
	var secondPayload createSessionResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondPayload); err != nil {
		t.Fatalf("decode second payload: %v", err)
	}
	if secondPayload.HostToken != "" {
		t.Fatal("second create returned host token")
	}
}

func TestSetQualityRejectsViewer(t *testing.T) {
	manager := NewManager(t.TempDir(), &fakeRunner{})
	session, _, err := manager.GetOrCreate(t.Context(), "https://www.youtube.com/watch?v=i6-j6_5aXL8")
	if err != nil {
		t.Fatalf("GetOrCreate returned error: %v", err)
	}
	session.SetQualityOptions([]QualityOption{{ID: "best", Label: "Auto"}, {ID: "301", Label: "1080p60"}})
	server, err := NewServer(manager)
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}

	body := []byte(`{"qualityId":"301","hostToken":"wrong"}`)
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/quality", bytes.NewReader(body)))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("quality status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
