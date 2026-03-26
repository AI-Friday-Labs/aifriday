package main

import (
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"srv.exe.dev/db"
	slackbot "srv.exe.dev/slack"
	"golang.org/x/net/html"
)

var flagListenAddr = flag.String("listen", ":8000", "address to listen on")

// Meeting represents a scheduled meetup.
type Meeting struct {
	Date     string // "Friday, March 27, 2026"
	Short    string // "Mar 27"
	DatePath string // "2026/03/27"
	IsPast   bool
}

// BriefSummary is extracted from a brief's static HTML.
type BriefSummary struct {
	Date     string        // "Tuesday, March 25, 2026"
	Short    string        // "Mar 25"
	DatePath string        // "2026/03/25"
	Lede     template.HTML // inner HTML of brief-lede div
	Preview  string        // plain-text truncation
}

type HomeData struct {
	NextMeeting *Meeting
	LatestBrief *BriefSummary
}

type MeetingsData struct {
	Upcoming []Meeting
	Past     []Meeting
}

type BriefIndexData struct {
	Briefs []BriefSummary
}

type site struct {
	siteDir     string
	templateDir string
	mu          sync.RWMutex
	briefs      []BriefSummary
}

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

	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	siteDir := filepath.Join(projectRoot, "site")
	templateDir := filepath.Join(projectRoot, "srv", "templates")

	s := &site{
		siteDir:     siteDir,
		templateDir: templateDir,
	}

	if err := s.scanBriefs(); err != nil {
		slog.Warn("initial brief scan", "error", err)
	}

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.scanBriefs(); err != nil {
				slog.Warn("brief rescan", "error", err)
			}
		}
	}()

	// Open database for Slack link capture
	dbPath := filepath.Join(projectRoot, "aifriday.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	bot, err := slackbot.New(database)
	if err != nil {
		return fmt.Errorf("create slack bot: %w", err)
	}

	errCh := make(chan error, 2)

	go func() {
		mux := http.NewServeMux()

		// Dynamic pages
		mux.HandleFunc("GET /{$}", s.handleHome)
		mux.HandleFunc("GET /meetings/{$}", s.handleMeetings)
		mux.HandleFunc("GET /brief/{$}", s.handleBriefIndex)

		// Static brief content
		mux.Handle("GET /brief/", http.StripPrefix("/brief/",
			http.FileServer(http.Dir(filepath.Join(siteDir, "brief")))))

		// Static assets
		mux.Handle("GET /static/", http.StripPrefix("/static/",
			http.FileServer(http.Dir(filepath.Join(siteDir, "static")))))

		// Root-level static files
		for _, name := range []string{"favicon.ico", "apple-touch-icon.png", "icon-192.png", "icon-512.png"} {
			n := name
			mux.HandleFunc("GET /"+n, func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, filepath.Join(siteDir, n))
			})
		}

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

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *site) render(w http.ResponseWriter, name string, data any) {
	tmpl, err := template.ParseFiles(filepath.Join(s.templateDir, name))
	if err != nil {
		slog.Error("parse template", "name", name, "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("execute template", "name", name, "error", err)
	}
}

func (s *site) handleHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, "index.html", HomeData{
		NextMeeting: nextMeeting(),
		LatestBrief: s.latestBrief(),
	})
}

func (s *site) handleMeetings(w http.ResponseWriter, r *http.Request) {
	up, past := splitMeetings()
	s.render(w, "meetings.html", MeetingsData{Upcoming: up, Past: past})
}

func (s *site) handleBriefIndex(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	briefs := s.briefs
	s.mu.RUnlock()
	s.render(w, "brief-index.html", BriefIndexData{Briefs: briefs})
}

// ---------------------------------------------------------------------------
// Meetings
// ---------------------------------------------------------------------------

var meetingSchedule = []struct {
	Year  int
	Month time.Month
	Day   int
}{
	{2026, time.March, 27},
	{2026, time.April, 17},
	{2026, time.May, 15},
	{2026, time.June, 26},
	{2026, time.July, 17},
	{2026, time.August, 14},
	{2026, time.September, 18},
	{2026, time.October, 16},
	{2026, time.November, 13},
	{2026, time.December, 18},
}

func buildMeeting(year int, month time.Month, day int, now time.Time) Meeting {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	return Meeting{
		Date:     t.Format("Monday, January 2, 2006"),
		Short:    t.Format("Jan 2"),
		DatePath: fmt.Sprintf("%d/%02d/%02d", year, month, day),
		IsPast:   t.Before(now.Truncate(24 * time.Hour)),
	}
}

