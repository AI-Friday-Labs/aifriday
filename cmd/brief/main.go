// cmd/brief generates the daily brief: fetches feeds, calls Claude, writes
// HTML, and posts to Slack.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"database/sql"

	"github.com/joho/godotenv"
	"srv.exe.dev/db"
	"srv.exe.dev/feeds"
	slackbot "srv.exe.dev/slack"
)

const llmGateway = "http://169.254.169.254/gateway/llm/anthropic/v1/messages"

var (
	flagDate    = flag.String("date", "", "date to generate brief for (YYYY-MM-DD), defaults to today")
	flagDryRun  = flag.Bool("dry-run", false, "skip Slack posting")
	flagNoSlack = flag.Bool("no-slack", false, "skip Slack posting")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found", "error", err)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	// Determine date — always use Central time for the brief date
	centralTZ, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return fmt.Errorf("load Central timezone: %w", err)
	}
	now := time.Now().In(centralTZ)
	briefDate := now
	if *flagDate != "" {
		var parseErr error
		briefDate, parseErr = time.ParseInLocation("2006-01-02", *flagDate, centralTZ)
		if parseErr != nil {
			return fmt.Errorf("invalid date %q: %w", *flagDate, parseErr)
		}
	}

	datePath := briefDate.Format("2006/01/02")
	dateHuman := briefDate.Format("Monday, January 2, 2006")
	slog.Info("generating brief", "date", dateHuman, "path", datePath)

	// Check if brief already exists
	htmlPath := filepath.Join(projectRoot, "site", "brief", datePath, "index.html")
	if _, err := os.Stat(htmlPath); err == nil {
		slog.Info("brief already exists", "path", htmlPath)
		return fmt.Errorf("brief for %s already exists at %s", datePath, htmlPath)
	}

	// Open database for Slack community links
	dbPath := filepath.Join(projectRoot, "aifriday.db")
	database, err := db.Open(dbPath)
	if err != nil {
		slog.Warn("could not open db for Slack links", "error", err)
	} else {
		defer database.Close()
		if err := db.RunMigrations(database); err != nil {
			slog.Warn("db migrations", "error", err)
		}
	}

	// Step 0: Process any new newsletter emails
	maildir := filepath.Join(os.Getenv("HOME"), "Maildir")
	if _, err := os.Stat(filepath.Join(maildir, "new")); err == nil && database != nil {
		slog.Info("processing newsletter emails...")
		if err := feeds.ProcessMaildir(database, maildir); err != nil {
			slog.Warn("newsletter processing failed", "error", err)
		}
	}

	// Step 1: Fetch feeds
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	slog.Info("fetching feeds...")
	articles, err := fetchAllSources(ctx, database)
	if err != nil {
		return fmt.Errorf("fetch feeds: %w", err)
	}
	slog.Info("fetched articles", "count", len(articles))

	if len(articles) == 0 {
		return fmt.Errorf("no articles found, cannot generate brief")
	}

	// Step 2: Find previous brief for prev/next linking
	prevDate := findPrevBrief(projectRoot, datePath)

	// Step 3: Build the prompt and call Claude
	slog.Info("calling Claude to generate brief...")
	briefJSON, err := generateBrief(ctx, articles, dateHuman, datePath, prevDate, projectRoot)
	if err != nil {
		return fmt.Errorf("generate brief: %w", err)
	}

	// Step 4: Save JSON
	jsonDir := filepath.Join(projectRoot, "briefs")
	os.MkdirAll(jsonDir, 0755)
	jsonPath := filepath.Join(jsonDir, fmt.Sprintf("brief_%s.json", briefDate.Format("2006-01-02")))
	if err := os.WriteFile(jsonPath, briefJSON, 0644); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	slog.Info("saved brief JSON", "path", jsonPath)

	// Step 5: Generate HTML via gen_site.py
	slog.Info("generating HTML...")
	cmd := exec.Command("python3", filepath.Join(projectRoot, "gen_site.py"), jsonDir)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gen_site.py: %w", err)
	}

	// Step 5b: Update prev brief's next_date link
	if prevDate != "" {
		updatePrevBriefNextLink(projectRoot, prevDate, datePath)
	}

	slog.Info("generated HTML", "path", htmlPath)

	// Step 6: Post to Slack
	if !*flagDryRun && !*flagNoSlack {
		slog.Info("posting to Slack...")
		if err := postToSlack(briefJSON, datePath); err != nil {
			slog.Error("slack post failed", "error", err)
			// Don't fail the whole run for Slack errors
		} else {
			slog.Info("posted to Slack")
		}
	} else {
		slog.Info("skipping Slack post (dry-run or --no-slack)")
	}

	slog.Info("brief generation complete", "date", dateHuman)
	return nil
}

