# RSVP & Survey System — Design Spec

## Overview

Add RSVP functionality to AI Friday meeting pages. Attendees fill out a form with their name, email, optional survey questions, and an optional newsletter signup. On submit they receive a calendar invite via email and a notification is posted to Slack.

## URL Routing

- `GET /rsvp` — redirects to `/meetings/{next_meeting_number}`. If no upcoming meeting exists, redirects to `/meetings/`.
- `GET /meetings/{number}` — two modes:
  - **Upcoming meeting (not past):** meeting info + RSVP form
  - **Past meeting:** recap HTML (existing behavior, unchanged)
- `POST /meetings/{number}/rsvp` — handles form submission, returns confirmation view

## Meeting Schedule Changes

Extend `meetingSchedule` struct in `cmd/srv/main.go` to include per-meeting details:

```go
var meetingSchedule = []struct {
    Number   int
    Year     int
    Month    time.Month
    Day      int
    Start    string // "9:30 AM"
    End      string // "11:00 AM"
    Location string // "RentCheck, 1582 Magazine St, New Orleans, LA 70130"
    Hint     string // rotating survey hint, optional
}{
    {1, 2026, time.March, 27, "", "", "", ""},
    {2, 2026, time.April, 17, "9:30 AM", "11:00 AM",
        "RentCheck, 1582 Magazine St, New Orleans, LA 70130",
        "This month we'd especially love to see: image generation, design tools, AI beyond Claude Code & Codex"},
    // ... remaining meetings get numbered as details are confirmed
}
```

Meeting #2 (April 17) gets numbered. Future meetings remain `0` until details are set.

## Data Model

New migration (`003-rsvps.sql` or next available number):

```sql
CREATE TABLE rsvps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    meeting_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    newsletter_opt_in BOOLEAN NOT NULL DEFAULT 0,
    responses TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(meeting_number, email)
);

INSERT INTO migrations (migration_number) VALUES (3);
```

The `responses` column stores a JSON object:

```json
{
    "learn_or_discuss": "Free text answer or empty string",
    "demo_built": "Free text answer or empty string",
    "demo_tool": "Free text answer or empty string"
}
```

Duplicate emails (same meeting) upsert: update name, responses, newsletter_opt_in, and re-send the calendar invite.

## Form Layout

Meeting detail page for upcoming meetings displays:

**Header section:**
- Meeting number and full date
- Time (e.g. "9:30 – 11:00 AM")
- Location with address
- "All meetings are informal and are about learning and sharing."

**RSVP section (required):**
- Name (text input, required)
- Email (email input, required) — helper text: "This is where we'll send the invite. No spam."
- Checkbox: "Subscribe to the AI Friday newsletter"

**Survey section (optional):**
- Section header: "Optional — help us plan the agenda"
- Q1: "Is there anything you'd like to learn about, get help with, or discuss?" (textarea)
- Q2: "Would you like to demo something you've built?" (textarea) — helper: "Doesn't need to be polished or finished. 10-15 minutes."
- Q3: "Would you like to demo a tool or setup you've been using?" (textarea) — helper: "AI app, skill, plugin — even a few hours of experience puts you ahead of most people in the room."
- Rotating hint below Q3 if the meeting has one set (e.g. "This month we'd especially love to see: image generation, design tools, AI beyond Claude Code & Codex")

**Submit button:** "RSVP"

## Submission Flow

`POST /meetings/{number}/rsvp` handles the form. Four steps, in order:

1. **Validate & store** — parse form, upsert into `rsvps` table. This must succeed; if it fails, show an error.
2. **Send calendar invite** — email .ics to the submitted email via Fastmail SMTP. Best-effort: log errors, don't fail the RSVP.
3. **Notify Slack** — post to #rsvp (`C0AR6MSBUAV`) with name, email, newsletter opt-in status, and any non-empty survey answers. Best-effort.
4. **Buttondown subscribe** — if newsletter checkbox is checked, POST to `https://api.buttondown.com/v1/subscribers`. Best-effort.

## Confirmation View

On successful submission, the form is replaced with a confirmation message on the same page (no redirect). Meeting details remain visible above.

