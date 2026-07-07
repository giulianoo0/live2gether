package restream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxCookieBytes = 128 * 1024

type BrowserSessionInfo struct {
	ID                string    `json:"id"`
	HasYouTubeCookies bool      `json:"hasYouTubeCookies"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type CredentialStore interface {
	EnsureBrowserSession(context.Context, string) (BrowserSessionInfo, error)
	SaveYouTubeCookies(context.Context, string, string) error
	YouTubeCookies(context.Context, string) (string, bool, error)
	DeleteYouTubeCookies(context.Context, string) error
}

type encryptedCookieProfile struct {
	Nonce      []byte
	Ciphertext []byte
	UpdatedAt  time.Time
}

type MemoryCredentialStore struct {
	key      []byte
	mu       sync.Mutex
	sessions map[string]time.Time
	cookies  map[string]encryptedCookieProfile
}

func NewMemoryCredentialStore(key []byte) *MemoryCredentialStore {
	return &MemoryCredentialStore{
		key:      append([]byte(nil), key...),
		sessions: make(map[string]time.Time),
		cookies:  make(map[string]encryptedCookieProfile),
	}
}

func (s *MemoryCredentialStore) EnsureBrowserSession(_ context.Context, id string) (BrowserSessionInfo, error) {
	if id != "" && !validBrowserSessionID(id) {
		return BrowserSessionInfo{}, errors.New("browser session id is invalid")
	}
	if id == "" {
		id = randomToken(24)
	}
	now := time.Now().UTC()

	s.mu.Lock()
	if _, ok := s.sessions[id]; !ok {
		s.sessions[id] = now
	}
	profile, hasCookies := s.cookies[id]
	if hasCookies {
		now = profile.UpdatedAt
	}
	s.mu.Unlock()

	return BrowserSessionInfo{ID: id, HasYouTubeCookies: hasCookies, UpdatedAt: now}, nil
}

func (s *MemoryCredentialStore) SaveYouTubeCookies(ctx context.Context, id, cookiesText string) error {
	if _, err := s.EnsureBrowserSession(ctx, id); err != nil {
		return err
	}
	if err := validateYouTubeCookies(cookiesText); err != nil {
		return err
	}
	nonce, ciphertext, err := encryptString(s.key, cookiesText)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.cookies[id] = encryptedCookieProfile{Nonce: nonce, Ciphertext: ciphertext, UpdatedAt: time.Now().UTC()}
	s.mu.Unlock()
	return nil
}

func (s *MemoryCredentialStore) YouTubeCookies(ctx context.Context, id string) (string, bool, error) {
	if _, err := s.EnsureBrowserSession(ctx, id); err != nil {
		return "", false, err
	}
	s.mu.Lock()
	profile, ok := s.cookies[id]
	s.mu.Unlock()
	if !ok {
		return "", false, nil
	}
	text, err := decryptString(s.key, profile.Nonce, profile.Ciphertext)
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

func (s *MemoryCredentialStore) DeleteYouTubeCookies(ctx context.Context, id string) error {
	if _, err := s.EnsureBrowserSession(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cookies, id)
	s.mu.Unlock()
	return nil
}

type PostgresCredentialStore struct {
	pool *pgxpool.Pool
	key  []byte
}

func NewPostgresCredentialStore(ctx context.Context, databaseURL string, key []byte) (*PostgresCredentialStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	store := &PostgresCredentialStore{pool: pool, key: append([]byte(nil), key...)}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresCredentialStore) Close() {
	s.pool.Close()
}

func (s *PostgresCredentialStore) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
create table if not exists browser_sessions (
	id text primary key,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);
create table if not exists youtube_cookie_profiles (
	session_id text primary key references browser_sessions(id) on delete cascade,
	nonce bytea not null,
	ciphertext bytea not null,
	updated_at timestamptz not null default now()
);`)
	return err
}

func (s *PostgresCredentialStore) EnsureBrowserSession(ctx context.Context, id string) (BrowserSessionInfo, error) {
	if id != "" && !validBrowserSessionID(id) {
		return BrowserSessionInfo{}, errors.New("browser session id is invalid")
	}
	if id == "" {
		id = randomToken(24)
	}

	var updatedAt time.Time
	if err := s.pool.QueryRow(ctx, `
insert into browser_sessions (id) values ($1)
on conflict (id) do update set updated_at = now()
returning updated_at`, id).Scan(&updatedAt); err != nil {
		return BrowserSessionInfo{}, err
	}

	var hasCookies bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from youtube_cookie_profiles where session_id = $1)`, id).Scan(&hasCookies); err != nil {
		return BrowserSessionInfo{}, err
	}
	return BrowserSessionInfo{ID: id, HasYouTubeCookies: hasCookies, UpdatedAt: updatedAt}, nil
}

func (s *PostgresCredentialStore) SaveYouTubeCookies(ctx context.Context, id, cookiesText string) error {
	if _, err := s.EnsureBrowserSession(ctx, id); err != nil {
		return err
	}
	if err := validateYouTubeCookies(cookiesText); err != nil {
		return err
	}
	nonce, ciphertext, err := encryptString(s.key, cookiesText)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
insert into youtube_cookie_profiles (session_id, nonce, ciphertext, updated_at)
values ($1, $2, $3, now())
on conflict (session_id) do update set nonce = excluded.nonce, ciphertext = excluded.ciphertext, updated_at = now()`,
		id, nonce, ciphertext)
	return err
}

func (s *PostgresCredentialStore) YouTubeCookies(ctx context.Context, id string) (string, bool, error) {
	if id == "" || !validBrowserSessionID(id) {
		return "", false, nil
	}

	var nonce, ciphertext []byte
	err := s.pool.QueryRow(ctx, `select nonce, ciphertext from youtube_cookie_profiles where session_id = $1`, id).Scan(&nonce, &ciphertext)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	text, err := decryptString(s.key, nonce, ciphertext)
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

func (s *PostgresCredentialStore) DeleteYouTubeCookies(ctx context.Context, id string) error {
	if _, err := s.EnsureBrowserSession(ctx, id); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `delete from youtube_cookie_profiles where session_id = $1`, id)
	return err
}

func EncryptionKeyFromString(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("COOKIE_ENCRYPTION_KEY is required when DATABASE_URL is set")
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(value) == 32 {
		return []byte(value), nil
	}
	return nil, errors.New("COOKIE_ENCRYPTION_KEY must be 32 raw bytes or base64-encoded 32 bytes")
}

func NewEphemeralEncryptionKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

func validateYouTubeCookies(text string) error {
	if len(text) > maxCookieBytes {
		return fmt.Errorf("cookies file is too large; max is %d bytes", maxCookieBytes)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("cookies text is required")
	}

	hasYouTubeCookie := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, r := range line {
			if r < 0x20 && r != '\t' {
				return errors.New("cookies text contains invalid control characters")
			}
		}
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			domain := strings.TrimPrefix(strings.ToLower(fields[0]), ".")
			if domain == "youtube.com" || strings.HasSuffix(domain, ".youtube.com") || domain == "google.com" || strings.HasSuffix(domain, ".google.com") {
				hasYouTubeCookie = true
			}
		}
	}
	if !hasYouTubeCookie {
		return errors.New("cookies text must include youtube.com or google.com cookies")
	}
	return nil
}

func validBrowserSessionID(id string) bool {
	if len(id) < 24 || len(id) > 96 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func encryptString(key []byte, plaintext string) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, []byte(plaintext), nil), nil
}

func decryptString(key, nonce, ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
