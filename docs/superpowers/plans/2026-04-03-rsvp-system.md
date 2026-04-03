# RSVP & Survey System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add RSVP functionality to AI Friday meeting pages — form, calendar invite via email, Slack notification, and Buttondown newsletter subscription.

**Architecture:** Extend the existing Go server (`cmd/srv/main.go`) with new routes, a DB migration for the `rsvps` table with flexible JSON responses, and three best-effort integrations (Fastmail SMTP, Slack API, Buttondown API). The meeting schedule struct gets extended with per-meeting time/location/hint fields. The meeting detail template gains conditional rendering: RSVP form for upcoming meetings, recap for past ones.

**Tech Stack:** Go stdlib (`net/smtp`, `encoding/json`, `net/http`), SQLite, Go HTML templates, existing Slack bot library (`github.com/slack-go/slack`)

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `db/migrations/004-rsvps.sql` | RSVP table migration |
| Create | `db/queries/rsvps.sql` | sqlc query definitions for RSVP upsert/lookup |
| Modify | `db/dbgen/` (regenerated) | sqlc-generated code for RSVP queries |
| Create | `srv/rsvp.go` | RSVP handler, calendar (.ics) generation, SMTP email sending, Buttondown API call |
| Modify | `cmd/srv/main.go:32-67` | Extend Meeting struct and meetingSchedule with time/location/hint; extend MeetingDetailData; add new routes; update handleMeetingDetail and buildMeeting |
| Modify | `srv/templates/meeting-detail.html` | Conditional RSVP form / confirmation / recap rendering |
| Modify | `site/static/style.css` | RSVP form styles |
| Modify | `slack/bot.go` | Add PostRSVPNotification method |

---

### Task 1: Database Migration

**Files:**
- Create: `db/migrations/004-rsvps.sql`

- [ ] **Step 1: Create migration file**

```sql
-- 004-rsvps.sql
CREATE TABLE IF NOT EXISTS rsvps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    meeting_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    newsletter_opt_in BOOLEAN NOT NULL DEFAULT 0,
    responses TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(meeting_number, email)
);

CREATE INDEX IF NOT EXISTS idx_rsvps_meeting ON rsvps(meeting_number);

INSERT INTO migrations (migration_number) VALUES (4);
```

- [ ] **Step 2: Verify migration runs**

Run: `cd /home/exedev/ai-friday && go run ./cmd/srv &` (start server briefly, check logs for "db: applied migration" with number 4, then stop it)

- [ ] **Step 3: Commit**

```bash
git add db/migrations/004-rsvps.sql
git commit -m "Add RSVP table migration (004)"
```

---

### Task 2: sqlc Queries for RSVPs

**Files:**
- Create: `db/queries/rsvps.sql`
- Regenerate: `db/dbgen/` (via sqlc generate)

- [ ] **Step 1: Create query file**

Create `db/queries/rsvps.sql`:

```sql
-- name: UpsertRSVP :exec
INSERT INTO rsvps (meeting_number, name, email, newsletter_opt_in, responses)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(meeting_number, email) DO UPDATE SET
    name = excluded.name,
    newsletter_opt_in = excluded.newsletter_opt_in,
    responses = excluded.responses;

-- name: RSVPExists :one
SELECT COUNT(*) FROM rsvps WHERE meeting_number = ? AND email = ?;

-- name: RSVPsByMeeting :many
SELECT id, meeting_number, name, email, newsletter_opt_in, responses, created_at
FROM rsvps
WHERE meeting_number = ?
ORDER BY created_at ASC;
```

- [ ] **Step 2: Regenerate sqlc**

Run: `cd /home/exedev/ai-friday/db && go generate`

Expected: new file `db/dbgen/rsvps.sql.go` with `UpsertRSVP`, `RSVPExists`, and `RSVPsByMeeting` functions. The `models.go` file gains an `Rsvp` struct.

If `sqlc` is not installed, install it: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`

If `go generate` fails because sqlc uses `go tool`, run directly: `cd /home/exedev/ai-friday/db && sqlc generate` (or check the go.mod for the tool directive).

- [ ] **Step 3: Verify generated code compiles**

Run: `cd /home/exedev/ai-friday && go build ./...`
Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add db/queries/rsvps.sql db/dbgen/
git commit -m "Add sqlc queries for RSVP upsert and lookup"
```

---

### Task 3: Extend Meeting Schedule Struct

**Files:**
- Modify: `cmd/srv/main.go:32-40` (Meeting struct)
- Modify: `cmd/srv/main.go:459-475` (meetingSchedule)
- Modify: `cmd/srv/main.go:477-487` (buildMeeting)
- Modify: `cmd/srv/main.go:60-67` (MeetingDetailData)
- Modify: `cmd/srv/main.go:219-253` (handleMeetingDetail)

