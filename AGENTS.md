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
