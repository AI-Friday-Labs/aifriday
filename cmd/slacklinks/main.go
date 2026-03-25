// cmd/slacklinks backfills Slack channel links into the database.
// Usage: go run ./cmd/slacklinks [-channel C0xxxxx] [-hours 24] [-list-channels]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"srv.exe.dev/db"
	"srv.exe.dev/db/dbgen"
)

var (
	flagChannel      = flag.String("channel", "", "channel ID to backfill (default: all bot channels)")
	flagHours        = flag.Int("hours", 24, "how many hours back to look")
	flagListChannels = flag.Bool("list-channels", false, "just list channels the bot is in")
	flagDryRun       = flag.Bool("dry-run", false, "print links without saving")
)

// linkRe matches URLs in message text. Slack wraps links in <url> or <url|label>.
var linkRe = regexp.MustCompile(`<(https?://[^>|]+)(?:\|[^>]*)?>`) 

// skipDomains are domains we don't want to capture as interesting links
var skipDomains = map[string]bool{
	"aifri.day":           true,
	"ai-friday.slack.com": true,
	"app.slack.com":       true,
	"slack.com":           true,
	"hooks.slack.com":     true,
	"files.slack.com":     true,
}

// skipChannelIDs are channels we don't capture links from.
var skipChannelIDs = map[string]bool{
	"C0ANS0UT7GE": true, // #daily-brief — our own posts
	"C0ANF7W0B3R": true, // #ai-alerts — automated noise
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	godotenv.Load()

	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN must be set")
	}

	api := slack.New(botToken)

	// List channels mode
	if *flagListChannels {
		return listChannels(api)
	}

	// Open database
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	dbPath := filepath.Join(projectRoot, "aifriday.db")

	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	if err := db.RunMigrations(database); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	q := dbgen.New(database)
	ctx := context.Background()

	// Determine which channels to scan
	channelIDs := []string{}
	if *flagChannel != "" {
		channelIDs = append(channelIDs, *flagChannel)
	} else {
		// Get all channels the bot is a member of
		channels, err := getBotChannels(api)
		if err != nil {
			return fmt.Errorf("list channels: %w", err)
		}
		for _, ch := range channels {
			channelIDs = append(channelIDs, ch.ID)
		}
		slog.Info("scanning all bot channels", "count", len(channelIDs))
	}

	// Build a channel name lookup
	channelNames := map[string]string{}
	userNames := map[string]string{}

	since := time.Now().Add(-time.Duration(*flagHours) * time.Hour)
	totalLinks := 0

	for _, chID := range channelIDs {
		// Skip ignored channels
		if skipChannelIDs[chID] {
			slog.Info("skipping channel", "id", chID)
			continue
		}

		// Get channel name if we don't have it
		if _, ok := channelNames[chID]; !ok {
			info, err := api.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: chID})
			if err != nil {
				slog.Warn("get channel info", "channel", chID, "error", err)
				channelNames[chID] = chID
			} else {
				channelNames[chID] = info.Name
			}
		}

		chName := channelNames[chID]
		slog.Info("scanning channel", "channel", chName, "id", chID)

		// Fetch conversation history
		params := &slack.GetConversationHistoryParameters{
			ChannelID: chID,
			Oldest:    fmt.Sprintf("%d", since.Unix()),
			Limit:     200,
			Inclusive: true,
		}

		for {
			history, err := api.GetConversationHistory(params)
			if err != nil {
				slog.Warn("get history", "channel", chName, "error", err)
				break
			}

			for _, msg := range history.Messages {
				// Skip bot messages
				if msg.SubType == "bot_message" || msg.BotID != "" {
					continue
				}

				links := extractLinks(msg.Text)
				if len(links) == 0 {
					continue
				}

				// Resolve user name
				userName := resolveUser(api, msg.User, userNames)

				for _, link := range links {
					totalLinks++
					if *flagDryRun {
						fmt.Printf("  [%s] @%s: %s\n", chName, userName, link)
						continue
					}

					err := q.InsertSlackLink(ctx, dbgen.InsertSlackLinkParams{
						Url:         link,
						ChannelID:   chID,
						ChannelName: chName,
						UserID:      msg.User,
						UserName:    userName,
						MessageTs:   msg.Timestamp,
						MessageText: truncate(msg.Text, 500),
					})
					if err != nil {
						slog.Warn("insert link", "url", link, "error", err)
					} else {
						slog.Info("saved link", "channel", chName, "user", userName, "url", link)
					}
				}
			}

			if !history.HasMore {
				break
			}
			params.Cursor = history.ResponseMetaData.NextCursor
		}
	}

	slog.Info("backfill complete", "total_links", totalLinks)
	return nil
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

		// Skip internal/uninteresting domains
		if shouldSkip(link) {
			continue
		}
		links = append(links, link)
	}
	return links
}

func shouldSkip(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if skipDomains[host] {
		return true
	}
	// Skip Slack internal URLs
	if strings.Contains(host, "slack.com") {
		return true
	}
	return false
}

func resolveUser(api *slack.Client, userID string, cache map[string]string) string {
	if name, ok := cache[userID]; ok {
		return name
	}
	user, err := api.GetUserInfo(userID)
	if err != nil {
		cache[userID] = userID
		return userID
	}
	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}
	if name == "" {
		name = user.Name
	}
	cache[userID] = name
	return name
}

func listChannels(api *slack.Client) error {
	channels, err := getBotChannels(api)
	if err != nil {
		return err
	}
	fmt.Printf("Bot is a member of %d channels:\n", len(channels))
	for _, ch := range channels {
		fmt.Printf("  %-15s #%s\n", ch.ID, ch.Name)
	}
	return nil
}

func getBotChannels(api *slack.Client) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""
	for {
		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel", "private_channel"},
			ExcludeArchived: true,
			Limit:           200,
			Cursor:          cursor,
		}
		channels, nextCursor, err := api.GetConversations(params)
		if err != nil {
			return nil, err
		}
		allChannels = append(allChannels, channels...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return allChannels, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