- [ ] **Step 1: Extend the Meeting struct**

In `cmd/srv/main.go`, replace the Meeting struct (lines 33-40):

```go
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
```

- [ ] **Step 2: Extend MeetingDetailData**

Replace MeetingDetailData (lines 61-66):

```go
type MeetingDetailData struct {
	Number        int
	Date          string
	DateISO       string
	IsPast        bool
	HasDetails    bool          // true if Start/Location are set
	Start         string
	End           string
	Location      string
	Hint          string
	RecapHTML     template.HTML
	RSVPSubmitted bool
	IsUpdate      bool
	FormError     string
	FormName      string        // re-populate on error
	FormEmail     string        // re-populate on error
}
```

- [ ] **Step 3: Extend meetingSchedule**

Replace meetingSchedule (lines 459-475):

```go
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
```

- [ ] **Step 4: Update buildMeeting to carry new fields**

Replace buildMeeting (lines 477-487):

```go
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
```

- [ ] **Step 5: Update all callers of buildMeeting**

Every call to `buildMeeting` now needs the extra args. Search for `buildMeeting(` and update each:

In `nextMeeting()` (line 492):
```go
mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
```

In `splitMeetings()` (line 503):
```go
mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
```

In `meetingByNumber()` (line 520):
```go
mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
```

In `handleSitemap()` (line 362):
```go
mt := buildMeeting(m.Number, m.Year, m.Month, m.Day, now, m.Start, m.End, m.Location, m.Hint)
```

- [ ] **Step 6: Update handleMeetingDetail**

Replace handleMeetingDetail (lines 219-253) to handle both upcoming and past meetings:

```go
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
```

- [ ] **Step 7: Verify it compiles**

Run: `cd /home/exedev/ai-friday && go build ./cmd/srv`
Expected: builds with no errors.

- [ ] **Step 8: Commit**

```bash
git add cmd/srv/main.go
git commit -m "Extend meeting schedule with time, location, hint; number meeting #2"
```

---

### Task 4: RSVP Handler & Integrations

**Files:**
- Create: `srv/rsvp.go`

This file contains: the POST handler, .ics generation, SMTP email sending, Buttondown API call, and Google Calendar URL builder.

- [ ] **Step 1: Create `srv/rsvp.go`**

Create `/home/exedev/ai-friday/srv/rsvp.go`:

