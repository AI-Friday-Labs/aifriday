// cmd/mailwatch watches ~/Maildir/new for incoming newsletter emails,
// extracts article links, and stores them in the database for the daily brief.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"srv.exe.dev/db"
	"srv.exe.dev/feeds"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Open database
	dbPath := filepath.Join("/home/exedev/ai-friday", "aifriday.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	if err := db.RunMigrations(database); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	maildir := filepath.Join(os.Getenv("HOME"), "Maildir")
	newDir := filepath.Join(maildir, "new")

	slog.Info("mail watcher starting", "maildir", newDir)

	// Initial processing of any backlog
	if err := feeds.ProcessMaildir(database, maildir); err != nil {
		slog.Warn("initial processing failed", "error", err)
	}

	// Poll every 60 seconds for new mail
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		entries, err := os.ReadDir(newDir)
		if err != nil {
			slog.Warn("read maildir", "error", err)
			continue
		}

		// Only process if there are .eml files
		hasEml := false
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".eml" {
				hasEml = true
				break
			}
		}
		if !hasEml {
			continue
		}

		if err := feeds.ProcessMaildir(database, maildir); err != nil {
			slog.Warn("processing failed", "error", err)
		}
	}

	return nil
}
