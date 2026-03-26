package feeds

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
)

// FeedSource defines an RSS/Atom feed to monitor
type FeedSource struct {
	Name    string
	URL     string
	Tags    []string // e.g. ["blog", "ai"]
	Filter  func(*Article) bool // optional filter
}

// Article is a normalized item from any feed
type Article struct {
	Title       string
	URL         string
	Source      string
	Author      string
	Published   time.Time
	Summary     string
	Tags        []string
	Points      int    // HN points, 0 for others
	CommentURL  string // HN comment link
}

// DefaultFeeds returns the initial set of feeds to monitor
func DefaultFeeds() []FeedSource {
	return []FeedSource{
		// --- Blogs & Labs ---
		{
			Name: "Simon Willison",
			URL:  "https://simonwillison.net/atom/everything/",
			Tags: []string{"blog", "ai", "llm"},
		},
		{
			Name: "OpenAI Blog",
			URL:  "https://openai.com/blog/rss.xml",
			Tags: []string{"blog", "ai", "openai"},
		},
		// Anthropic has no RSS feed as of March 2026
		{
			Name: "Google AI Blog",
			URL:  "https://blog.google/technology/ai/rss/",
			Tags: []string{"blog", "ai", "google"},
		},
		{
			Name: "Hugging Face Blog",
			URL:  "https://huggingface.co/blog/feed.xml",
			Tags: []string{"blog", "ai", "open-source"},
		},
		{
			Name: "Latent Space",
			URL:  "https://latent.space/feed",
			Tags: []string{"newsletter", "ai"},
		},

		// --- Tech Press (AI sections) ---
		{
			Name: "TechCrunch AI",
			URL:  "https://techcrunch.com/category/artificial-intelligence/feed/",
			Tags: []string{"news", "ai", "business"},
		},
		{
			Name: "The Verge AI",
			URL:  "https://www.theverge.com/rss/ai-artificial-intelligence/index.xml",
			Tags: []string{"news", "ai", "consumer"},
		},
		{
			Name: "Ars Technica AI",
			URL:  "https://arstechnica.com/ai/feed/",
			Tags: []string{"news", "ai"},
		},
		{
			Name: "MIT Technology Review AI",
			URL:  "https://www.technologyreview.com/topic/artificial-intelligence/feed/",
			Tags: []string{"news", "ai", "research"},
		},

		// --- Newsletters ---
		{
			Name: "Ben's Bites",
			URL:  "https://bensbites.com/feed",
			Tags: []string{"newsletter", "ai"},
		},
	}
}

// hnAIFilter returns true if a HN article is AI-related.
// Uses word-boundary-aware matching to avoid false positives.
func hnAIFilter(a *Article) bool {
	title := strings.ToLower(a.Title)
	summary := strings.ToLower(a.Summary)
	text := " " + title + " " + summary + " "

	// Exact phrases (no boundary tricks needed)
	phrases := []string{
		"machine learning", "deep learning", "neural network",
		"large language model", "language model",
		"stable diffusion", "hugging face", "open source model",
		"model context protocol", "chain of thought",
		"fine-tune", "fine tune", "vector database",
		"coding agent", "coding assistant", "ai agent",
		"prompt injection", "prompt engineering",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}

	// Word-boundary keywords: surround with non-alpha check
	// These are short terms that could false-positive without boundaries
	words := []string{
		"llm", "llms", "gpt", "chatgpt", "gpt-4", "gpt-5",
		"claude", "gemini", "openai", "anthropic", "deepmind",
		"copilot", "midjourney", "dall-e", "sora",
		"langchain", "llamaindex", "rag",
		"lora", "qlora", "mistral", "llama",
		"agentic", "mcp", "fastmcp",
		"transformer", "diffusion", "embedding",
		"chatbot", "deepseek", "qwen",
	}
	for _, w := range words {
		// Check word boundaries using space/punctuation
		if containsWord(text, w) {
			return true
		}
	}

	// "AI" needs special handling — only match as standalone word
	// to avoid matching "wait", "fair", "air", etc.
	if containsWord(text, "ai") {
		return true
	}

	return false
}

// containsWord checks if word appears in text surrounded by non-alphanumeric chars
func containsWord(text, word string) bool {
	idx := 0
	for {
		i := strings.Index(text[idx:], word)
		if i < 0 {
			return false
		}
		pos := idx + i
		before := pos == 0 || !isAlphaNum(text[pos-1])
		after := pos+len(word) >= len(text) || !isAlphaNum(text[pos+len(word)])
		if before && after {
			return true
		}
		idx = pos + 1
	}
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// FetchAll fetches all configured feeds and returns articles from the last `since` duration
func FetchAll(ctx context.Context, sources []FeedSource, since time.Duration) ([]Article, error) {
	cutoff := time.Now().Add(-since)
	parser := gofeed.NewParser()

	var (
		mu       sync.Mutex
		articles []Article
		wg       sync.WaitGroup
	)

	for _, src := range sources {
		wg.Add(1)
		go func(s FeedSource) {
			defer wg.Done()

			feed, err := parser.ParseURLWithContext(s.URL, ctx)
			if err != nil {
				slog.Warn("feed fetch failed", "source", s.Name, "url", s.URL, "error", err)
				return
			}

			for _, item := range feed.Items {
				pubTime := time.Now()
				if item.PublishedParsed != nil {
					pubTime = *item.PublishedParsed
				} else if item.UpdatedParsed != nil {
					pubTime = *item.UpdatedParsed
				}

				if pubTime.Before(cutoff) {
					continue
				}

				author := ""
				if item.Author != nil {
					author = item.Author.Name
				}

				a := Article{
					Title:     item.Title,
					URL:       item.Link,
					Source:    s.Name,
					Author:    author,
					Published: pubTime,
					Summary:   truncate(stripHTML(item.Description), 500),
					Tags:      s.Tags,
				}

				// Apply source-specific filter
				if s.Filter != nil && !s.Filter(&a) {
					continue
				}

				mu.Lock()
				articles = append(articles, a)
				mu.Unlock()
			}

			slog.Info("feed fetched", "source", s.Name, "items", len(feed.Items))
		}(src)
	}

	wg.Wait()
	return articles, nil
}

func stripHTML(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FormatArticleList returns a simple text summary of articles
func FormatArticleList(articles []Article) string {
	if len(articles) == 0 {
		return "No articles found."
	}
	var b strings.Builder
	for i, a := range articles {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   %s\n   %s\n\n",
			i+1, a.Source, a.Title, a.URL, a.Summary[:min(len(a.Summary), 150)]))
	}
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