func nextMeeting() *Meeting {
	now := time.Now()
	for _, m := range meetingSchedule {
		mt := buildMeeting(m.Year, m.Month, m.Day, now)
		if !mt.IsPast {
			return &mt
		}
	}
	return nil
}

func splitMeetings() (upcoming, past []Meeting) {
	now := time.Now()
	for _, m := range meetingSchedule {
		mt := buildMeeting(m.Year, m.Month, m.Day, now)
		if mt.IsPast {
			past = append(past, mt)
		} else {
			upcoming = append(upcoming, mt)
		}
	}
	// Reverse past so newest first
	for i, j := 0, len(past)-1; i < j; i, j = i+1, j-1 {
		past[i], past[j] = past[j], past[i]
	}
	return
}

// ---------------------------------------------------------------------------
// Brief scanning
// ---------------------------------------------------------------------------

func (s *site) scanBriefs() error {
	briefsDir := filepath.Join(s.siteDir, "brief")
	var briefs []BriefSummary

	years, err := os.ReadDir(briefsDir)
	if err != nil {
		return err
	}
	for _, yEntry := range years {
		if !yEntry.IsDir() || len(yEntry.Name()) != 4 {
			continue
		}
		months, _ := os.ReadDir(filepath.Join(briefsDir, yEntry.Name()))
		for _, mEntry := range months {
			if !mEntry.IsDir() || len(mEntry.Name()) != 2 {
				continue
			}
			days, _ := os.ReadDir(filepath.Join(briefsDir, yEntry.Name(), mEntry.Name()))
			for _, dEntry := range days {
				if !dEntry.IsDir() {
					continue
				}
				path := filepath.Join(briefsDir, yEntry.Name(), mEntry.Name(), dEntry.Name(), "index.html")
				if _, err := os.Stat(path); err != nil {
					continue
				}
				brief, err := parseBriefFile(path, yEntry.Name(), mEntry.Name(), dEntry.Name())
				if err != nil {
					slog.Warn("parse brief", "path", path, "error", err)
					continue
				}
				briefs = append(briefs, brief)
			}
		}
	}

	sort.Slice(briefs, func(i, j int) bool {
		return briefs[i].DatePath > briefs[j].DatePath
	})

	s.mu.Lock()
	s.briefs = briefs
	s.mu.Unlock()

	slog.Info("scanned briefs", "count", len(briefs))
	return nil
}

func (s *site) latestBrief() *BriefSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.briefs) > 0 {
		b := s.briefs[0]
		return &b
	}
	return nil
}

func parseBriefFile(path, year, month, day string) (BriefSummary, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return BriefSummary{}, err
	}

	doc, err := html.Parse(strings.NewReader(string(content)))
	if err != nil {
		return BriefSummary{}, err
	}

	var ledeHTML string
	var dateStr string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "div" && nodeHasClass(n, "brief-lede") {
				ledeHTML = innerHTMLOf(n)
			}
			if n.Data == "h1" && nodeHasClass(n, "brief-date") {
				dateStr = textOf(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	plain := textOf2(doc, "brief-lede")
	if len(plain) > 160 {
		plain = plain[:157] + "…"
	}

	// Build short date like "Mar 25"
	var shortDate string
	if t, err := time.Parse("2006/01/02", year+"/"+month+"/"+day); err == nil {
		shortDate = t.Format("Jan 2")
	}

	return BriefSummary{
		Date:     dateStr,
		Short:    shortDate,
		DatePath: year + "/" + month + "/" + day,
		Lede:     template.HTML(ledeHTML),
		Preview:  plain,
	}, nil
}

// ---------------------------------------------------------------------------
// HTML helpers
// ---------------------------------------------------------------------------

func nodeHasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" && strings.Contains(a.Val, class) {
			return true
		}
	}
	return false
}

func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

func textOf2(doc *html.Node, class string) string {
	var result string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && nodeHasClass(n, class) {
			result = textOf(n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return result
}

func innerHTMLOf(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderHTML(&sb, c)
	}
	return sb.String()
}

func renderHTML(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(n.Data)
	case html.ElementNode:
		sb.WriteString("<" + n.Data)
		for _, a := range n.Attr {
			fmt.Fprintf(sb, ` %s="%s"`, a.Key, a.Val)
		}
		sb.WriteString(">")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderHTML(sb, c)
		}
		sb.WriteString("</" + n.Data + ">")
	}
}
