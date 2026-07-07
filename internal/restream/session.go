package restream

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusResolving Status = "resolving"
	StatusStarting  Status = "starting"
	StatusReady     Status = "ready"
	StatusFailed    Status = "failed"
	StatusStopped   Status = "stopped"
)

type Snapshot struct {
	ID                string          `json:"id"`
	URL               string          `json:"url"`
	Status            Status          `json:"status"`
	Message           string          `json:"message"`
	WatchPath         string          `json:"watchPath"`
	PlaylistPath      string          `json:"playlistPath"`
	PlaylistVersion   int             `json:"playlistVersion"`
	Qualities         []QualityOption `json:"qualities"`
	SelectedQualityID string          `json:"selectedQualityId"`
	ViewerCount       int             `json:"viewerCount"`
	Viewers           []string        `json:"viewers"`
	Chat              []ChatMessage   `json:"chat"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type QualityOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Height  int    `json:"height,omitempty"`
	FPS     int    `json:"fps,omitempty"`
	Bitrate int    `json:"bitrate,omitempty"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type Session struct {
	ID        string
	URL       string
	HLSDir    string
	HostToken string

	mu                sync.Mutex
	status            Status
	message           string
	updatedAt         time.Time
	qualities         []QualityOption
	selectedQualityID string
	playlistVersion   int
	runCancel         context.CancelFunc
	subscribers       map[chan Snapshot]struct{}
	viewers           map[string]string
	chat              []ChatMessage
}

func NewSession(id, sourceURL, hlsDir string) *Session {
	return &Session{
		ID:                id,
		URL:               sourceURL,
		HLSDir:            hlsDir,
		HostToken:         newHostToken(),
		status:            StatusQueued,
		message:           "Waiting for restreamer",
		updatedAt:         time.Now(),
		qualities:         []QualityOption{{ID: "best", Label: "Auto"}},
		selectedQualityID: "best",
		subscribers:       make(map[chan Snapshot]struct{}),
		viewers:           make(map[string]string),
	}
}

func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Session) SetStatus(status Status, message string) {
	s.mu.Lock()
	s.status = status
	s.message = message
	s.updatedAt = time.Now()
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *Session) SetQualityOptions(options []QualityOption) {
	if len(options) == 0 {
		options = []QualityOption{{ID: "best", Label: "Auto"}}
	}

	s.mu.Lock()
	s.qualities = append([]QualityOption(nil), options...)
	if !qualityExists(s.qualities, s.selectedQualityID) {
		s.selectedQualityID = s.qualities[0].ID
	}
	s.updatedAt = time.Now()
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *Session) SelectQuality(qualityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !qualityExists(s.qualities, qualityID) {
		return errors.New("unknown quality")
	}
	s.selectedQualityID = qualityID
	s.playlistVersion++
	s.updatedAt = time.Now()
	s.broadcastLocked()
	return nil
}

func (s *Session) SelectedQualityID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectedQualityID
}

func (s *Session) ValidateHostToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return token != "" && token == s.HostToken
}

func (s *Session) SetRunCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	oldCancel := s.runCancel
	s.runCancel = cancel
	s.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
}

func (s *Session) Subscribe(ctx context.Context) <-chan Snapshot {
	ch := make(chan Snapshot, 8)

	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	ch <- s.snapshotLocked()
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subscribers, ch)
		close(ch)
		s.mu.Unlock()
	}()

	return ch
}

func (s *Session) SubscribeViewer(ctx context.Context) (string, string, <-chan Snapshot) {
	viewerID := randomToken(9)
	name := randomViewerName()
	ch := make(chan Snapshot, 8)

	s.mu.Lock()
	s.viewers[viewerID] = name
	s.subscribers[ch] = struct{}{}
	s.broadcastLocked()
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.viewers, viewerID)
		delete(s.subscribers, ch)
		s.broadcastLocked()
		close(ch)
		s.mu.Unlock()
	}()

	return viewerID, name, ch
}

func (s *Session) AddChat(viewerID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(text) > 500 {
		text = text[:500]
	}

	s.mu.Lock()
	author := s.viewers[viewerID]
	if author == "" {
		author = "viewer"
	}
	s.chat = append(s.chat, ChatMessage{
		ID:        randomToken(9),
		Author:    author,
		Text:      text,
		CreatedAt: time.Now(),
	})
	if len(s.chat) > 50 {
		s.chat = s.chat[len(s.chat)-50:]
	}
	s.updatedAt = time.Now()
	s.broadcastLocked()
	s.mu.Unlock()
}

func (s *Session) snapshotLocked() Snapshot {
	qualities := append([]QualityOption(nil), s.qualities...)
	viewers := make([]string, 0, len(s.viewers))
	for _, name := range s.viewers {
		viewers = append(viewers, name)
	}
	sort.Strings(viewers)
	chat := append([]ChatMessage(nil), s.chat...)
	return Snapshot{
		ID:                s.ID,
		URL:               s.URL,
		Status:            s.status,
		Message:           s.message,
		WatchPath:         "/watch/" + s.ID,
		PlaylistPath:      "/hls/" + s.ID + "/index.m3u8",
		PlaylistVersion:   s.playlistVersion,
		Qualities:         qualities,
		SelectedQualityID: s.selectedQualityID,
		ViewerCount:       len(viewers),
		Viewers:           viewers,
		Chat:              chat,
		UpdatedAt:         s.updatedAt,
	}
}

func (s *Session) broadcastLocked() {
	snapshot := s.snapshotLocked()
	for ch := range s.subscribers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

func qualityExists(options []QualityOption, id string) bool {
	for _, option := range options {
		if option.ID == id {
			return true
		}
	}
	return false
}

func newHostToken() string {
	return randomToken(24)
}

func randomToken(size int) string {
	raw := make([]byte, size)
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

func randomViewerName() string {
	adjectives := []string{"blue", "quiet", "rapid", "clear", "bright", "steady", "silver", "north", "signal", "live"}
	nouns := []string{"viewer", "pilot", "caster", "signal", "guest", "node", "orbit", "wave", "runner", "watcher"}
	a := randomIndex(len(adjectives))
	n := randomIndex(len(nouns))
	return adjectives[a] + "-" + nouns[n]
}

func randomIndex(max int) int {
	if max <= 0 {
		return 0
	}
	var raw [1]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0
	}
	return int(raw[0]) % max
}
