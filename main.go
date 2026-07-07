package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"live2gether/internal/restream"
)

func main() {
	addr := listenAddr()
	dataDir := env("DATA_DIR", filepath.Join(os.TempDir(), "thingy-restream"))
	transcode := env("RESTREAM_TRANSCODE", "1") != "0"

	runner := restream.NewProcessRunner(transcode)
	manager := restream.NewManager(dataDir, runner)
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