```go
package srv

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// RSVPResponses is the JSON blob stored in the responses column.
type RSVPResponses struct {
	LearnOrDiscuss string `json:"learn_or_discuss"`
	DemoBuilt      string `json:"demo_built"`
	DemoTool       string `json:"demo_tool"`
}

// RSVPConfig holds env-based configuration for RSVP integrations.
type RSVPConfig struct {
	FastmailPassword string
	ButtondownAPIKey string
	SlackRSVPChannel string
}

// RSVPResult is passed back to the handler after processing.
type RSVPResult struct {
	Success  bool
	IsUpdate bool
	Error    string
}

// ProcessRSVP handles the full RSVP submission flow:
// 1. Upsert to DB
// 2. Send calendar invite email (best-effort)
// 3. Notify Slack (best-effort)
// 4. Subscribe to Buttondown (best-effort)
func ProcessRSVP(ctx context.Context, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (interface{ RowsAffected() (int64, error) }, error)
}, cfg RSVPConfig, slackAPI SlackPoster, meeting MeetingInfo, name, email string, newsletterOptIn bool, responses RSVPResponses) RSVPResult {
	// This is a coordination function — the actual DB call happens in the handler
	// since it uses sqlc-generated code. This file provides the integration helpers.
	return RSVPResult{Success: true}
}

// MeetingInfo contains the meeting details needed for integrations.
type MeetingInfo struct {
	Number   int
	Date     string // "Thursday, April 17, 2026"
	Short    string // "Apr 17"
	Start    string // "9:30 AM"
	End      string // "11:00 AM"
	Location string
}

// SlackPoster is the interface for posting Slack messages.
type SlackPoster interface {
	PostRSVPNotification(channel string, meeting MeetingInfo, name, email string, newsletterOptIn bool, isUpdate bool, responses RSVPResponses) error
}

// --- Calendar (.ics) Generation ---

// GenerateICS creates an .ics calendar file for a meeting.
func GenerateICS(meeting MeetingInfo) ([]byte, error) {
	// Parse start and end times in Central Time
	central, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}

	// Parse the date from meeting.Date (e.g. "Thursday, April 17, 2026")
	meetDate, err := time.Parse("Monday, January 2, 2006", meeting.Date)
	if err != nil {
		return nil, fmt.Errorf("parse meeting date: %w", err)
	}

	startTime, err := parseTimeOfDay(meeting.Start)
	if err != nil {
		return nil, fmt.Errorf("parse start time: %w", err)
	}
	endTime, err := parseTimeOfDay(meeting.End)
	if err != nil {
		return nil, fmt.Errorf("parse end time: %w", err)
	}

	dtStart := time.Date(meetDate.Year(), meetDate.Month(), meetDate.Day(),
		startTime.hour, startTime.minute, 0, 0, central).UTC()
	dtEnd := time.Date(meetDate.Year(), meetDate.Month(), meetDate.Day(),
		endTime.hour, endTime.minute, 0, 0, central).UTC()

	var buf bytes.Buffer
	fmt.Fprintln(&buf, "BEGIN:VCALENDAR")
	fmt.Fprintln(&buf, "PRODID:-//AI Friday//aifri.day//EN")
	fmt.Fprintln(&buf, "VERSION:2.0")
	fmt.Fprintln(&buf, "METHOD:REQUEST")
	fmt.Fprintln(&buf, "BEGIN:VEVENT")
	fmt.Fprintf(&buf, "UID:meeting-%d@aifri.day\r\n", meeting.Number)
	fmt.Fprintf(&buf, "SUMMARY:AI Friday Meeting #%d\r\n", meeting.Number)
	fmt.Fprintf(&buf, "DTSTART:%s\r\n", dtStart.Format("20060102T150405Z"))
	fmt.Fprintf(&buf, "DTEND:%s\r\n", dtEnd.Format("20060102T150405Z"))
	fmt.Fprintf(&buf, "LOCATION:%s\r\n", meeting.Location)
	fmt.Fprintf(&buf, "DESCRIPTION:Monthly AI meetup. Informal — learning and sharing.\\nhttps://aifri.day/meetings/%d\r\n", meeting.Number)
	fmt.Fprintln(&buf, "ORGANIZER;CN=AI Friday:mailto:andrew@aifri.day")
	fmt.Fprintf(&buf, "URL:https://aifri.day/meetings/%d\r\n", meeting.Number)
	fmt.Fprintln(&buf, "END:VEVENT")
	fmt.Fprintln(&buf, "END:VCALENDAR")

	return buf.Bytes(), nil
}

type timeOfDay struct {
	hour   int
	minute int
}

func parseTimeOfDay(s string) (timeOfDay, error) {
	// Parse "9:30 AM" or "11:00 AM" style strings
	s = strings.TrimSpace(s)
	t, err := time.Parse("3:04 PM", s)
	if err != nil {
		return timeOfDay{}, fmt.Errorf("invalid time %q: %w", s, err)
	}
	return timeOfDay{hour: t.Hour(), minute: t.Minute()}, nil
}

// GoogleCalendarURL returns a pre-filled Google Calendar event URL.
func GoogleCalendarURL(meeting MeetingInfo) string {
	central, _ := time.LoadLocation("America/Chicago")
	meetDate, _ := time.Parse("Monday, January 2, 2006", meeting.Date)
	startTOD, _ := parseTimeOfDay(meeting.Start)
	endTOD, _ := parseTimeOfDay(meeting.End)

	dtStart := time.Date(meetDate.Year(), meetDate.Month(), meetDate.Day(),
		startTOD.hour, startTOD.minute, 0, 0, central).UTC()
	dtEnd := time.Date(meetDate.Year(), meetDate.Month(), meetDate.Day(),
		endTOD.hour, endTOD.minute, 0, 0, central).UTC()

	dates := dtStart.Format("20060102T150405Z") + "/" + dtEnd.Format("20060102T150405Z")

	v := url.Values{}
	v.Set("action", "TEMPLATE")
	v.Set("text", fmt.Sprintf("AI Friday Meeting #%d", meeting.Number))
	v.Set("dates", dates)
	v.Set("location", meeting.Location)
	v.Set("details", fmt.Sprintf("Monthly AI meetup. https://aifri.day/meetings/%d", meeting.Number))

	return "https://calendar.google.com/calendar/render?" + v.Encode()
}

// --- Email (SMTP via Fastmail) ---

// SendCalendarInvite sends an .ics calendar invite via Fastmail SMTP.
func SendCalendarInvite(meeting MeetingInfo, toEmail, toName, fastmailPassword string, icsData []byte) error {
	from := "andrew@aifri.day"
	subject := fmt.Sprintf("AI Friday Meeting #%d — %s, %s", meeting.Number, meeting.Short, meeting.Start)

	// Build MIME email with .ics attachment
	boundary := "aifriday-calendar-boundary"
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: AI Friday <%s>\r\n", from)
	fmt.Fprintf(&buf, "To: %s <%s>\r\n", toName, toEmail)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%s\r\n", boundary)
	fmt.Fprintf(&buf, "\r\n")

	// Plain text body
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
	fmt.Fprintf(&buf, "You're confirmed for AI Friday Meeting #%d!\r\n\r\n", meeting.Number)
	fmt.Fprintf(&buf, "When: %s, %s – %s\r\n", meeting.Date, meeting.Start, meeting.End)
	fmt.Fprintf(&buf, "Where: %s\r\n\r\n", meeting.Location)
	fmt.Fprintf(&buf, "All meetings are informal and are about learning and sharing.\r\n\r\n")
	fmt.Fprintf(&buf, "https://aifri.day/meetings/%d\r\n", meeting.Number)
	fmt.Fprintf(&buf, "\r\n")

	// .ics attachment
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	fmt.Fprintf(&buf, "Content-Type: text/calendar; charset=utf-8; method=REQUEST\r\n")
	fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=invite.ics\r\n\r\n")
	buf.Write(icsData)
	fmt.Fprintf(&buf, "\r\n")

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)

	// Send via Fastmail SMTP
	auth := smtp.PlainAuth("", from, fastmailPassword, "smtp.fastmail.com")

	// Use TLS connection to smtp.fastmail.com:587
	tlsConfig := &tls.Config{ServerName: "smtp.fastmail.com"}
	conn, err := tls.Dial("tcp", "smtp.fastmail.com:465", tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS dial: %w", err)
	}

	client, err := smtp.NewClient(conn, "smtp.fastmail.com")
	if err != nil {
		return fmt.Errorf("SMTP client: %w", err)
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL: %w", err)
	}
	if err := client.Rcpt(toEmail); err != nil {
		return fmt.Errorf("SMTP RCPT: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := wc.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("SMTP write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("SMTP close: %w", err)
	}

	return client.Quit()
}

// --- Buttondown Newsletter ---

// SubscribeToButtondown adds an email to the Buttondown newsletter.
func SubscribeToButtondown(ctx context.Context, email, apiKey string) error {
	body, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.buttondown.com/v1/subscribers", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("buttondown request: %w", err)
	}
	defer resp.Body.Close()

	// 201 = created, 409 = already subscribed — both fine
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		return fmt.Errorf("buttondown returned %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/exedev/ai-friday && go build ./...`
Expected: builds. (The `srv` package is not imported by `cmd/srv` via its package path — it's in the `srv/` directory but `cmd/srv/main.go` is `package main`. The `rsvp.go` file uses `package srv` which matches `srv/server.go`. This is fine — it compiles as part of the `srv` package but won't conflict since `cmd/srv` doesn't import it. We'll import the needed functions directly in the next task.)

