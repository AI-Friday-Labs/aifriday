package feeds

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// skipSenders are From addresses to ignore entirely (system emails, not newsletters)
var skipSenders = map[string]bool{
	"support@exe.dev": true,
}

// emailHeader holds parsed email headers
type emailHeader struct {
	From    string
	Subject string
	Date    string
	ContentType string
}

// ProcessMaildir scans ~/Maildir/new for newsletter emails, extracts links,
// stores them in the database, and moves processed files to ~/Maildir/cur.
func ProcessMaildir(db *sql.DB, maildir string) error {
	newDir := filepath.Join(maildir, "new")
	curDir := filepath.Join(maildir, "cur")
	os.MkdirAll(curDir, 0755)

	entries, err := os.ReadDir(newDir)
	if err != nil {
		return fmt.Errorf("read maildir: %w", err)
	}

	var processed, skipped int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			continue
		}

		path := filepath.Join(newDir, entry.Name())
		newsletter, err := processEmail(db, path, entry.Name())
		if err != nil {
			slog.Warn("failed to process email", "file", entry.Name(), "error", err)
			skipped++
			continue
		}

		if newsletter == "" {
			// Not a recognized newsletter, skip but still move
			skipped++
		} else {
			processed++
		}

		// Move to cur/ to avoid reprocessing
		dst := filepath.Join(curDir, entry.Name())
		if err := os.Rename(path, dst); err != nil {
			slog.Warn("failed to move email to cur", "file", entry.Name(), "error", err)
		}
	}

	slog.Info("newsletter maildir processed", "processed", processed, "skipped", skipped)
	return nil
}

func processEmail(db *sql.DB, path, filename string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	headers, body, err := parseEmail(f)
	if err != nil {
		return "", err
	}

	// Identify newsletter
	newsletter := identifyNewsletter(headers.From)
	if newsletter == "" {
		return "", nil
	}

	// Skip confirmation/welcome emails — they don't contain real articles
	subjectLower := strings.ToLower(headers.Subject)
	skipSubjects := []string{"confirm your", "welcome to", "verify your", "please confirm",
		"confirm signup", "quick steps to get started", "here's your access"}
	for _, s := range skipSubjects {
		if strings.Contains(subjectLower, s) {
			slog.Debug("skipping welcome/confirm email", "newsletter", newsletter, "subject", headers.Subject)
			return "", nil
		}
	}

	// Extract links from HTML body
	links := extractLinks(body)
	if len(links) == 0 {
		slog.Debug("no links found in newsletter", "newsletter", newsletter, "subject", headers.Subject)
		return newsletter, nil
	}

	// Filter to interesting links (skip tracking, unsubscribe, social, etc.)
	filtered := filterLinks(links)

	// Store in database
	var inserted int
	for _, link := range filtered {
		err := insertNewsletterArticle(db, link.url, link.text, newsletter, headers.Subject, headers.Date, filename)
		if err != nil {
			// Duplicate is fine
			continue
		}
		inserted++
	}

	slog.Info("newsletter processed", "newsletter", newsletter, "subject", headers.Subject,
		"links_found", len(links), "links_filtered", len(filtered), "inserted", inserted)
	return newsletter, nil
}

type extractedLink struct {
	url  string
	text string
}

