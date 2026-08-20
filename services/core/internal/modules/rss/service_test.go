package rss

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewTokenStoresDigestOfOpaqueToken(t *testing.T) {
	service := &Service{readRandom: func(destination []byte) (int, error) {
		for index := range destination {
			destination[index] = byte(index)
		}
		return len(destination), nil
	}}
	raw, digest, err := service.newToken()
	if err != nil {
		t.Fatalf("newToken() error = %v", err)
	}
	if len(raw) != 43 {
		t.Fatalf("raw token length = %d, want 43", len(raw))
	}
	want := sha256.Sum256([]byte(raw))
	if !bytes.Equal(digest, want[:]) {
		t.Fatal("stored digest does not match the issued token")
	}
	if bytes.Contains(digest, []byte(raw)) {
		t.Fatal("stored digest unexpectedly contains the raw token")
	}
}

func TestRenderFeedProducesValidPrivateEnclosureURLs(t *testing.T) {
	origin, err := url.Parse("https://peergo.example")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{publicOrigin: *origin}
	observedAt := time.Date(2026, time.August, 18, 4, 30, 0, 0, time.UTC)
	promotionEndsAt := observedAt.Add(30 * time.Minute)
	data, err := service.renderFeed(
		ResolvedSubscription{Subscription: Subscription{
			Name:             "免费电影",
			IncludeCategory:  true,
			IncludeSubtitle:  true,
			IncludeSize:      true,
			IncludePromotion: true,
		}},
		FeedProjection{ObservedAt: observedAt, Items: []FeedItem{{
			TorrentID:       1234,
			Title:           "Example & Film",
			Subtitle:        "1080p",
			SizeBytes:       2 * 1024 * 1024 * 1024,
			Promotion:       "free",
			PromotionEndsAt: &promotionEndsAt,
			PublishedAt:     observedAt.Add(-time.Hour),
			CategoryName:    "电影",
			Seeders:         10,
			Leechers:        2,
			Completed:       8,
		}}},
		strings.Repeat("a", 43),
	)
	if err != nil {
		t.Fatalf("renderFeed() error = %v", err)
	}
	var parsed struct {
		Channel struct {
			Items []struct {
				Title     string `xml:"title"`
				Enclosure struct {
					URL string `xml:"url,attr"`
				} `xml:"enclosure"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("rendered RSS is invalid XML: %v\n%s", err, data)
	}
	if len(parsed.Channel.Items) != 1 {
		t.Fatalf("item count = %d, want 1", len(parsed.Channel.Items))
	}
	item := parsed.Channel.Items[0]
	if item.Title != "Example & Film" {
		t.Fatalf("title = %q", item.Title)
	}
	wantURL := "https://peergo.example/rss/" + strings.Repeat("a", 43) + "/torrents/1234/download"
	if item.Enclosure.URL != wantURL {
		t.Fatalf("enclosure URL = %q, want %q", item.Enclosure.URL, wantURL)
	}
	if !bytes.Contains(data, []byte("xmlns:atom=\"http://www.w3.org/2005/Atom\"")) {
		t.Fatalf("RSS is missing the Atom namespace: %s", data)
	}
}

func TestNormalizeSubscriptionInputRejectsDuplicateFilters(t *testing.T) {
	_, err := normalizeSubscriptionInput(SubscriptionInput{
		Name: "duplicates", ItemLimit: 20, PriceFilter: PriceFilterAll,
		PromotionFilters: []string{"free", "free"},
	})
	if err != ErrInvalidInput {
		t.Fatalf("normalizeSubscriptionInput() error = %v, want %v", err, ErrInvalidInput)
	}
}
