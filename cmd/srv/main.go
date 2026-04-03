package main

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
	Number   int    // sequential meeting number (0 = not yet held)
	Date     string // "Friday, March 27, 2026"
	Short    string // "Mar 27"
	DatePath string // "2026/03/27"
	IsPast   bool
	HasRecap bool   // true if a recap file exists
	Start    string // "9:30 AM" — empty if not yet announced
	End      string // "11:00 AM"
	Location string // full address
	Hint     string // rotating survey hint, optional
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

type MeetingDetailData struct {
	Number            int
	Date              string
	DateISO           string
	IsPast            bool
	HasDetails        bool          // true if Start/Location are set
	Start             string
	End               string
	Location          string
	Hint              string
	RecapHTML         template.HTML
	RSVPSubmitted     bool
	IsUpdate          bool
	FormError         string
	FormName          string
	FormEmail         string
	GoogleCalendarURL string
}

type BriefIndexData struct {
	Briefs []BriefSummary
}

type site struct {
	siteDir     string
	templateDir string
	recapsDir   string
	mu          sync.RWMutex
	briefs      []BriefSummary
	db          *sql.DB
	bot         *slackbot.Bot
	rsvpCfg     RSVPConfig
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

	recapsDir := filepath.Join(projectRoot, "srv", "recaps")

	var s *site

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

	s = &site{
		siteDir:     siteDir,
		templateDir: templateDir,
		recapsDir:   recapsDir,
		db:          database,
		bot:         bot,
		rsvpCfg: RSVPConfig{
			FastmailPassword: os.Getenv("FASTMAIL_APP_PASSWORD"),
			ButtondownAPIKey: os.Getenv("BUTTONDOWN_API_KEY"),
			SlackRSVPChannel: os.Getenv("SLACK_RSVP_CHANNEL"),
		},
	}

	errCh := make(chan error, 2)

	go func() {
		mux := http.NewServeMux()

		// Dynamic pages
		mux.HandleFunc("GET /{$}", s.handleHome)
		mux.HandleFunc("GET /meetings/{$}", s.handleMeetings)
		mux.HandleFunc("GET /meetings/{number}", s.handleMeetingDetail)
		mux.HandleFunc("GET /rsvp", s.handleRSVPRedirect)
		mux.HandleFunc("POST /meetings/{number}/rsvp", s.handleRSVPSubmit)
		mux.HandleFunc("GET /meetings/{number}/invite.ics", s.handleICSDownload)
		mux.HandleFunc("GET /brief/{$}", s.handleBriefIndex)

		// SEO: robots.txt, sitemap, RSS feed
		mux.HandleFunc("GET /robots.txt", handleRobots)
		mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
		mux.HandleFunc("GET /feed.xml", s.handleFeed)

		// Static brief content
		mux.Handle("GET /brief/", http.StripPrefix("/brief/",
			http.FileServer(http.Dir(filepath.Join(siteDir, "brief")))))

		// Static assets
		mux.Handle("GET /static/", http.StripPrefix("/static/",
			http.FileServer(http.Dir(filepath.Join(siteDir, "static")))))

		// Root-level static files
		for _, name := range []string{"favicon.ico", "apple-touch-icon.png", "icon-192.png", "icon-512.png", "og-default.png"} {
			n := name
			mux.HandleFunc("GET /"+n, func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, filepath.Join(siteDir, n))
			})
		}

		slog.Info("starting HTTP server", "addr", *flagListenAddr, "site_dir", siteDir)
		errCh <- http.ListenAndServe(*flagListenAddr, wwwRedirect(gzipHandler(mux)))
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