func parseEmail(r io.Reader) (emailHeader, string, error) {
	br := bufio.NewReader(r)
	var headers emailHeader
	var lastKey string

	// Parse headers
	for {
		line, err := br.ReadString('\n')
		if err != nil && line == "" {
			break
		}
		line = strings.TrimRight(line, "\r\n")

		// Empty line = end of headers
		if line == "" {
			break
		}

		// Continuation line (starts with whitespace)
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			value := strings.TrimSpace(line)
			switch lastKey {
			case "from":
				headers.From += " " + value
			case "subject":
				headers.Subject += " " + value
			case "content-type":
				headers.ContentType += " " + value
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		lastKey = key

		switch key {
		case "from":
			headers.From = value
		case "subject":
			headers.Subject = decodeRFC2047(value)
		case "date":
			headers.Date = value
		case "content-type":
			headers.ContentType = value
		}
	}

	// Read body
	bodyBytes, err := io.ReadAll(br)
	if err != nil {
		return headers, "", fmt.Errorf("read body: %w", err)
	}

	body := string(bodyBytes)

	// Handle multipart emails — extract HTML part
	if strings.Contains(headers.ContentType, "multipart/") {
		htmlBody := extractHTMLFromMultipart(headers.ContentType, body)
		if htmlBody != "" {
			body = htmlBody
		}
	}

	return headers, body, nil
}

func extractHTMLFromMultipart(contentType, body string) string {
	// Extract boundary from content-type
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}

	boundary, ok := params["boundary"]
	if !ok {
		return ""
	}

	mr := multipart.NewReader(strings.NewReader(body), boundary)
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}

		partCT := part.Header.Get("Content-Type")
		partCTE := strings.ToLower(part.Header.Get("Content-Transfer-Encoding"))

		var partBody []byte
		switch partCTE {
		case "base64":
			partBody, err = io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		case "quoted-printable":
			partBody, err = io.ReadAll(quotedprintable.NewReader(part))
		default:
			partBody, err = io.ReadAll(part)
		}
		if err != nil {
			continue
		}

		if strings.Contains(partCT, "text/html") {
			return string(partBody)
		}

		// Nested multipart (e.g. multipart/alternative inside multipart/mixed)
		if strings.Contains(mediaType, "multipart") || strings.Contains(partCT, "multipart/") {
			nested := extractHTMLFromMultipart(partCT, string(partBody))
			if nested != "" {
				return nested
			}
		}
	}
	return ""
}

// decodeRFC2047 decodes MIME encoded-word strings (=?charset?encoding?text?=)
func decodeRFC2047(s string) string {
	dec := new(mime.WordDecoder)
	result, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return result
}