Note: If there's a package conflict (cmd/srv main.go is in package main, not package srv), we may need to put rsvp.go in a different location. Check the actual package structure. If `cmd/srv/main.go` is `package main`, then `srv/rsvp.go` being `package srv` is a separate package. We'll import it from cmd/srv via `srv.exe.dev/srv` or inline the code. Based on the existing code, `cmd/srv/main.go` does NOT import the `srv` package for the main site — it has its own handlers inline. The `srv/` package (`srv/server.go`) appears to be an older/unused server. So we should put `rsvp.go` alongside `cmd/srv/main.go` as `package main`, OR create a new package.

**Decision:** Put RSVP logic in a new file `cmd/srv/rsvp.go` as `package main` to match `cmd/srv/main.go`. This keeps all the serving logic together.

Revise: Create `/home/exedev/ai-friday/cmd/srv/rsvp.go` with `package main` at the top instead of `package srv`. Everything else stays the same.

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/exedev/ai-friday && go build ./cmd/srv`
Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/srv/rsvp.go
git commit -m "Add RSVP handler: .ics generation, SMTP email, Buttondown API"
```

---

### Task 5: Slack RSVP Notification

**Files:**
- Modify: `slack/bot.go`

- [ ] **Step 1: Add PostRSVPNotification to slack/bot.go**

Add the following method at the end of `/home/exedev/ai-friday/slack/bot.go` (after the existing `PostDailyBrief` method at line 317):

