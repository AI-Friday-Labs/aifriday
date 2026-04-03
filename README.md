# AI Friday NOLA

The web app and Slack bot for [AI Friday](https://aifri.day), a weekly AI meetup in New Orleans.

## What it does

- **Daily Brief** -- An AI-curated daily digest of AI news, posted to Slack at 5 AM CT and published to the website. Pulls from RSS feeds, Hacker News, newsletters, and links shared by community members.
- **Slack Bot** -- Monitors AI Friday Slack channels, captures shared links (with a scoring boost for the daily brief), and responds to mentions and DMs.
- **Website** -- Meeting schedule with RSVP, daily brief archive, and meeting recaps at [aifri.day](https://aifri.day).
- **RSVP System** -- Per-meeting RSVP with calendar invite emails and optional newsletter signup via Buttondown.

## Architecture

```
cmd/srv/          Main server (HTTP + Slack bot, port 8000)
cmd/brief/        Daily brief generator (feeds -> Claude -> HTML -> Slack)
cmd/slacklinks/   CLI to dump captured Slack links
slack/            Slack bot (Socket Mode, link capture, reactions)
feeds/            RSS feeds, Hacker News API, newsletter email ingestion
db/               SQLite database, migrations, sqlc-generated queries
srv/templates/    Go HTML templates (homepage, meetings, briefs)
srv/recaps/       Meeting recap HTML fragments
site/             Static assets and generated brief HTML
gen_site.py       Regenerates all brief HTML from JSON
RULEBOOK.md       Editorial guidelines for daily brief content
```

## Setup

### Prerequisites

- Go 1.26+
- SQLite
- A `.env` file with the required secrets (see below)

### Environment Variables

| Variable | Purpose |
|---|---|
| `SLACK_BOT_TOKEN` | Slack bot OAuth token (`xoxb-...`) |
| `SLACK_APP_TOKEN` | Slack app-level token (`xapp-...`, for Socket Mode) |
| `FASTMAIL_APP_PASSWORD` | SMTP password for sending calendar invite emails |
| `BUTTONDOWN_API_KEY` | Buttondown API key for newsletter subscriber management |
| `SLACK_RSVP_CHANNEL` | Slack channel ID for RSVP notifications |

### Build & Run

```bash
make build              # Builds ai-friday-bot and gen-brief binaries
sudo systemctl restart srv  # Restart the service (changes go live immediately)
```

### Other make targets

```bash
make test         # Run all Go tests
make brief        # Generate today's brief (dry run, no Slack post)
make brief-post   # Generate and post today's brief to Slack
make restart      # Build + restart service
make clean        # Remove binaries
```

## Daily Brief Pipeline

1. `cmd/brief` fetches articles from RSS feeds, Hacker News (30+ points, AI-filtered), newsletters, and Slack-captured links
2. Articles are scored (novelty, usefulness, coolness, credibility, +2 community boost)
3. Top items are sent to Claude 3.5 Haiku to generate a structured brief
4. Output is saved as JSON in `briefs/` and rendered to HTML in `site/brief/YYYY/MM/DD/`
5. A Slack message is posted to #daily-brief
6. Runs daily at 5:00 AM CT via systemd timer

Editorial guidelines are in [RULEBOOK.md](RULEBOOK.md).

## Deployment

Runs as a systemd service (`srv`) on an [exe.dev](https://exe.dev) VM, proxied to [aifri.day](https://aifri.day). The database is SQLite (`aifriday.db`), stored locally.

## License

Private project for the AI Friday NOLA community.
