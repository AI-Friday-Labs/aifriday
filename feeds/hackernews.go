package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const hnBaseURL = "https://hacker-news.firebaseio.com/v0"

// HNItem represents a Hacker News story
type HNItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Score       int    `json:"score"`
	Descendants int    `json:"descendants"` // comment count
	By          string `json:"by"`
	Time        int64  `json:"time"` // unix timestamp
	Type        string `json:"type"`
}

// FetchHNTopStories fetches top stories from the HN API, filters for AI-related
// content, and returns them as Articles.
// minPoints: minimum score threshold (e.g. 50)
// maxStories: how many top story IDs to check (e.g. 200)
// since: only return stories newer than this duration
func FetchHNTopStories(ctx context.Context, minPoints int, maxStories int, since time.Duration) ([]Article, error) {
	cutoff := time.Now().Add(-since)

	// Fetch top story IDs
	ids, err := fetchHNStoryIDs(ctx, "topstories")
	if err != nil {
		return nil, fmt.Errorf("fetch top stories: %w", err)
	}

	// Also grab best stories for broader coverage
	bestIDs, err := fetchHNStoryIDs(ctx, "beststories")
	if err != nil {
		slog.Warn("failed to fetch best stories", "error", err)
	} else {
		// Merge, dedup
		seen := make(map[int]bool, len(ids))
		for _, id := range ids {
			seen[id] = true
		}
		for _, id := range bestIDs {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}

	if len(ids) > maxStories {
		ids = ids[:maxStories]
	}

	// Fetch items concurrently (bounded)
	var (
		mu       sync.Mutex
		articles []Article
		wg       sync.WaitGroup
		sem      = make(chan struct{}, 20) // concurrency limit
	)

	for _, id := range ids {
		wg.Add(1)
		go func(storyID int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			item, err := fetchHNItem(ctx, storyID)
			if err != nil {
				return
			}

			// Skip non-stories
			if item.Type != "story" || item.URL == "" {
				return
			}

			// Time filter
			pubTime := time.Unix(item.Time, 0)
			if pubTime.Before(cutoff) {
				return
			}

			// Points filter
			if item.Score < minPoints {
				return
			}

			a := Article{
				Title:      item.Title,
				URL:        item.URL,
				Source:     "Hacker News",
				Author:     item.By,
				Published:  pubTime,
				Points:     item.Score,
				CommentURL: fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID),
				Tags:       []string{"hn"},
			}

			// AI filter
			if !hnAIFilter(&a) {
				return
			}

			mu.Lock()
			articles = append(articles, a)
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	slog.Info("HN API fetch complete", "checked", len(ids), "ai_matches", len(articles))
	return articles, nil
}

func fetchHNStoryIDs(ctx context.Context, endpoint string) ([]int, error) {
	url := fmt.Sprintf("%s/%s.json", hnBaseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ids []int
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func fetchHNItem(ctx context.Context, id int) (*HNItem, error) {
	url := fmt.Sprintf("%s/item/%d.json", hnBaseURL, id)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var item HNItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	return &item, nil
}