```go
// PostRSVPNotification sends an RSVP notification to the given channel.
func (b *Bot) PostRSVPNotification(channel string, meetingNumber int, meetingShort string, name, email string, newsletterOptIn bool, isUpdate bool, responses map[string]string) error {
	label := "New RSVP"
	emoji := "📋"
	if isUpdate {
		label = "Updated RSVP"
		emoji = "🔄"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s — Meeting #%d (%s)\n\n", emoji, label, meetingNumber, meetingShort)
	fmt.Fprintf(&sb, "Name: %s\n", name)
	fmt.Fprintf(&sb, "Email: %s\n", email)
	if newsletterOptIn {
		sb.WriteString("Newsletter: ✅\n")
	}

	// Survey responses
	type field struct {
		key   string
		emoji string
		label string
	}
	fields := []field{
		{"learn_or_discuss", "💬", "Learn/discuss"},
		{"demo_built", "🎪", "Demo something built"},
		{"demo_tool", "🛠️", "Demo a tool"},
	}
	hasAny := false
	for _, f := range fields {
		if v, ok := responses[f.key]; ok && strings.TrimSpace(v) != "" {
			if !hasAny {
				sb.WriteString("\n")
				hasAny = true
			}
			fmt.Fprintf(&sb, "%s %s: \"%s\"\n", f.emoji, f.label, v)
		}
	}

	_, _, err := b.API.PostMessage(channel,
		slack.MsgOptionText(sb.String(), false),
		slack.MsgOptionDisableLinkUnfurl(),
	)
	return err
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/exedev/ai-friday && go build ./...`
Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add slack/bot.go
git commit -m "Add Slack RSVP notification method"
```

---

### Task 6: Wire Routes and POST Handler

**Files:**
- Modify: `cmd/srv/main.go` (routes + POST handler)

- [ ] **Step 1: Add RSVP route and /rsvp redirect**

In `cmd/srv/main.go`, inside the `go func()` that sets up the HTTP mux (around line 142-167), add these routes after the existing meeting routes:

```go
		mux.HandleFunc("GET /rsvp", s.handleRSVPRedirect)
		mux.HandleFunc("POST /meetings/{number}/rsvp", s.handleRSVPSubmit)
		mux.HandleFunc("GET /meetings/{number}/invite.ics", s.handleICSDownload)
```

- [ ] **Step 2: Add the handleRSVPRedirect method**

Add to `cmd/srv/main.go`:

```go
func (s *site) handleRSVPRedirect(w http.ResponseWriter, r *http.Request) {
	next := nextMeeting()
	if next == nil || next.Number == 0 {
		http.Redirect(w, r, "/meetings/", http.StatusFound)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/meetings/%d", next.Number), http.StatusFound)
}
```

- [ ] **Step 3: Add the handleRSVPSubmit method**

Add to `cmd/srv/main.go`. This needs access to the database and Slack bot, so we need to add those to the `site` struct.

First, extend the `site` struct (around line 72):

```go
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
```

Then update the `run()` function to pass db and bot to the site struct. Around line 101-105, change:

```go
	s := &site{
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
```

Note: the `database` and `bot` variables are declared after the current `s := &site{...}` block. Move the site creation to after both are initialized (after line 135). The database is opened at line 122-129 and bot at line 132-135. Move the `s := &site{}` block to after line 135 (after bot creation).

Now add the handler:

```go
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

	// Parse form
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

	// Step 1: Check if this is an update
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

		// Upsert
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
			"demo_built":      responses.DemoBuilt,
			"demo_tool":       responses.DemoTool,
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
		Number:        num,
		Date:          meeting.Date,
		DateISO:       dateISO,
		IsPast:        false,
		HasDetails:    meeting.Start != "" && meeting.Location != "",
		Start:         meeting.Start,
		End:           meeting.End,
		Location:      meeting.Location,
		Hint:          meeting.Hint,
		RSVPSubmitted: true,
		IsUpdate:      isUpdate,
	})
}
```

- [ ] **Step 4: Add the .ics download handler**

```go
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
```

- [ ] **Step 5: Add missing imports to main.go**

Ensure `"encoding/json"` and `"database/sql"` are in the import block of `cmd/srv/main.go`. They should already be present or add them.

- [ ] **Step 6: Verify it compiles**

Run: `cd /home/exedev/ai-friday && go build ./cmd/srv`
Expected: builds with no errors.

- [ ] **Step 7: Commit**

```bash
git add cmd/srv/main.go cmd/srv/rsvp.go
git commit -m "Wire RSVP routes: form handler, .ics download, /rsvp redirect"
```

---

### Task 7: Meeting Detail Template

**Files:**
- Modify: `srv/templates/meeting-detail.html`

- [ ] **Step 1: Rewrite the meeting detail template**

Replace the entire contents of `/home/exedev/ai-friday/srv/templates/meeting-detail.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI Friday #{{.Number}} &mdash; {{if .IsPast}}Recap{{else}}RSVP{{end}}</title>
  <meta name="description" content="{{if .IsPast}}Recap and notes from AI Friday meeting #{{.Number}} in New Orleans.{{else}}RSVP for AI Friday meeting #{{.Number}} in New Orleans — {{.Date}}.{{end}}">
  <link rel="canonical" href="https://aifri.day/meetings/{{.Number}}">
  <meta property="og:title" content="AI Friday #{{.Number}} — {{.Date}}">
  <meta property="og:description" content="{{if .IsPast}}Recap and notes from AI Friday meeting #{{.Number}}.{{else}}RSVP for AI Friday meeting #{{.Number}} — {{.Date}}.{{end}}">
  <meta property="og:url" content="https://aifri.day/meetings/{{.Number}}">
  <meta property="og:type" content="article">
  <meta property="og:image" content="https://aifri.day/og-default.png">
  <meta property="og:site_name" content="AI Friday">
  <meta name="twitter:card" content="summary">
  <meta name="twitter:title" content="AI Friday #{{.Number}} — {{.Date}}">
  <meta name="twitter:description" content="{{if .IsPast}}Recap and notes from AI Friday meeting #{{.Number}}.{{else}}RSVP for AI Friday meeting #{{.Number}} — {{.Date}}.{{end}}">
  <meta name="twitter:image" content="https://aifri.day/og-default.png">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Crimson+Text:wght@400;600;700&family=Instrument+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/static/style.css?v=6">
  <link rel="icon" href="/favicon.ico" sizes="48x48">
  <link rel="icon" href="/icon-192.png" type="image/png" sizes="192x192">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <script defer src="https://umami-production-d337.up.railway.app/script.js" data-website-id="3a8001f6-6312-473a-b2bd-38dae609847c"></script>
  <script src="https://analytics.ahrefs.com/analytics.js" data-key="23HVW+pAWhX3mLQMX/nA5A" async></script>
