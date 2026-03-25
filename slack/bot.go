package slackbot

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const DailyBriefChannel = "C0ANS0UT7GE"

type Bot struct {
	API    *slack.Client
	Socket *socketmode.Client
	BotUID string
}

func New() (*Bot, error) {
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
		API:    api,
		Socket: socket,
		BotUID: auth.UserID,
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

// PostDailyBrief sends a formatted brief to #daily-brief
func (b *Bot) PostDailyBrief(briefText string) error {
	_, _, err := b.API.PostMessage(DailyBriefChannel,
		slack.MsgOptionText(briefText, false),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
	)
	return err
}
