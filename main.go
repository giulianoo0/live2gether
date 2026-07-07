package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"live2gether/internal/restream"
)

func main() {
	ctx := context.Background()
	addr := listenAddr()
	dataDir := env("DATA_DIR", filepath.Join(os.TempDir(), "live2gether-restream"))
	transcode := env("RESTREAM_TRANSCODE", "1") != "0"

	store, closeStore, err := credentialStore(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer closeStore()

	runner := restream.NewProcessRunner(transcode)
	manager := restream.NewManager(dataDir, runner, store)
	server, err := restream.NewServer(manager)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on http://localhost%s", addr)
	log.Printf("hls data dir: %s", dataDir)
	if err := server.Router().Run(addr); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}

func credentialStore(ctx context.Context) (restream.CredentialStore, func(), error) {
	databaseURL := os.Getenv("DATABASE_URL")
	keyValue := os.Getenv("COOKIE_ENCRYPTION_KEY")
	if databaseURL == "" {
		key := restream.NewEphemeralEncryptionKey()
		return restream.NewMemoryCredentialStore(key), func() {}, nil
	}

	key, err := restream.EncryptionKeyFromString(keyValue)
	if err != nil {
		return nil, nil, err
	}
	store, err := restream.NewPostgresCredentialStore(ctx, databaseURL, key)
	if err != nil {
		return nil, nil, err
	}
	return store, store.Close, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func listenAddr() string {
	if addr := os.Getenv("ADDR"); addr != "" {
		return addr
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}
