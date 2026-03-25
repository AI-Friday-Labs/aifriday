package feeds

import (
	"context"
	"database/sql"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"srv.exe.dev/db/dbgen"
)

// SlackLinkArticles reads stored Slack links from the database and converts
// them into feed Articles for the daily brief pipeline.
// It looks back the given duration from now and deduplicates by URL.
func SlackLinkArticles(db *sql.DB, since time.Duration) ([]Article, error) {
	cutoff := time.Now().Add(-since)
	q := dbgen.New(db)

	links, err := q.SlackLinksSince(context.Background(), cutoff)
	if err != nil {
		return nil, err
	}

	slog.Info("slack links fetched", "count", len(links), "since", cutoff.Format(time.RFC3339))

	seen := make(map[string]struct{})
	var articles []Article

	for _, link := range links {
		// Deduplicate by URL — keep first occurrence (results are ordered DESC by created_at)
		normalized := strings.TrimRight(link.Url, "/")
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}

		articles = append(articles, Article{
			Title:     slackLinkTitle(link),
			URL:       link.Url,
			Source:    "AI Friday Slack",
			Author:    link.UserName,
			Published: link.CreatedAt,
			Summary:   strings.TrimSpace(link.MessageText),
			Tags:      []string{"slack", "community"},
			Points:    0,
		})
	}

	return articles, nil
}

// slackLinkTitle derives a title for a Slack link.
// It tries to extract a meaningful title from the message text;
// if the message is empty or is just the URL itself, it falls back to the URL domain.
func slackLinkTitle(link dbgen.SlackLink) string {
	text := strings.TrimSpace(link.MessageText)

	// Remove the URL itself from the message to see if there's surrounding context
	without := strings.ReplaceAll(text, link.Url, "")
	without = strings.TrimSpace(without)

	// Also strip Slack's <url|label> format
	without = stripSlackURLMarkup(without)
	without = strings.TrimSpace(without)

	if without != "" {
		// Use the first line of remaining text as the title
		if i := strings.IndexAny(without, "\n\r"); i > 0 {
			without = without[:i]
		}
		// Cap length
		if len(without) > 120 {
			without = without[:120] + "…"
		}
		return without
	}

	// Fallback: use the domain from the URL
	return domainFromURL(link.Url)
}

// stripSlackURLMarkup removes Slack-style <url> and <url|label> markup,
// keeping only the label text (or nothing if there's no label).
func stripSlackURLMarkup(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		start := strings.IndexByte(s, '<')
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			// Malformed markup; keep the rest
			b.WriteString(s[start:])
			break
		}
		inner := s[start+1 : start+end]
		if pipe := strings.IndexByte(inner, '|'); pipe >= 0 {
			// Keep the label part after the pipe
			b.WriteString(inner[pipe+1:])
		}
		// If no pipe, it's just a bare <url> — drop it entirely
		s = s[start+end+1:]
	}
	return b.String()
}

// domainFromURL extracts the hostname from a URL, stripping "www." prefix.
func domainFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	host := u.Hostname()
	host = strings.TrimPrefix(host, "www.")
	return host
}
