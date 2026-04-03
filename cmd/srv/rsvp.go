package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
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

// MeetingInfo contains the meeting details needed for integrations.
type MeetingInfo struct {
	Number   int
	Date     string // "Thursday, April 17, 2026"
	Short    string // "Apr 17"
	Start    string // "9:30 AM"
	End      string // "11:00 AM"
	Location string
}

// --- Calendar (.ics) Generation ---

// GenerateICS creates an .ics calendar file for a meeting.
func GenerateICS(meeting MeetingInfo) ([]byte, error) {
	central, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}

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
	// .ics requires CRLF line endings
	w := func(line string) { buf.WriteString(line + "\r\n") }

	w("BEGIN:VCALENDAR")
	w("PRODID:-//AI Friday//aifri.day//EN")
	w("VERSION:2.0")
	w("METHOD:REQUEST")
	w("BEGIN:VEVENT")
	w(fmt.Sprintf("UID:meeting-%d@aifri.day", meeting.Number))
	w(fmt.Sprintf("SUMMARY:AI Friday Meeting #%d", meeting.Number))
	w(fmt.Sprintf("DTSTART:%s", dtStart.Format("20060102T150405Z")))
	w(fmt.Sprintf("DTEND:%s", dtEnd.Format("20060102T150405Z")))
	w(fmt.Sprintf("LOCATION:%s", meeting.Location))
	w(fmt.Sprintf("DESCRIPTION:Monthly AI meetup. Informal — learning and sharing.\\nhttps://aifri.day/meetings/%d", meeting.Number))
	w("ORGANIZER;CN=AI Friday:mailto:andrew@aifri.day")
	w(fmt.Sprintf("URL:https://aifri.day/meetings/%d", meeting.Number))
	w("END:VEVENT")
	w("END:VCALENDAR")

	return buf.Bytes(), nil
}

type timeOfDay struct {
	hour   int
	minute int
}

func parseTimeOfDay(s string) (timeOfDay, error) {
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

	boundary := "aifriday-calendar-boundary"
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: AI Friday <%s>\r\n", from)
	fmt.Fprintf(&buf, "To: %s <%s>\r\n", toName, toEmail)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%s\r\n", boundary)
	buf.WriteString("\r\n")

	// Plain text body
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	fmt.Fprintf(&buf, "You're confirmed for AI Friday Meeting #%d!\r\n\r\n", meeting.Number)
	fmt.Fprintf(&buf, "When: %s, %s – %s\r\n", meeting.Date, meeting.Start, meeting.End)
	fmt.Fprintf(&buf, "Where: %s\r\n\r\n", meeting.Location)
	buf.WriteString("All meetings are informal and are about learning and sharing.\r\n\r\n")
	fmt.Fprintf(&buf, "https://aifri.day/meetings/%d\r\n", meeting.Number)
	buf.WriteString("\r\n")

	// .ics attachment
	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/calendar; charset=utf-8; method=REQUEST\r\n")
	buf.WriteString("Content-Disposition: attachment; filename=invite.ics\r\n\r\n")
	buf.Write(icsData)
	buf.WriteString("\r\n")

	fmt.Fprintf(&buf, "--%s--\r\n", boundary)

	// Send via Fastmail SMTP (TLS on port 465)
	auth := smtp.PlainAuth("", from, fastmailPassword, "smtp.fastmail.com")

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
