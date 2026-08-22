package trackeroperationsv1

import (
	"testing"
	"time"
)

func TestActivePeerPageValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	page := ActivePeerPage{
		GeneratedAt: now,
		Items: []ActivePeer{{
			UserID: "0198f20a-6da8-7e51-9c64-111111111111", ClientFamily: "qbittorrent",
			Uploaded: 10, Downloaded: 20, Left: 30, LastAnnounce: now.Add(-time.Minute),
		}},
	}
	if !page.Valid(100) {
		t.Fatal("valid active peer page was rejected")
	}
	page.Items[0].UserID = "not-a-user"
	if page.Valid(100) {
		t.Fatal("invalid active peer user was accepted")
	}
}