</head>
<body>
  <div class="container">

    <nav class="nav">
      <a href="/" class="nav-logo">
        <span class="logo-ai">AI</span><span class="logo-friday">Friday</span>
        <span class="logo-location">New Orleans</span>
      </a>
      <div class="nav-links">
        <a href="/meetings/">&larr; Meetings</a>
      </div>
    </nav>

    <section class="intro-section">
      <h1 class="page-title">AI Friday #{{.Number}}</h1>
      <p class="intro-text text-muted">{{.Date}}</p>
    </section>

    {{if .IsPast}}
      <!-- Past meeting: show recap -->
      <section class="meeting-recap">
        {{.RecapHTML}}
      </section>

    {{else if .RSVPSubmitted}}
      <!-- Confirmation -->
      {{if .HasDetails}}
      <section class="rsvp-meeting-info">
        <p class="meeting-time">{{.Start}} – {{.End}}</p>
        <p class="meeting-location">{{.Location}}</p>
      </section>
      {{end}}

      <section class="rsvp-confirmation">
        {{if .IsUpdate}}
        <h2>Updated!</h2>
        <p>We&rsquo;ve sent a fresh calendar invite to your email.</p>
        {{else}}
        <h2>You&rsquo;re in!</h2>
        <p>Check your email for a calendar invite.</p>
        {{end}}

        <div class="rsvp-calendar-links">
          <a href="/meetings/{{.Number}}/invite.ics" class="btn">Download .ics</a>
          <a href="{{.GoogleCalendarURL}}" target="_blank" rel="noopener" class="btn">Add to Google Calendar</a>
        </div>
      </section>

    {{else if .HasDetails}}
      <!-- Upcoming meeting with details: show RSVP form -->
      <section class="rsvp-meeting-info">
        <p class="meeting-time">{{.Start}} – {{.End}}</p>
        <p class="meeting-location">{{.Location}}</p>
        <p class="meeting-vibe">All meetings are informal and are about learning and sharing.</p>
      </section>

      <section class="rsvp-form-section">
        <h2>RSVP</h2>

        {{if .FormError}}
        <p class="rsvp-error">{{.FormError}}</p>
        {{end}}

        <form action="/meetings/{{.Number}}/rsvp" method="POST" class="rsvp-form">
          <div class="rsvp-field">
            <label for="rsvp-name">Name</label>
            <input type="text" id="rsvp-name" name="name" required value="{{.FormName}}">
          </div>

          <div class="rsvp-field">
            <label for="rsvp-email">Email</label>
            <input type="email" id="rsvp-email" name="email" required value="{{.FormEmail}}" placeholder="you@example.com">
            <span class="rsvp-hint">This is where we&rsquo;ll send the invite. No spam.</span>
          </div>

          <div class="rsvp-checkbox">
            <label>
              <input type="checkbox" name="newsletter">
              Subscribe to the AI Friday newsletter
            </label>
          </div>

          <hr class="rsvp-divider">

          <h3>Optional &mdash; help us plan the agenda</h3>

          <div class="rsvp-field">
            <label for="rsvp-learn">Is there anything you&rsquo;d like to learn about, get help with, or discuss?</label>
            <textarea id="rsvp-learn" name="learn_or_discuss" rows="3"></textarea>
          </div>

          <div class="rsvp-field">
            <label for="rsvp-demo-built">Would you like to demo something you&rsquo;ve built?</label>
            <span class="rsvp-hint">Doesn&rsquo;t need to be polished or finished. 10&ndash;15 minutes.</span>
            <textarea id="rsvp-demo-built" name="demo_built" rows="3"></textarea>
          </div>

          <div class="rsvp-field">
            <label for="rsvp-demo-tool">Would you like to demo a tool or setup you&rsquo;ve been using?</label>
            <span class="rsvp-hint">AI app, skill, plugin &mdash; even a few hours of experience puts you ahead of most people in the room.</span>
            <textarea id="rsvp-demo-tool" name="demo_tool" rows="3"></textarea>
          </div>

          {{if .Hint}}
          <p class="rsvp-rotating-hint">{{.Hint}}</p>
          {{end}}

          <button type="submit" class="btn btn-primary rsvp-submit">RSVP</button>
        </form>
      </section>

    {{else}}
      <!-- Upcoming meeting without details yet -->
      <section class="rsvp-meeting-info">
        <p class="meeting-vibe">Details coming soon. Check back closer to the date.</p>
      </section>
    {{end}}

  </div>

  <script type="application/ld+json">
  {
    "@context": "https://schema.org",
    "@type": "Event",
    "name": "AI Friday #{{.Number}}",
    "startDate": "{{.DateISO}}",
    "location": {
      "@type": "Place",
      "name": "{{.Location}}",
      "address": {
        "@type": "PostalAddress",
        "addressLocality": "New Orleans",
        "addressRegion": "LA"
      }
    },
    "organizer": {
      "@type": "Organization",
      "name": "AI Friday",
      "url": "https://aifri.day"
    }
  }
  </script>