var linkRe = regexp.MustCompile(`<a\s[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)

func extractLinks(html string) []extractedLink {
	matches := linkRe.FindAllStringSubmatch(html, -1)
	var links []extractedLink
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		rawURL := strings.TrimSpace(m[1])
		text := strings.TrimSpace(stripHTML(m[2]))

		// Decode HTML entities
		rawURL = strings.ReplaceAll(rawURL, "&amp;", "&")

		// Dedup
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true

		links = append(links, extractedLink{url: rawURL, text: text})
	}
	return links
}

// skipDomains are domains we never want to extract as article links
var skipDomains = map[string]bool{
	"list-manage.com":      true,
	"mailchimp.com":        true,
	"facebook.com":         true,
	"twitter.com":          true,
	"x.com":                true,
	"instagram.com":        true,
	"linkedin.com":         true,
	"tiktok.com":           true,
	"bit.ly":               true,
	"goo.gl":               true,
	"mailto":               true,
}

// skipPatterns are URL patterns to skip
var skipPatterns = []string{
	"unsubscribe",
	"manage-preferences",
	"email-preferences",
	"view-in-browser",
	"view-online",
	"browser-version",
	"click.convertkit",
	"trk.klclick",
	"pixel",
	"mailto:",
	"tel:",
	"app-link/post",  // substack app deep links
}

func filterLinks(links []extractedLink) []extractedLink {
	var result []extractedLink
	for _, link := range links {
		if shouldSkipLink(link.url, link.text) {
			continue
		}
		// Clean the URL
		cleaned := cleanNewsletterURL(link.url)
		if cleaned == "" {
			continue
		}
		link.url = cleaned
		result = append(result, link)
	}
	return result
}

func shouldSkipLink(rawURL, text string) bool {
	lower := strings.ToLower(rawURL)

	// Skip non-http links
	if !strings.HasPrefix(lower, "http") {
		return true
	}

	// Skip known patterns
	for _, pat := range skipPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}

	// Parse the URL to check domain
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}

	host := strings.ToLower(u.Hostname())

	// Check exact domain matches
	for domain := range skipDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}

	// Skip if link text is too short to be a meaningful article title.
	// Real article headlines are typically 20+ characters.
	cleanText := strings.TrimSpace(text)
	if len(cleanText) < 20 {
		return true
	}
	lowerText := strings.ToLower(cleanText)

	// Skip generic/navigation link text
	genericPrefixes := []string{
		"read more", "click here", "learn more", "view ", "here",
		"subscribe", "share", "forward", "unsubscribe", "manage",
		"read online", "sign up", "advertise", "view in browser",
		"trending ai tools", "everything else in ai",
		"read the full", "try for free", "get started",
		"confirm", "open in app", "read in app", "download",
		"follow me", "follow us", "join the", "join our",
		"check it out", "update your preferences",
		"want absolutely everything", "ai skill of the day",
		"the rundown roundtable",
	}
	for _, g := range genericPrefixes {
		if strings.HasPrefix(lowerText, g) {
			return true
		}
	}

	// Skip sponsor/ad indicators
	if strings.Contains(lowerText, "sponsored") || strings.Contains(lowerText, "advertisement") {
		return true
	}

	// Skip email addresses
	if strings.Contains(cleanText, "@") && strings.Contains(cleanText, ".") {
		return true
	}

	// Skip arrow-prefixed calls to action ("→ Read the deep dive")
	if strings.HasPrefix(cleanText, "→") || strings.HasPrefix(cleanText, "➜") {
		return true
	}

	// Skip star ratings ("⭐️⭐️⭐️ Nailed it", "⭐⭐ Could Be Better")
	if strings.HasPrefix(cleanText, "⭐") {
		return true
	}

	// Skip newsletter boilerplate and section headers
	boilerplate := []string{
		"highlights:", "news, guides", "powered by", "terms of service",
		"privacy policy", "upgrade your", "get early access",
		"create your own", "start today with",
	}
	for _, h := range boilerplate {
		if strings.Contains(lowerText, h) {
			return true
		}
	}

	// Skip bare URLs used as link text ("https://www.example.com")
	if strings.HasPrefix(cleanText, "http://") || strings.HasPrefix(cleanText, "https://") {
		return true
	}

	return false
}

// unwrapTrackingURL attempts to extract the real destination URL from
// newsletter tracking redirects. Returns the original URL if no pattern matches.
func unwrapTrackingURL(rawURL string) string {
	// TLDR: tracking.tldrnewsletter.com/CL0/https:%2F%2Freal-url.com%2Fpath/N/...
	if strings.Contains(rawURL, "tldrnewsletter.com/CL0/") {
		parts := strings.SplitN(rawURL, "/CL0/", 2)
		if len(parts) == 2 {
			encoded := parts[1]
			// The real URL is URL-encoded and followed by /N/tracking-id
			decoded, err := url.PathUnescape(encoded)
			if err == nil {
				// Strip the trailing /N/tracking-id
				re := regexp.MustCompile(`^(https?://.*?)(?:/\d+/[a-f0-9-]+.*)$`)
				if m := re.FindStringSubmatch(decoded); m != nil {
					return m[1]
				}
				return decoded
			}
		}
	}

	// Substack: substack.com/redirect/2/BASE64... or substack.com/redirect/UUID...
	if strings.Contains(rawURL, "substack.com/redirect/2/") {
		parts := strings.SplitN(rawURL, "/redirect/2/", 2)
		if len(parts) == 2 {
			// Base64 encoded JSON: {"e":"https://real-url"}
			b64 := parts[1]
			// URL params may follow after ?
			if idx := strings.IndexByte(b64, '?'); idx >= 0 {
				b64 = b64[:idx]
			}
			// Add padding if needed
			switch len(b64) % 4 {
			case 2:
				b64 += "=="
			case 3:
				b64 += "="
			}
			decoded, err := b64Decode(b64)
			if err == nil {
				// Parse {"e":"url"}
				if idx := strings.Index(decoded, `"e":"`); idx >= 0 {
					rest := decoded[idx+5:]
					if end := strings.IndexByte(rest, '"'); end >= 0 {
						return rest[:end]
					}
				}
			}
		}
	}

	// TLDR short links: links.tldrnewsletter.com/XXXXX — these are fine as-is,
	// they'll redirect when users click them

	// Beehiiv: link.mail.beehiiv.com/ss/... — opaque tokens, can't unwrap
	// server-side. Keep as-is; the link text provides the article title.

	return rawURL
}