// ---------------------------------------------------------------------------
// Feed fetching
// ---------------------------------------------------------------------------

func fetchAllSources(ctx context.Context, database *sql.DB) ([]feeds.Article, error) {
	var allArticles []feeds.Article

	// RSS feeds (last 36 hours to catch overnight stuff)
	rssArticles, err := feeds.FetchAll(ctx, feeds.DefaultFeeds(), 36*time.Hour)
	if err != nil {
		slog.Warn("RSS fetch error", "error", err)
	} else {
		allArticles = append(allArticles, rssArticles...)
	}

	// Hacker News (30+ points, check 300 stories, last 36 hours)
	hnArticles, err := feeds.FetchHNTopStories(ctx, 30, 300, 36*time.Hour)
	if err != nil {
		slog.Warn("HN fetch error", "error", err)
	} else {
		allArticles = append(allArticles, hnArticles...)
	}

	// Slack community links (last 36 hours)
	if database != nil {
		slackArticles, err := feeds.SlackLinkArticles(database, 36*time.Hour)
		if err != nil {
			slog.Warn("Slack links fetch error", "error", err)
		} else {
			allArticles = append(allArticles, slackArticles...)
			slog.Info("fetched Slack community links", "count", len(slackArticles))
		}
	}

	// Newsletter articles (last 36 hours)
	if database != nil {
		newsletterArticles, err := feeds.NewsletterArticles(database, 36*time.Hour)
		if err != nil {
			slog.Warn("Newsletter articles fetch error", "error", err)
		} else {
			allArticles = append(allArticles, newsletterArticles...)
			slog.Info("fetched newsletter articles", "count", len(newsletterArticles))
		}
	}

	// Sort by points (HN) then recency
	sort.Slice(allArticles, func(i, j int) bool {
		if allArticles[i].Points != allArticles[j].Points {
			return allArticles[i].Points > allArticles[j].Points
		}
		return allArticles[i].Published.After(allArticles[j].Published)
	})

	return allArticles, nil
}

// ---------------------------------------------------------------------------
// Brief generation via Claude
// ---------------------------------------------------------------------------

