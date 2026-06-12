package main

import (
	"log"
	"net/http"
	"os"

	"rootaika/server/internal/rootaika"
)

func main() {
	dbPath := env("ROOTAIKA_DB_PATH", "rootaika.db")
	addr := env("ROOTAIKA_ADDR", ":8080")

	store, err := rootaika.OpenStore(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	log.Printf("rootaika server listening on %s, db=%s", addr, dbPath)
	if err := http.ListenAndServe(addr, rootaika.NewApp(store)); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
