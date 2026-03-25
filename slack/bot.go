package slackbot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"srv.exe.dev/db/dbgen"
)

const DailyBriefChannel = "C0ANS0UT7GE"

// skipChannels are channels we don't capture links from (by ID).
var skipChannels = map[string]bool{
	DailyBriefChannel: true, // #daily-brief — our own posts
	"C0ANF7W0B3R":    true, // #ai-alerts — automated noise
}

// linkRe matches Slack-formatted URLs: <https://example.com> or <https://example.com|label>
var linkRe = regexp.MustCompile(`<(https?://[^>|]+)(?:\|[^>]*)?>`) //nolint:gocritic

// skipDomains are domains we don't capture as community links
var skipDomains = map[string]bool{
	"aifri.day":           true,
	"slack.com":           true,
	"app.slack.com":       true,
	"hooks.slack.com":     true,
	"files.slack.com":     true,
	"ai-friday.slack.com": true,
}

type Bot struct {
	API    *slack.Client
	Socket *socketmode.Client
	BotUID string
	DB     *sql.DB // may be nil

	userMu    sync.Mutex
	userCache map[string]string
}

// New creates a new Bot. The database parameter is optional (pass nil to
// disable link capture).
func New(database *sql.DB) (*Bot, error) {
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")

	if botToken == "" || appToken == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN and SLACK_APP_TOKEN must be set")
	}

	api := slack.New(botToken,
		slack.OptionAppLevelToken(appToken),
	)

	socket := socketmode.New(api,
		socketmode.OptionLog(slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug)),
	)

	// Get our own bot user ID
	auth, err := api.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("auth test failed: %w", err)
	}

	return &Bot{
		API:       api,
		Socket:    socket,
		BotUID:    auth.UserID,
		DB:        database,
		userCache: make(map[string]string),
	}, nil
}

func (b *Bot) Run() error {
	go b.handleEvents()
	slog.Info("starting socket mode", "bot_uid", b.BotUID)
	return b.Socket.Run()
}

func (b *Bot) handleEvents() {
	for evt := range b.Socket.Events {
		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				continue
			}
			b.Socket.Ack(*evt.Request)
			b.handleEventAPI(eventsAPI)

		case socketmode.EventTypeSlashCommand:
			cmd, ok := evt.Data.(slack.SlashCommand)
			if !ok {
				continue
			}
			b.Socket.Ack(*evt.Request)
			b.handleSlashCommand(cmd)

		case socketmode.EventTypeConnecting:
			slog.Info("connecting to Slack...")
		case socketmode.EventTypeConnected:
			slog.Info("connected to Slack")
		case socketmode.EventTypeConnectionError:
			slog.Error("connection error", "data", evt.Data)
		case socketmode.EventTypeHello:
			slog.Info("hello from Slack")
		default:
			slog.Debug("unhandled event", "type", evt.Type)
		}
	}
}

func (b *Bot) handleEventAPI(event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		inner := event.InnerEvent
		switch ev := inner.Data.(type) {
		case *slackevents.AppMentionEvent:
			b.handleMention(ev)
		case *slackevents.ReactionAddedEvent:
			b.handleReaction(ev)
		case *slackevents.MessageEvent:
			// Ignore bot's own messages
			if ev.User == b.BotUID {
				return
			}
			if ev.ChannelType == "im" {
				b.handleDM(ev)
			} else {
				// Channel message — capture links
				b.captureLinks(ev)
			}
		}
	}
}

func (b *Bot) handleMention(ev *slackevents.AppMentionEvent) {
	text := strings.TrimSpace(ev.Text)
	slog.Info("mentioned", "user", ev.User, "text", text, "channel", ev.Channel)

	_, _, err := b.API.PostMessage(ev.Channel,
		slack.MsgOptionText("Hey! 👋 I'm the AI Friday bot. I post daily AI briefs here. Still getting set up — more commands coming soon!", false),
		slack.MsgOptionTS(ev.TimeStamp),
	)
	if err != nil {
		slog.Error("failed to reply to mention", "error", err)
	}
}

