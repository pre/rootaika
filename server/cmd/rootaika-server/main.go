package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"rootaika/server/internal/rootaika"
)

func main() {
	dbPath := env("ROOTAIKA_DB_PATH", "rootaika.db")
	addr := env("ROOTAIKA_ADDR", ":8080")
	dataDir := env("ROOTAIKA_DATA_DIR", filepath.Dir(dbPath))

	store, err := rootaika.OpenStore(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	log.Printf("rootaika server listening on %s, db=%s, data_dir=%s", addr, dbPath, dataDir)
	app := rootaika.NewApp(store).WithDataDir(dataDir)
	if err := http.ListenAndServe(addr, app); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