func b64Decode(s string) (string, error) {
	// Try standard base64 first, then URL-safe
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		decoded, err := enc.DecodeString(s)
		if err == nil {
			return string(decoded), nil
		}
	}
	return "", fmt.Errorf("base64 decode failed")
}

func cleanNewsletterURL(rawURL string) string {
	// First try to unwrap tracking redirects
	unwrapped := unwrapTrackingURL(rawURL)

	// Parse and strip UTM parameters
	u, err := url.Parse(unwrapped)
	if err != nil {
		return unwrapped
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

// identifyNewsletter extracts the sender display name from a From header.
// Returns "" only for explicitly skipped senders (system emails).
// Any other email to the newsletters inbox is treated as a newsletter.
func identifyNewsletter(from string) string {
	// Check skip list
	for addr := range skipSenders {
		if strings.Contains(strings.ToLower(from), addr) {
			return ""
		}
	}

	// Skip confirmation/welcome subject patterns (handled in processEmail)

	// Extract display name: "The Rundown AI <news@foo.com>" -> "The Rundown AI"
	if idx := strings.IndexByte(from, '<'); idx > 0 {
		name := strings.TrimSpace(from[:idx])
		// Strip surrounding quotes if present
		name = strings.Trim(name, `"`)
		if name != "" {
			return name
		}
	}

	// No display name — use the part before @ as a fallback
	// e.g. "news@daily.therundown.ai" -> "daily.therundown.ai"
	email := strings.Trim(from, "<> ")
	if idx := strings.IndexByte(email, '@'); idx >= 0 {
		domain := email[idx+1:]
		// Strip common prefixes
		for _, prefix := range []string{"mail.", "em.", "newsletter.", "daily."} {
			domain = strings.TrimPrefix(domain, prefix)
		}
		return domain
	}

	return from
}

func insertNewsletterArticle(db *sql.DB, articleURL, title, newsletter, subject, date, filename string) error {
	_, err := db.Exec(
		`INSERT INTO newsletter_articles (url, title, newsletter, email_subject, email_date, email_file)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(url, newsletter) DO NOTHING`,
		articleURL, title, newsletter, subject, date, filename,
	)
	return err
}

// NewsletterArticles returns articles from newsletters stored in the database
// for the given lookback duration.
func NewsletterArticles(db *sql.DB, since time.Duration) ([]Article, error) {
	cutoff := time.Now().UTC().Add(-since)
	cutoffStr := cutoff.Format("2006-01-02 15:04:05")

	rows, err := db.Query(
		`SELECT url, title, newsletter, email_subject, email_date
		 FROM newsletter_articles
		 WHERE created_at >= ?
		 ORDER BY created_at DESC`,
		cutoffStr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var articles []Article
	for rows.Next() {
		var articleURL, title, newsletter, subject, dateStr string
		if err := rows.Scan(&articleURL, &title, &newsletter, &subject, &dateStr); err != nil {
			return nil, err
		}

		// Dedup by URL across newsletters
		normalized := canonicalURL(articleURL)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true

		// Use title from link text, fall back to email subject
		if title == "" {
			title = subject
		}

		pubTime := time.Now()
		if t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", dateStr); err == nil {
			pubTime = t
		} else if t, err := time.Parse("Mon, 2 Jan 2006 15:04:05 -0700", dateStr); err == nil {
			pubTime = t
		} else if t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 +0000 (UTC)", dateStr); err == nil {
			pubTime = t
		} else if t, err := time.Parse("Mon, 2 Jan 2006 15:04:05 +0000 (UTC)", dateStr); err == nil {
			pubTime = t
		}

		articles = append(articles, Article{
			Title:     title,
			URL:       articleURL,
			Source:    newsletter,
			Author:    newsletter,
			Published: pubTime,
			Summary:   "Featured in " + newsletter + ": " + subject,
			Tags:      []string{"newsletter"},
		})
	}

	return articles, rows.Err()
}
