// Package main is the entry point for the 10K Sites tracker server.
// Run: ./10ksites (or PORT=8080 ./10ksites)
//
// The binary is fully standalone: all HTML templates and CSS are embedded
// via go:embed. The database auto-creates at ~/.10ksites/tracker.db (local)
// or connects to Turso if TURSO_DATABASE_URL is set.
package main

import (
	"log"

	"10ksites/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
}