</body>
</html>
```

- [ ] **Step 2: Add GoogleCalendarURL to MeetingDetailData**

The template references `{{.GoogleCalendarURL}}`. We need to either: (a) add it as a field to `MeetingDetailData`, or (b) use a template function. Simplest: add it as a field.

In `cmd/srv/main.go`, add to the `MeetingDetailData` struct:

```go
	GoogleCalendarURL string
```

Then in `handleRSVPSubmit`, when rendering the confirmation, compute and pass it:

```go
		GoogleCalendarURL: GoogleCalendarURL(meetingInfo),
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/exedev/ai-friday && go build ./cmd/srv`
Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add srv/templates/meeting-detail.html cmd/srv/main.go
git commit -m "Add RSVP form and confirmation to meeting detail template"
```

---

### Task 8: RSVP Form CSS

**Files:**
- Modify: `site/static/style.css`

- [ ] **Step 1: Add RSVP form styles**

Append the following to the end of `/home/exedev/ai-friday/site/static/style.css`:

```css
/* --- RSVP Form ----------------------------------------------------------- */
.rsvp-meeting-info {
  margin-bottom: var(--space-xl);
}

.meeting-time {
  font-family: var(--font-display);
  font-size: var(--text-h3);
  font-weight: 600;
  color: var(--text);
  margin-bottom: var(--space-xs);
}

.meeting-location {
  font-size: var(--text-body);
  color: var(--text-secondary);
  margin-bottom: var(--space-md);
}

.meeting-vibe {
  font-size: var(--text-body);
  color: var(--muted);
  font-style: italic;
}

.rsvp-form-section {
  margin-bottom: var(--space-2xl);
}

.rsvp-form-section h2 {
  font-family: var(--font-display);
  font-size: var(--text-h2);
  font-weight: 700;
  margin-bottom: var(--space-lg);
}

.rsvp-form-section h3 {
  font-family: var(--font-display);
  font-size: var(--text-h3);
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: var(--space-lg);
}

.rsvp-form {
  max-width: 560px;
}

.rsvp-field {
  margin-bottom: var(--space-lg);
}

.rsvp-field label {
  display: block;
  font-size: var(--text-body);
  font-weight: 600;
  color: var(--text);
  margin-bottom: var(--space-sm);
}

.rsvp-field input[type="text"],
.rsvp-field input[type="email"],
.rsvp-field textarea {
  width: 100%;
  padding: 10px 14px;
  font-family: var(--font-body);
  font-size: var(--text-body);
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  outline: none;
  transition: border-color 0.15s ease;
  box-sizing: border-box;
}

.rsvp-field input:focus,
.rsvp-field textarea:focus {
  border-color: var(--accent);
}

.rsvp-field input::placeholder {
  color: var(--muted);
}

.rsvp-field textarea {
  resize: vertical;
  min-height: 80px;
}

.rsvp-hint {
  display: block;
  font-size: var(--text-small);
  color: var(--muted);
  margin-top: var(--space-xs);
  margin-bottom: var(--space-sm);
}

.rsvp-checkbox {
  margin-bottom: var(--space-lg);
}

.rsvp-checkbox label {
  display: flex;
  align-items: center;
  gap: var(--space-sm);
  font-size: var(--text-body);
  color: var(--text);
  cursor: pointer;
}

.rsvp-checkbox input[type="checkbox"] {
  width: 18px;
  height: 18px;
  accent-color: var(--accent);
}

.rsvp-divider {
  border: none;
  border-top: 1px solid var(--line);
  margin: var(--space-xl) 0;
}

.rsvp-rotating-hint {
  font-size: var(--text-body);
  color: var(--accent);
  font-style: italic;
  margin-bottom: var(--space-lg);
  padding: var(--space-md);
  background: var(--surface-soft);
  border-radius: 8px;
}

.rsvp-submit {
  margin-top: var(--space-md);
  padding: 14px 40px;
  font-size: var(--text-body-lg);
}

.rsvp-error {
  color: #c0392b;
  font-weight: 600;
  margin-bottom: var(--space-lg);
  padding: var(--space-md);
  background: #fdf2f0;
  border: 1px solid #e8c4be;
  border-radius: 8px;
}

/* --- RSVP Confirmation --------------------------------------------------- */
.rsvp-confirmation {
  margin-bottom: var(--space-2xl);
}

.rsvp-confirmation h2 {
  font-family: var(--font-display);
  font-size: var(--text-h2);
  font-weight: 700;
  color: var(--secondary);
  margin-bottom: var(--space-sm);
}

.rsvp-confirmation p {
  font-size: var(--text-body-lg);
  color: var(--text-secondary);
  margin-bottom: var(--space-lg);
}

.rsvp-calendar-links {
  display: flex;
  gap: var(--space-md);
  flex-wrap: wrap;
}

@media (max-width: 640px) {
  .rsvp-calendar-links {
    flex-direction: column;
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add site/static/style.css
git commit -m "Add RSVP form and confirmation CSS styles"
```

