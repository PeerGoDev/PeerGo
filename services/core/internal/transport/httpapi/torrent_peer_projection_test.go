package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestTorrentPeerProjectionsSeparateMemberAndManagerIdentity(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	page := torrents.ManagedTorrentPeerList{
		TorrentID: 42, TotalConnections: 2, GeneratedAt: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		Items: []torrents.ManagedTorrentPeer{{
			UserID: userID, NumericID: 7, Username: "real-uploader", DisplayName: "真实发布者",
			ClientFamilies: []string{"qbittorrent"}, AddressFamilies: []string{"ipv4", "ipv6"},
			ActiveConnections: 2, SeedingConnections: 2, ProgressBasisPoints: 10_000,
			Uploaded: 2_048, UploadSpeed: 128, LastAnnounce: time.Date(2026, time.August, 23, 23, 59, 0, 0, time.UTC),
			Uploader: true, AnonymousUploader: true, Seedbox: true,
		}},
	}

	member := torrentPeerListDTO(page)
	if len(member.Items) != 1 || !member.Items[0].Anonymous || member.Items[0].UserNumericId != 0 ||
		member.Items[0].Username != "anonymous" || member.Items[0].DisplayName != "匿名" ||
		len(member.Items[0].AddressFamilies) != 2 || !member.Items[0].Seedbox || member.Items[0].UploadSpeed != "128" {
		t.Fatalf("member peer projection = %+v", member.Items)
	}

	managed := managedTorrentPeerListDTO(page)
	if len(managed.Items) != 1 || managed.Items[0].UserId != userID || managed.Items[0].UserNumericId != 7 ||
		managed.Items[0].Username != "real-uploader" || !managed.Items[0].AnonymousUploader ||
		len(managed.Items[0].AddressFamilies) != 2 || !managed.Items[0].Seedbox {
		t.Fatalf("managed peer projection = %+v", managed.Items)
	}
}
