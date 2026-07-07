package restream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Runner interface {
	Start(context.Context, *Session, RunOptions)
}

type RunOptions struct {
	QualityID   string
	CookiesText string
}

type CreateSessionOptions struct {
	BrowserSessionID string
}

type Manager struct {
	rootDir string
	runner  Runner
	store   CredentialStore

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager(rootDir string, runner Runner, stores ...CredentialStore) *Manager {
	var store CredentialStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return &Manager{
		rootDir:  rootDir,
		runner:   runner,
		store:    store,
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) CredentialStore() CredentialStore {
	return m.store
}

func (m *Manager) GetOrCreate(ctx context.Context, rawURL string, options ...CreateSessionOptions) (*Session, bool, error) {
	sourceURL, err := NormalizeSource(rawURL)
	if err != nil {
		return nil, false, err
	}

	var browserSessionID string
	if len(options) > 0 {
		browserSessionID = strings.TrimSpace(options[0].BrowserSessionID)
	}
	if browserSessionID != "" && m.store != nil {
		if _, err := m.store.EnsureBrowserSession(ctx, browserSessionID); err != nil {
			return nil, false, err
		}
	}

	idSource := sourceURL
	if browserSessionID != "" {
		idSource += "\x00" + browserSessionID
	}
	id := StableSessionID(idSource)
	m.mu.Lock()
	if session := m.sessions[id]; session != nil {
		m.mu.Unlock()
		if status := session.Status(); status == StatusFailed || status == StatusStopped {
			m.start(ctx, session)
		}
		return session, false, nil
	}

	hlsDir := filepath.Join(m.rootDir, id)
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		m.mu.Unlock()
		return nil, false, err
	}

	session := NewSession(id, sourceURL, hlsDir)
	session.BrowserSessionID = browserSessionID
	m.sessions[id] = session
	m.mu.Unlock()

	m.start(ctx, session)
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
	m.start(ctx, session)
	return nil
}

func (m *Manager) start(_ context.Context, session *Session) {
	runCtx, cancel := context.WithCancel(context.Background())
	session.SetRunCancel(cancel)
	options := RunOptions{QualityID: session.SelectedQualityID()}
	if m.store != nil && session.BrowserSessionID != "" {
		if cookiesText, ok, err := m.store.YouTubeCookies(runCtx, session.BrowserSessionID); err == nil && ok {
			options.CookiesText = cookiesText
		}
	}
	m.runner.Start(runCtx, session, options)
}