---

### Task 9: Build, Deploy & Smoke Test

**Files:** None new — build and restart.

- [ ] **Step 1: Build**

Run: `cd /home/exedev/ai-friday && make build`
Expected: both `ai-friday-bot` and `gen-brief` binaries compile successfully.

- [ ] **Step 2: Restart the service**

Run: `sudo systemctl restart srv`

- [ ] **Step 3: Check service is healthy**

Run: `systemctl status srv --no-pager`
Expected: active (running), no errors in recent logs. Should see "db: applied migration" for migration 004.

- [ ] **Step 4: Test /rsvp redirect**

Run: `curl -sI http://localhost:8000/rsvp | head -5`
Expected: `302 Found` with `Location: /meetings/2`

- [ ] **Step 5: Test meeting #2 page loads**

Run: `curl -s http://localhost:8000/meetings/2 | grep -o 'RSVP'`
Expected: matches "RSVP" (the form heading and submit button)

- [ ] **Step 6: Test .ics download**

Run: `curl -s http://localhost:8000/meetings/2/invite.ics | head -5`
Expected: starts with `BEGIN:VCALENDAR`

- [ ] **Step 7: Test form submission**

Run:
```bash
curl -s -X POST http://localhost:8000/meetings/2/rsvp \
  -d "name=Test+User&email=test@example.com&learn_or_discuss=Testing+the+form" \
  | grep -o "You're in\|Updated"
```
Expected: `You're in` (or similar confirmation text)

Check Slack #rsvp channel for the notification. Check that test@example.com receives a calendar invite.

- [ ] **Step 8: Test past meeting still works**

Run: `curl -s http://localhost:8000/meetings/1 | grep -o 'recap'`
Expected: matches (the recap section still renders)

- [ ] **Step 9: Commit any final fixes**

If any fixes were needed during testing, commit them:

```bash
git add -A
git commit -m "Fix issues found during RSVP smoke testing"
```
