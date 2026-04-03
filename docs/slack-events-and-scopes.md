# Slack Bot Events & Scopes Analysis

## Current Bot Setup

### Environment Variables (`.env`)
- `SLACK_BOT_TOKEN` — Bot User OAuth Token (starts with `xoxb-`)
- `SLACK_APP_TOKEN` — App-Level Token (starts with `xapp-`, required for Socket Mode)

No other config variables exist in `.env`.

### Current Event Subscriptions (in code)
The bot handles these Events API inner events via Socket Mode:
1. **`AppMentionEvent`** — when someone @mentions the bot
2. **`ReactionAddedEvent`** — when a reaction emoji is added (any channel the bot can see)
3. **`MessageEvent`** — all message events, but **filtered in code** to only process `ChannelType == "im"` (DMs)

---

## Question 1: Does MessageEvent via Socket Mode receive messages from ALL channels?

**YES** — Socket Mode delivers `message` events from all channel types the bot is a member of, not just DMs. The `MessageEvent` struct has a `ChannelType` field with these possible values:

| `ChannelType` value | Meaning |
|---|---|
| `"channel"` | Public channel |
| `"group"` | Private channel |
| `"im"` | Direct message (1:1) |
| `"mim"` | Multi-party direct message |

The current code at `bot.go:104` filters to only `"im"`, meaning **channel messages are received but silently dropped**.

**However**, this depends on two things being configured correctly:
1. The **Slack app manifest** must subscribe to the `message.channels` and/or `message.groups` event types (configured in the Slack App dashboard under "Event Subscriptions")
2. The bot token must have the appropriate **OAuth scopes** (see below)

If the app is only subscribed to `message.im` events in the dashboard, then only DM messages arrive via Socket Mode — the library doesn't control this.

---

## Question 2: Required Slack API Scopes

To receive `message` events from different channel types, you need these **Bot Token Scopes** (OAuth & Permissions page in Slack dashboard):

| Scope | Purpose |
|---|---|
| `channels:history` | Read messages from **public channels** the bot is in |
| `groups:history` | Read messages from **private channels** the bot is in |
| `im:history` | Read **direct messages** with the bot |
| `mpim:history` | Read **multi-party DMs** the bot is in |

And you need these **Event Subscriptions** (bot events):

| Event | Scope Required |
|---|---|
| `message.channels` | `channels:history` |
| `message.groups` | `groups:history` |
| `message.im` | `im:history` |
| `message.mpim` | `mpim:history` |

Additionally, for link detection specifically, consider:
- `links:read` — if you want to use the `link_shared` event instead of parsing messages yourself

### Scopes you likely already have (based on current functionality):
- `app_mentions:read` — for `AppMentionEvent`
- `reactions:read` — for `ReactionAddedEvent`
- `im:history` — for DM `MessageEvent`
- `chat:write` — for `PostMessage` calls
- `connections:write` — required for Socket Mode

### Scopes you need to ADD for channel message capture:
- **`channels:history`** — public channel messages
- **`groups:history`** — private channel messages (if needed)

And subscribe to these events in the dashboard:
- **`message.channels`**
- **`message.groups`** (if needed)

---

## Question 3: Checking OAuth Scopes from Code

The `AuthTest()` method used in `bot.go` does **NOT** return scope information. The `AuthTestResponse` struct only contains: `URL`, `Team`, `User`, `TeamID`, `UserID`, `EnterpriseID`, `BotID`.

To check scopes programmatically, you have two options:

### Option A: Check the HTTP response headers from any API call
Slack returns scopes in response headers:
- `X-OAuth-Scopes` — all scopes the token has
- `X-Accepted-OAuth-Scopes` — scopes required for that specific endpoint

The slack-go library doesn't expose these headers directly, but you could make a raw HTTP call.

### Option B: Quick CLI check with curl
```bash
curl -s -H "Authorization: Bearer $SLACK_BOT_TOKEN" \
  https://slack.com/api/auth.test -D - -o /dev/null 2>&1 | grep -i x-oauth-scopes
```

This will show all scopes the token currently has.

---

## Code Change Required

To capture links from all channels, the minimal change in `bot.go` is:

```go
case *slackevents.MessageEvent:
    // Ignore bot's own messages
    if ev.User == b.BotUID {
        return
    }
    if ev.ChannelType == "im" {
        b.handleDM(ev)
    }
    // Process links from ALL channel types
    b.extractAndStoreLinks(ev)
```

Or to handle channel messages explicitly:
```go
case *slackevents.MessageEvent:
    if ev.User == b.BotUID {
        return
    }
    switch ev.ChannelType {
    case "im":
        b.handleDM(ev)
    case "channel", "group", "mim":
        b.handleChannelMessage(ev)
    }
```