func (s *site) handleMeetingDetail(w http.ResponseWriter, r *http.Request) {
	numStr := r.PathValue("number")
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 1 {
		http.NotFound(w, r)
		return
	}

	meeting := meetingByNumber(num)
	if meeting == nil {
		http.NotFound(w, r)
		return
	}

	// Parse DatePath to get ISO date
	var dateISO string
	if t, err := time.Parse("2006/01/02", meeting.DatePath); err == nil {
		dateISO = t.Format("2006-01-02")
	}

	hasDetails := meeting.Start != "" && meeting.Location != ""

	if meeting.IsPast {
		// Load recap HTML for past meetings
		recapPath := filepath.Join(s.recapsDir, fmt.Sprintf("%d.html", num))
		recapBytes, err := os.ReadFile(recapPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		s.render(w, "meeting-detail.html", MeetingDetailData{
			Number:    num,
			Date:      meeting.Date,
			DateISO:   dateISO,
			IsPast:    true,
			RecapHTML: template.HTML(recapBytes),
		})
		return
	}

	// Upcoming meeting — show RSVP form
	s.render(w, "meeting-detail.html", MeetingDetailData{
		Number:     num,
		Date:       meeting.Date,
		DateISO:    dateISO,
		IsPast:     false,
		HasDetails: hasDetails,
		Start:      meeting.Start,
		End:        meeting.End,
		Location:   meeting.Location,
		Hint:       meeting.Hint,
	})
}

func (s *site) handleBriefIndex(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	briefs := s.briefs
	s.mu.RUnlock()
	s.render(w, "brief-index.html", BriefIndexData{Briefs: briefs})
}

// ---------------------------------------------------------------------------
// SEO handlers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// RSVP handlers
// ---------------------------------------------------------------------------

func (s *site) handleRSVPRedirect(w http.ResponseWriter, r *http.Request) {
	next := nextMeeting()
	if next == nil || next.Number == 0 {
		http.Redirect(w, r, "/meetings/", http.StatusFound)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/meetings/%d", next.Number), http.StatusFound)
}

func (s *site) handleRSVPSubmit(w http.ResponseWriter, r *http.Request) {
	numStr := r.PathValue("number")
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 1 {
		http.NotFound(w, r)
		return
	}

	meeting := meetingByNumber(num)
	if meeting == nil || meeting.IsPast {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	newsletterOptIn := r.FormValue("newsletter") == "on"
	responses := RSVPResponses{
		LearnOrDiscuss: strings.TrimSpace(r.FormValue("learn_or_discuss")),
		DemoBuilt:      strings.TrimSpace(r.FormValue("demo_built")),
		DemoTool:       strings.TrimSpace(r.FormValue("demo_tool")),
	}

	// Validate
	if name == "" || email == "" {
		var dateISO string
		if t, err := time.Parse("2006/01/02", meeting.DatePath); err == nil {
			dateISO = t.Format("2006-01-02")
		}
		s.render(w, "meeting-detail.html", MeetingDetailData{
			Number:     num,
			Date:       meeting.Date,
			DateISO:    dateISO,
			IsPast:     false,
			HasDetails: meeting.Start != "" && meeting.Location != "",
			Start:      meeting.Start,
			End:        meeting.End,
			Location:   meeting.Location,
			Hint:       meeting.Hint,
			FormError:  "Name and email are required.",
			FormName:   name,
			FormEmail:  email,
		})
		return
	}

	// Step 1: Check if this is an update, then upsert
	ctx := r.Context()
	responsesJSON, _ := json.Marshal(responses)
	isUpdate := false

	if s.db != nil {
		var count int64
		err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM rsvps WHERE meeting_number = ? AND email = ?",
			num, email).Scan(&count)
		if err == nil && count > 0 {
			isUpdate = true
		}

		_, err = s.db.ExecContext(ctx,
			`INSERT INTO rsvps (meeting_number, name, email, newsletter_opt_in, responses)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(meeting_number, email) DO UPDATE SET
			 name = excluded.name, newsletter_opt_in = excluded.newsletter_opt_in, responses = excluded.responses`,
			num, name, email, newsletterOptIn, string(responsesJSON))
		if err != nil {
			slog.Error("RSVP upsert failed", "error", err)
			http.Error(w, "Something went wrong. Please try again.", 500)
			return
		}
	}

	meetingInfo := MeetingInfo{
		Number:   num,
		Date:     meeting.Date,
		Short:    meeting.Short,
		Start:    meeting.Start,
		End:      meeting.End,
		Location: meeting.Location,
	}

	// Step 2: Send calendar invite (best-effort)
	if s.rsvpCfg.FastmailPassword != "" {
		icsData, err := GenerateICS(meetingInfo)
		if err != nil {
			slog.Error("ICS generation failed", "error", err)
		} else {
			if err := SendCalendarInvite(meetingInfo, email, name, s.rsvpCfg.FastmailPassword, icsData); err != nil {
				slog.Error("calendar email failed", "error", err, "to", email)
			} else {
				slog.Info("calendar invite sent", "to", email, "meeting", num)
			}
		}
	}

	// Step 3: Notify Slack (best-effort)
	if s.bot != nil && s.rsvpCfg.SlackRSVPChannel != "" {
		respMap := map[string]string{
			"learn_or_discuss": responses.LearnOrDiscuss,
			"demo_built":       responses.DemoBuilt,
			"demo_tool":        responses.DemoTool,
		}
		if err := s.bot.PostRSVPNotification(s.rsvpCfg.SlackRSVPChannel, num, meeting.Short, name, email, newsletterOptIn, isUpdate, respMap); err != nil {
			slog.Error("Slack RSVP notification failed", "error", err)
		}
	}

	// Step 4: Buttondown subscribe (best-effort)
	if newsletterOptIn && s.rsvpCfg.ButtondownAPIKey != "" {
		if err := SubscribeToButtondown(ctx, email, s.rsvpCfg.ButtondownAPIKey); err != nil {
			slog.Error("Buttondown subscribe failed", "error", err, "email", email)
		}
	}

	// Render confirmation
	var dateISO string
	if t, err := time.Parse("2006/01/02", meeting.DatePath); err == nil {
		dateISO = t.Format("2006-01-02")
	}
	s.render(w, "meeting-detail.html", MeetingDetailData{
		Number:            num,
		Date:              meeting.Date,
		DateISO:           dateISO,
		IsPast:            false,
		HasDetails:        meeting.Start != "" && meeting.Location != "",
		Start:             meeting.Start,
		End:               meeting.End,
		Location:          meeting.Location,
		Hint:              meeting.Hint,
		RSVPSubmitted:     true,
		IsUpdate:          isUpdate,
		GoogleCalendarURL: GoogleCalendarURL(meetingInfo),
	})
}

func (s *site) handleICSDownload(w http.ResponseWriter, r *http.Request) {
	numStr := r.PathValue("number")
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 1 {
		http.NotFound(w, r)
		return
	}

	meeting := meetingByNumber(num)
	if meeting == nil || meeting.Start == "" {
		http.NotFound(w, r)
		return
	}

	info := MeetingInfo{
		Number:   num,
		Date:     meeting.Date,
		Short:    meeting.Short,
		Start:    meeting.Start,
		End:      meeting.End,
		Location: meeting.Location,
	}

	icsData, err := GenerateICS(info)
	if err != nil {
		slog.Error("ICS generation failed", "error", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=aifriday-meeting-%d.ics", num))
	w.Write(icsData)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func gzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

// wwwRedirect redirects www.aifri.day to aifri.day to avoid duplicate content.
func wwwRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if strings.HasPrefix(host, "www.") {
			target := "https://" + strings.TrimPrefix(host, "www.") + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "User-agent: *\nAllow: /\n\nSitemap: https://aifri.day/sitemap.xml\n")
}

// Sitemap XML types
type sitemapURL struct {
	XMLName    xml.Name `xml:"url"`
	Loc        string   `xml:"loc"`
	LastMod    string   `xml:"lastmod,omitempty"`
	ChangeFreq string   `xml:"changefreq,omitempty"`
	Priority   string   `xml:"priority,omitempty"`
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *site) handleSitemap(w http.ResponseWriter, r *http.Request) {
	urls := []sitemapURL{
		{Loc: "https://aifri.day/", ChangeFreq: "weekly", Priority: "1.0"},
		{Loc: "https://aifri.day/brief/", ChangeFreq: "daily", Priority: "0.8"},
		{Loc: "https://aifri.day/meetings/", ChangeFreq: "monthly", Priority: "0.6"},
	}

	// Add individual brief pages
	s.mu.RLock()
	briefs := s.briefs
	s.mu.RUnlock()
	for _, b := range briefs {
		var lastmod string
		if t, err := time.Parse("2006/01/02", b.DatePath); err == nil {
			lastmod = t.Format("2006-01-02")
		}
		urls = append(urls, sitemapURL{
			Loc:        "https://aifri.day/brief/" + b.DatePath + "/",
			LastMod:    lastmod,
			ChangeFreq: "monthly",
			Priority:   "0.7",
		})
	}

	// Add meeting detail pages (only those with recaps)
	now := time.Now()
	for _, m := range meetingSchedule {
		if m.Number > 0 {
			mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
			if mt.HasRecap {
				var lastmod string
				if t, err := time.Parse("2006/01/02", mt.DatePath); err == nil {
					lastmod = t.Format("2006-01-02")
				}
				urls = append(urls, sitemapURL{
					Loc:        fmt.Sprintf("https://aifri.day/meetings/%d", mt.Number),
					LastMod:    lastmod,
					ChangeFreq: "yearly",
					Priority:   "0.5",
				})
			}
		}
	}

	sitemap := sitemapURLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(sitemap); err != nil {
		slog.Error("encode sitemap", "error", err)
	}
}

// RSS feed types
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	Items       []rssItem `xml:"item"`
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

func (s *site) handleFeed(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	briefs := s.briefs
	s.mu.RUnlock()

	var items []rssItem
	for _, b := range briefs {
		var pubDate string
		if t, err := time.Parse("2006/01/02", b.DatePath); err == nil {
			pubDate = t.Format(time.RFC1123Z)
		}
		link := "https://aifri.day/brief/" + b.DatePath + "/"
		items = append(items, rssItem{
			Title:       b.Date,
			Link:        link,
			Description: b.Preview,
			PubDate:     pubDate,
			GUID:        link,
		})
	}

	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:       "AI Friday \u2014 Daily Brief",
			Link:        "https://aifri.day/brief/",
			Description: "Curated AI news for builders. From the AI Friday meetup in New Orleans.",
			Language:    "en-us",
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		slog.Error("encode feed", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Meetings
// ---------------------------------------------------------------------------

var meetingSchedule = []struct {
	Number   int // 0 means not yet numbered
	Year     int
	Month    time.Month
	Day      int
	Start    string // "9:30 AM"
	End      string // "11:00 AM"
	Location string
	Hint     string
}{
	{1, 2026, time.March, 27, "", "", "", ""},
	{2, 2026, time.April, 17, "9:30 AM", "11:00 AM",
		"RentCheck, 1582 Magazine St, New Orleans, LA 70130",
		"This month we'd especially love to see: image generation, design tools, AI beyond Claude Code & Codex"},
	{0, 2026, time.May, 15, "", "", "", ""},
	{0, 2026, time.June, 26, "", "", "", ""},
	{0, 2026, time.July, 17, "", "", "", ""},
	{0, 2026, time.August, 14, "", "", "", ""},
	{0, 2026, time.September, 18, "", "", "", ""},
	{0, 2026, time.October, 16, "", "", "", ""},
	{0, 2026, time.November, 13, "", "", "", ""},
	{0, 2026, time.December, 18, "", "", "", ""},
}

func buildMeeting(number, year int, month time.Month, day int, now time.Time, start, end, location, hint string) Meeting {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	return Meeting{
		Number:   number,
		Date:     t.Format("Monday, January 2, 2006"),
		Short:    t.Format("Jan 2"),
		DatePath: fmt.Sprintf("%d/%02d/%02d", year, month, day),
		IsPast:   !t.After(now.Truncate(24 * time.Hour)),
		HasRecap: number > 0,
		Start:    start,
		End:      end,
		Location: location,
		Hint:     hint,
	}
}

func nextMeeting() *Meeting {
	now := time.Now()
	for _, m := range meetingSchedule {
		mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
		if !mt.IsPast {
			return &mt
		}
	}
	return nil
}

func splitMeetings() (upcoming, past []Meeting) {
	now := time.Now()
	for _, m := range meetingSchedule {
		mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
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

func meetingByNumber(num int) *Meeting {
	now := time.Now()
	for _, m := range meetingSchedule {
		if m.Number == num {
			mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
			return &mt
		}
	}
	return nil
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