func (b *Bot) handleDM(ev *slackevents.MessageEvent) {
	slog.Info("DM received", "user", ev.User, "text", ev.Text)

	_, _, err := b.API.PostMessage(ev.Channel,
		slack.MsgOptionText("Hey! I'm the AI Friday bot. DM support coming soon. For now, catch the daily brief in #daily-brief!", false),
	)
	if err != nil {
		slog.Error("failed to reply to DM", "error", err)
	}
}

func (b *Bot) handleReaction(ev *slackevents.ReactionAddedEvent) {
	slog.Info("reaction", "emoji", ev.Reaction, "user", ev.User, "item_channel", ev.Item.Channel)
	// TODO: track reactions for feedback loop
}

func (b *Bot) handleSlashCommand(cmd slack.SlashCommand) {
	slog.Info("slash command", "command", cmd.Command, "text", cmd.Text, "user", cmd.UserID)
	// TODO: implement /brief, /subscribe, etc.
}

// captureLinks extracts URLs from channel messages and saves them to the database.
func (b *Bot) captureLinks(ev *slackevents.MessageEvent) {
	// Skip bot messages
	if ev.BotID != "" || ev.SubType == "bot_message" {
		return
	}

	// No DB — nothing to do
	if b.DB == nil {
		return
	}

	// Skip ignored channels (by ID or name)
	if skipChannels[ev.Channel] {
		return
	}

	links := extractLinks(ev.Text)
	if len(links) == 0 {
		return
	}

	// Resolve channel name (best-effort)
	channelName := ev.Channel
	info, err := b.API.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: ev.Channel})
	if err == nil {
		channelName = info.Name
	}

	userName := b.resolveUser(ev.User)

	q := dbgen.New(b.DB)
	ctx := context.Background()

	for _, link := range links {
		err := q.InsertSlackLink(ctx, dbgen.InsertSlackLinkParams{
			Url:         link,
			ChannelID:   ev.Channel,
			ChannelName: channelName,
			UserID:      ev.User,
			UserName:    userName,
			MessageTs:   ev.TimeStamp,
			MessageText: truncate(ev.Text, 500),
		})
		if err != nil {
			slog.Warn("insert slack link", "url", link, "error", err)
		} else {
			slog.Info("captured link", "channel", channelName, "user", userName, "url", link)
		}
	}
}

// resolveUser gets a display name for a Slack user ID, with caching.
func (b *Bot) resolveUser(userID string) string {
	b.userMu.Lock()
	defer b.userMu.Unlock()

	if name, ok := b.userCache[userID]; ok {
		return name
	}

	user, err := b.API.GetUserInfo(userID)
	if err != nil {
		b.userCache[userID] = userID
		return userID
	}

	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}
	if name == "" {
		name = user.Name
	}
	b.userCache[userID] = name
	return name
}

func extractLinks(text string) []string {
	matches := linkRe.FindAllStringSubmatch(text, -1)
	var links []string
	seen := map[string]bool{}
	for _, m := range matches {
		link := m[1]
		if seen[link] {
			continue
		}
		seen[link] = true
		if shouldSkipLink(link) {
			continue
		}
		links = append(links, link)
	}
	return links
}

func shouldSkipLink(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if skipDomains[host] {
		return true
	}
	if strings.Contains(host, "slack.com") {
		return true
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// BriefURL returns the full URL for a brief on the given date.
// date should be in YYYY/MM/DD format.
func BriefURL(date string) string {
	return "https://aifri.day/brief/" + date + "/"
}

// PostDailyBrief sends a formatted brief to #daily-brief.
// date is the brief date in YYYY/MM/DD format (e.g. "2026/03/25").
// briefText is the Slack-formatted summary. A link to the full brief
// on the website is appended automatically.
func (b *Bot) PostDailyBrief(date, briefText string) error {
	fullText := briefText + "\n\n" + "📖 Full brief: " + BriefURL(date)
	_, _, err := b.API.PostMessage(DailyBriefChannel,
		slack.MsgOptionText(fullText, false),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
	)
	return err
}
