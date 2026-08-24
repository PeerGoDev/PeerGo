package trackeroperationsv1

import (
	"regexp"
	"time"
)

const MaxActivePeerLimit = 200

var activePeerUserIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ActivePeer is the privacy-minimized view of one connection currently held
// by the Tracker Swarm Engine. Socket addresses, ports, passkeys, peer IDs and
// session keys never cross the service boundary.
type ActivePeer struct {
	UserID        string    `json:"user_id"`
	ClientFamily  string    `json:"client_family"`
	AddressFamily int       `json:"address_family"`
	Seedbox       bool      `json:"seedbox"`
	Uploaded      int64     `json:"uploaded"`
	Downloaded    int64     `json:"downloaded"`
	UploadSpeed   int64     `json:"upload_speed"`
	DownloadSpeed int64     `json:"download_speed"`
	Left          int64     `json:"left"`
	LastAnnounce  time.Time `json:"last_announce"`
}

// ActivePeerPage is bounded at the Tracker boundary. Truncated tells Core
// that more active connections exist without requiring a full swarm scan.
type ActivePeerPage struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Items       []ActivePeer `json:"items"`
	Truncated   bool         `json:"truncated"`
}

func (page ActivePeerPage) Valid(limit int) bool {
	if page.GeneratedAt.IsZero() || limit < 1 || limit > MaxActivePeerLimit || page.Items == nil || len(page.Items) > limit {
		return false
	}
	_, offset := page.GeneratedAt.Zone()
	if offset != 0 {
		return false
	}
	for _, peer := range page.Items {
		if !activePeerUserIDPattern.MatchString(peer.UserID) || peer.ClientFamily == "" || len(peer.ClientFamily) > 32 ||
			(peer.AddressFamily != 4 && peer.AddressFamily != 6) || peer.Uploaded < 0 || peer.Downloaded < 0 ||
			peer.UploadSpeed < 0 || peer.DownloadSpeed < 0 || peer.Left < 0 || peer.LastAnnounce.IsZero() || peer.LastAnnounce.After(page.GeneratedAt) {
			return false
		}
		_, peerOffset := peer.LastAnnounce.Zone()
		if peerOffset != 0 {
			return false
		}
	}
	return true
}
