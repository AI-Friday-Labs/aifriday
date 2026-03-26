package feeds

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"srv.exe.dev/db/dbgen"
)

// SlackLinkArticles reads stored Slack links from the database and converts
// them into feed Articles for the daily brief pipeline.
// It looks back the given duration from now and deduplicates by URL.
func SlackLinkArticles(db *sql.DB, since time.Duration) ([]Article, error) {
	cutoff := time.Now().UTC().Add(-since)
	// Format as SQLite-compatible string (space separator, not 'T') so text
	// comparison works correctly against CURRENT_TIMESTAMP-generated values.
	cutoffStr := cutoff.Format("2006-01-02 15:04:05")

	links, err := slackLinksSinceStr(db, cutoffStr)
	if err != nil {
		return nil, err
	}

	slog.Info("slack links fetched", "count", len(links), "since", cutoff.Format(time.RFC3339))

	// Deduplicate by canonical URL
	seen := make(map[string]struct{})
	type slackItem struct {
		link     dbgen.SlackLink
		cleanURL string
	}
	var unique []slackItem
	for _, link := range links {
		normalized := canonicalURL(link.Url)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		unique = append(unique, slackItem{link: link, cleanURL: stripUTMParams(link.Url)})
	}

	// Fetch page metadata in parallel
	type pageMeta struct {
		Title       string
		Description string
	}
	metas := make([]pageMeta, len(unique))
	var wg sync.WaitGroup
	for i, item := range unique {
		wg.Add(1)
		go func(idx int, u string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			title, desc, err := fetchPageMeta(ctx, u)
			if err != nil {
				slog.Debug("failed to fetch page meta", "url", u, "error", err)
				return
			}
			metas[idx] = pageMeta{Title: title, Description: desc}
		}(i, item.cleanURL)
	}
	wg.Wait()

	var articles []Article
	for i, item := range unique {
		meta := metas[i]
		title := slackLinkTitle(item.link)
		// Prefer the page's own title if we got one
		if meta.Title != "" {
			title = meta.Title
		}
		summary := cleanSlackText(item.link.MessageText)
		if meta.Description != "" {
			// Combine: page description + what the person said
			summary = meta.Description + " | Shared by " + item.link.UserName + ": " + summary
		}
		articles = append(articles, Article{
			Title:     title,
			URL:       item.cleanURL,
			Source:    "AI Friday Slack",
			Author:    item.link.UserName,
			Published: item.link.CreatedAt,
			Summary:   summary,
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
	// Also remove URL-with-UTM-stripped version
	without = strings.ReplaceAll(without, stripUTMParams(link.Url), "")
	without = strings.TrimSpace(without)

	// Strip Slack markup: <@user> mentions, <url|label> format
	without = cleanSlackText(without)
	without = strings.TrimSpace(without)

	if without != "" {
		// Use the first line of remaining text as the title
		if i := strings.IndexAny(without, "\n\r"); i > 0 {
			without = without[:i]
		}
		without = strings.TrimSpace(without)
		// Skip generic/conversational fragments — need at least 15 chars of substance
		if len(without) >= 15 {
			if len(without) > 120 {
				without = without[:120] + "…"
			}
			return without
		}
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

// fetchPageMeta fetches a URL and extracts the page title and meta description.
func fetchPageMeta(ctx context.Context, rawURL string) (title, description string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "AIFridayBot/1.0 (https://aifriday.com)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read up to 256KB — we only need the <head>
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", "", err
	}

	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", "", err
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom.String() == "title" && title == "" {
			if n.FirstChild != nil {
				title = strings.TrimSpace(n.FirstChild.Data)
			}
		}
		if n.Type == html.ElementNode && n.DataAtom.String() == "meta" {
			var name, property, content string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "name":
					name = strings.ToLower(a.Val)
				case "property":
					property = strings.ToLower(a.Val)
				case "content":
					content = a.Val
				}
			}
			if (name == "description" || property == "og:description") && description == "" {
				description = strings.TrimSpace(content)
			}
			if property == "og:title" && title == "" {
				title = strings.TrimSpace(content)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title, description, nil
}

// canonicalURL strips tracking parameters and trailing slashes for dedup.
func canonicalURL(rawURL string) string {
	u, err := url.Parse(strings.ReplaceAll(rawURL, "&amp;", "&"))
	if err != nil {
		return strings.TrimRight(rawURL, "/")
	}
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return strings.TrimRight(u.String(), "/")
}

// stripUTMParams removes UTM tracking parameters from a URL.
func stripUTMParams(rawURL string) string {
	// Also fix HTML-encoded ampersands from Slack
	rawURL = strings.ReplaceAll(rawURL, "&amp;", "&")
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// cleanSlackText strips Slack markup from message text for use as a summary.
func cleanSlackText(s string) string {
	s = strings.TrimSpace(s)
	// Strip <@USER_ID> mentions
	for {
		start := strings.Index(s, "<@")
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			break
		}
		s = s[:start] + s[start+end+1:]
	}
	// Strip URL markup, keeping labels
	s = stripSlackURLMarkup(s)
	// Fix HTML entities
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "-->", "→")
	// Collapse whitespace
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
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

// slackLinksSinceStr queries slack_links with a pre-formatted string cutoff
// to avoid Go time.Time serialization issues (T vs space separator).
func slackLinksSinceStr(db *sql.DB, cutoff string) ([]dbgen.SlackLink, error) {
	const query = `SELECT id, url, channel_id, channel_name, user_id, user_name, message_ts, message_text, created_at FROM slack_links WHERE created_at >= ? ORDER BY created_at DESC`
	rows, err := db.Query(query, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []dbgen.SlackLink
	for rows.Next() {
		var i dbgen.SlackLink
		var createdStr string
		if err := rows.Scan(&i.ID, &i.Url, &i.ChannelID, &i.ChannelName, &i.UserID, &i.UserName, &i.MessageTs, &i.MessageText, &createdStr); err != nil {
			return nil, err
		}
		i.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		items = append(items, i)
	}
	return items, rows.Err()
}
