# AI Friday Bot

Slack bot for the AI Friday New Orleans meetup group.
Posts a daily brief of curated AI news, tools, and updates to #daily-brief.

## Structure
- `cmd/srv/main.go` — entry point
- `srv/` — HTTP server + Slack bot logic
- `db/` — SQLite database, migrations
- `feeds/` — RSS/content ingestion
- `brief/` — Daily brief generation + curation rules
- `slack/` — Slack API client

## Key files
- `RULEBOOK.md` — Editorial rules for content selection
- `cmd/slacklinks/main.go` — Backfill tool to pull Slack links into DB

## Slack Link Capture
The bot captures URLs shared in all Slack channels it belongs to.
Links are stored in the `slack_links` table and fed into the daily brief
pipeline with a +2 scoring boost. Community links appear in a "From the
Community" section on the website.
