package restream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Runner interface {
	Start(context.Context, *Session, string)
}

type Manager struct {
	rootDir string
	runner  Runner

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager(rootDir string, runner Runner) *Manager {
	return &Manager{
		rootDir:  rootDir,
		runner:   runner,
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) GetOrCreate(ctx context.Context, rawURL string) (*Session, bool, error) {
	_ = ctx
	sourceURL, err := NormalizeSource(rawURL)
	if err != nil {
		return nil, false, err
	}

	id := StableSessionID(sourceURL)
	m.mu.Lock()
	if session := m.sessions[id]; session != nil {
		m.mu.Unlock()
		return session, false, nil
	}

	hlsDir := filepath.Join(m.rootDir, id)
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		m.mu.Unlock()
		return nil, false, err
	}

	session := NewSession(id, sourceURL, hlsDir)
	m.sessions[id] = session
	m.mu.Unlock()

	m.start(session)
	return session, true, nil
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	return session, ok
}

func (m *Manager) SetQuality(ctx context.Context, id, qualityID, hostToken string) error {
	session, ok := m.Get(id)
	if !ok {
		return errors.New("session not found")
	}
	if !session.ValidateHostToken(hostToken) {
		return errors.New("host token is invalid")
	}
	if err := session.SelectQuality(qualityID); err != nil {
		return err
	}
	m.start(session)
	return nil
}

func (m *Manager) start(session *Session) {
	ctx, cancel := context.WithCancel(context.Background())
	session.SetRunCancel(cancel)
	m.runner.Start(ctx, session, session.SelectedQualityID())
}