**First-time RSVP:**
- "You're in! Check your email for a calendar invite."

**Duplicate email (upsert):**
- "Updated! We've sent a fresh calendar invite."

**Backup calendar links** below the confirmation message:
- "Add to Google Calendar" (pre-filled Google Calendar URL)
- "Download .ics" (direct download link)

## Calendar Invite (.ics)

Generated per-meeting from the `meetingSchedule` data:

```ics
BEGIN:VCALENDAR
PRODID:-//AI Friday//aifri.day//EN
VERSION:2.0
METHOD:REQUEST
BEGIN:VEVENT
UID:meeting-{number}@aifri.day
SUMMARY:AI Friday Meeting #{number}
DTSTART:{start_time_utc}
DTEND:{end_time_utc}
LOCATION:{location}
DESCRIPTION:Monthly AI meetup. Informal — learning and sharing.\nhttps://aifri.day/meetings/{number}
ORGANIZER;CN=AI Friday:mailto:andrew@aifri.day
URL:https://aifri.day/meetings/{number}
END:VEVENT
END:VCALENDAR
```

Times are converted from Central Time to UTC for the .ics.

## Email

- **From:** `AI Friday <andrew@aifri.day>`
- **Subject:** `AI Friday Meeting #{number} — {month} {day}, {start_time}`
- **Body:** Short plain text confirmation with meeting details and link to meeting page
- **Attachment:** .ics file as `text/calendar` content type (triggers calendar accept button in most email clients)
- **SMTP:** `smtp.fastmail.com:587`, TLS, using app-specific password

## Slack Notification

Posted to #rsvp (`C0AR6MSBUAV`):

```
📋 New RSVP — Meeting #2 (Apr 17)

Name: Jane Smith
Email: jane@example.com
Newsletter: ✅

💬 Learn/discuss: "Want to learn about image generation workflows"
🛠️ Demo a tool: "I've been using Cursor for a few weeks"
```

Empty survey fields are omitted from the message. If no survey fields are filled in, the survey section is omitted entirely.

For upserts, the message says "Updated RSVP" instead of "New RSVP".

## Buttondown Integration

When newsletter checkbox is checked, POST to Buttondown API:

```
POST https://api.buttondown.com/v1/subscribers
Authorization: Token {BUTTONDOWN_API_KEY}
Content-Type: application/json

{"email": "user@example.com"}
```

If the email is already subscribed, Buttondown returns a 409 — that's fine, we ignore it.

## Environment Variables

Three new entries in `.env`:

- `FASTMAIL_APP_PASSWORD` — app-specific password for andrew@aifri.day SMTP
- `BUTTONDOWN_API_KEY` — Buttondown API key for subscriber management
- `SLACK_RSVP_CHANNEL` — channel ID for RSVP notifications (C0AR6MSBUAV)

## Template Changes

`srv/templates/meeting-detail.html` — extend with conditional RSVP form:
- If `!IsPast && HasDetails` (meeting has time/location set): show RSVP form
- If `IsPast && HasRecap`: show recap (existing)
- If `!IsPast && !HasDetails`: show "Details coming soon"

New template data struct extends `MeetingDetailData` with:
- Time, Location, Hint fields from meeting schedule
- `RSVPSubmitted bool` — whether to show form or confirmation
- `IsUpdate bool` — whether this was a duplicate email (for confirmation wording)
- `FormData` — for re-populating form on validation error

## Google Calendar Link

Pre-filled URL format (no API needed):

```
https://calendar.google.com/calendar/render?action=TEMPLATE
&text=AI+Friday+Meeting+%232
&dates=20260417T143000Z/20260417T160000Z
&location=RentCheck,+1582+Magazine+St,+New+Orleans,+LA+70130
&details=Monthly+AI+meetup.+https://aifri.day/meetings/2
```

## What This Does NOT Include

- Admin page for viewing RSVPs (query the DB directly for now)
- RSVP count displayed on the page
- Cancellation/un-RSVP flow
- Automated reminders before the meeting
- Past RSVP data shown on recap pages

These can be added later if useful.