func generateBrief(ctx context.Context, articles []feeds.Article, dateHuman, datePath, prevDate, projectRoot string) ([]byte, error) {
	// Read the rulebook
	rulebookPath := filepath.Join(projectRoot, "RULEBOOK.md")
	rulebook, err := os.ReadFile(rulebookPath)
	if err != nil {
		slog.Warn("could not read RULEBOOK.md", "error", err)
		rulebook = []byte("Write a concise, practical AI news brief for builders.")
	}

	// Format articles for the prompt
	var articleList strings.Builder
	for i, a := range articles {
		if i >= 50 { // Cap at 50 to keep prompt reasonable
			break
		}
		fmt.Fprintf(&articleList, "%d. [%s] %s\n   URL: %s\n", i+1, a.Source, a.Title, a.URL)
		if a.Points > 0 {
			fmt.Fprintf(&articleList, "   HN Points: %d (internal use only — do NOT include in output) | Comments: %s\n", a.Points, a.CommentURL)
		}
		if a.Summary != "" {
			summary := a.Summary
			if len(summary) > 300 {
				summary = summary[:300] + "..."
			}
			fmt.Fprintf(&articleList, "   Summary: %s\n", summary)
		}
		fmt.Fprintln(&articleList)
	}

	prompt := fmt.Sprintf(`You are the editor of the AI Friday Daily Brief, a curated newsletter for the AI Friday meetup in New Orleans.

Today's date: %s
Date path: %s
Previous brief date path: %s

Here are the editorial rules:

%s

Here are today's candidate articles:

%s

Generate the daily brief as a JSON object. The JSON must have this exact structure:

{
  "date": "%s",
  "date_path": "%s",
  "prev_date": "%s",
  "next_date": "",
  "lede": "<HTML string> A 2-4 sentence overview paragraph. Use <strong> for emphasis and <a href='url'> for links. This is the hook — make it punchy and specific. Mention the top 3-5 stories by name with links.",
  "sections": [
    {
      "title": "Section Name (e.g. Things People Built, Big Moves, Tools & Releases)",
      "items": [
        {
          "title": "Article title or rewritten headline",
          "url": "https://...",
          "body": "<HTML string> 2-4 sentences. Can use <strong>, <a href>, <code>. Write for semi-technical builders. Include HN discussion link if relevant.",
          "via": "Source name (e.g. Hacker News, Simon Willison)"
        }
      ]
    }
  ],
  "quick_links": [
    {
      "title": "Short headline",
      "url": "https://...",
      "note": "Brief note (optional)"
    }
  ],
  "sources": [
    {"name": "Source Name", "url": "https://..."}
  ],
  "slack_text": "Conversational Slack message for #daily-brief. Write like a smart friend catching people up over coffee. Start with a warm greeting ('Good morning, NOLA!' or similar) + date + a 1-2 sentence vibe-check on the day. Then 2-3 sections separated by --- with emoji headers. Each item gets a bullet with a <url|linked headline> plus 1-3 conversational sentences about WHY it matters. Not a dry list — give personality and editorial voice. Aim for 8-10 linked items total. Use Slack mrkdwn (*bold*, _italic_, <url|text>). No HTML. The system appends the website link automatically — do NOT add one."
}

Important:
- Pick 5-10 items for the main sections, plus 3-5 quick links
- The "body" field in items uses HTML (not Markdown)
- The "lede" field uses HTML (not Markdown)  
- The "slack_text" field is conversational Slack mrkdwn. Write it like a friend, not a news ticker. Use *bold*, _italic_, <url|text> for links. Every item MUST have a <url|linked headline>. Use --- between sections. No HTML.
- Do NOT include any footer like 'details in thread' or 'full brief' in slack_text — the system appends the website URL automatically
- Diversify sources: don't let more than half the items come from Hacker News. Pull from tech press, newsletters, blogs, and community links too
- Group items into 2-4 sections with descriptive titles
- The lede should reference the most important stories with links
- Follow the RULEBOOK strictly for tone, audience, and content selection
- Do NOT mention Hacker News point counts in any output (lede, body, notes, slack_text). HN points are provided to help you rank/select stories but should never appear in the published brief.
- Output ONLY the JSON object, no markdown fences, no explanation

Podcast Episodes:
- Articles tagged as podcasts (sources like "AI Daily Brief", "How I AI", "Behind the Craft", "AI for Humans", "a16z AI", "AI & I (Every.to)") are recent podcast episodes.
- Include 1-3 notable podcast episodes per brief in a "Worth a Listen" or "Podcasts" section (or fold into other sections if they fit).
- For podcast items, mention the podcast name and guest/topic. Set "via" to the podcast name.
- Only include episodes that are genuinely relevant to the day's news or particularly interesting. Don't pad the brief with filler episodes.

Community Links:
- Articles with source "AI Friday Slack" were shared by community members in the AI Friday Slack.
- Give these a STRONG boost in selection — they reflect what the community is actually talking about.
- If there are any community links worth including, add a dedicated section titled "From the Community" with these items.
- For community items, set "via" to the sharer's name (shown in the Author field), e.g. "via dunn in #ai-friday"
- Community links don't need to pass the same novelty bar as other sources — if someone shared it, it's worth considering.
- Still apply the RULEBOOK tone and accessibility rules to community items.`,
		dateHuman, datePath, prevDate,
		string(rulebook),
		articleList.String(),
		dateHuman, datePath, prevDate)

	// Call Claude
	reqBody := map[string]any{
		"model":      "claude-sonnet-4-5-20250929",
		"max_tokens": 8192,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", llmGateway, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read LLM response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LLM returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse Claude's response
	var claudeResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	if len(claudeResp.Content) == 0 {
		return nil, fmt.Errorf("empty LLM response")
	}

	// Extract JSON from Claude's response (strip markdown fences if present)
	text := strings.TrimSpace(claudeResp.Content[0].Text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		// Remove first and last line
		if len(lines) > 2 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	// Validate it's valid JSON
	var briefData map[string]any
	if err := json.Unmarshal([]byte(text), &briefData); err != nil {
		// Save the raw response for debugging
		os.WriteFile("/tmp/brief_raw_response.txt", []byte(text), 0644)
		return nil, fmt.Errorf("invalid JSON from LLM (saved to /tmp/brief_raw_response.txt): %w", err)
	}

	// Pretty-print
	pretty, err := json.MarshalIndent(briefData, "", "  ")
	if err != nil {
		return []byte(text), nil
	}
	return pretty, nil
}

// ---------------------------------------------------------------------------
// Slack posting
// ---------------------------------------------------------------------------

func postToSlack(briefJSON []byte, datePath string) error {
	var data struct {
		SlackText string `json:"slack_text"`
	}
	if err := json.Unmarshal(briefJSON, &data); err != nil {
		return fmt.Errorf("parse brief JSON: %w", err)
	}

	if data.SlackText == "" {
		return fmt.Errorf("no slack_text in brief JSON")
	}

	bot, err := slackbot.New(nil)
	if err != nil {
		return fmt.Errorf("create slack bot: %w", err)
	}

	return bot.PostDailyBrief(datePath, data.SlackText)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findPrevBrief(projectRoot, currentDatePath string) string {
	briefDir := filepath.Join(projectRoot, "site", "brief")
	var dates []string

	years, _ := os.ReadDir(briefDir)
	for _, y := range years {
		if !y.IsDir() || len(y.Name()) != 4 {
			continue
		}
		months, _ := os.ReadDir(filepath.Join(briefDir, y.Name()))
		for _, m := range months {
			if !m.IsDir() || len(m.Name()) != 2 {
				continue
			}
			days, _ := os.ReadDir(filepath.Join(briefDir, y.Name(), m.Name()))
			for _, d := range days {
				if !d.IsDir() {
					continue
				}
				p := y.Name() + "/" + m.Name() + "/" + d.Name()
				if p < currentDatePath {
					dates = append(dates, p)
				}
			}
		}
	}

	if len(dates) == 0 {
		return ""
	}

	sort.Strings(dates)
	return dates[len(dates)-1]
}

func updatePrevBriefNextLink(projectRoot, prevDatePath, newDatePath string) {
	htmlPath := filepath.Join(projectRoot, "site", "brief", prevDatePath, "index.html")
	content, err := os.ReadFile(htmlPath)
	if err != nil {
		slog.Warn("could not read prev brief for next-link update", "path", htmlPath, "error", err)
		return
	}

	// Replace the empty next placeholder with a real link
	old := `<div class="brief-nav-placeholder"></div>`
	newLink := fmt.Sprintf(`<a href="/brief/%s/" class="brief-nav-link brief-nav-link--next">
        <span class="brief-nav-label">Next &rarr;</span>
        <span class="brief-nav-date">%s</span>
      </a>`, newDatePath, newDatePath)

	updated := strings.Replace(string(content), old, newLink, 1)
	if updated != string(content) {
		os.WriteFile(htmlPath, []byte(updated), 0644)
		slog.Info("updated prev brief next link", "prev", prevDatePath, "next", newDatePath)
	}
}
