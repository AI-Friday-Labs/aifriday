package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/joho/godotenv"
	slackbot "srv.exe.dev/slack"
)

var flagListenAddr = flag.String("listen", ":8000", "address to listen on")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found", "error", err)
	}

	// Resolve site directory relative to this source file
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	siteDir := filepath.Join(projectRoot, "site")

	// Start Slack bot
	bot, err := slackbot.New()
	if err != nil {
		return fmt.Errorf("create slack bot: %w", err)
	}

	errCh := make(chan error, 2)

	// Static file server
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/", http.FileServer(http.Dir(siteDir)))
		slog.Info("starting HTTP server", "addr", *flagListenAddr, "site_dir", siteDir)
		errCh <- http.ListenAndServe(*flagListenAddr, mux)
	}()

	go func() {
		slog.Info("starting Slack bot")
		errCh <- bot.Run()
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("service error: %w", err)
	case sig := <-sigs:
		slog.Info("shutting down", "signal", sig)
		return nil
	}
}
