package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	slackbot "srv.exe.dev/slack"
	"srv.exe.dev/srv"
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

	// Load .env file
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found", "error", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Start HTTP server
	server, err := srv.New("db.sqlite3", hostname)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Start Slack bot
	bot, err := slackbot.New()
	if err != nil {
		return fmt.Errorf("create slack bot: %w", err)
	}

	errCh := make(chan error, 2)

	go func() {
		slog.Info("starting HTTP server", "addr", *flagListenAddr)
		errCh <- server.Serve(*flagListenAddr)
	}()

	go func() {
		slog.Info("starting Slack bot")
		errCh <- bot.Run()
	}()

	// Wait for signal or error
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
