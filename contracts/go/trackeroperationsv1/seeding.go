package trackeroperationsv1

const (
	SeedingTotalsPath     = "/internal/v1/operations/seeding-totals"
	MaxSeedingTotalsUsers = 50
)

// UserSeedingTotal describes complete, online torrents, with each torrent counted once.
type UserSeedingTotal struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

type UserSeedingTotals map[string]UserSeedingTotal

func (totals UserSeedingTotals) Valid(userIDs []string) bool {
	if len(userIDs) == 0 || len(userIDs) > MaxSeedingTotalsUsers || len(totals) != len(userIDs) {
		return false
	}
	for _, userID := range userIDs {
		total, ok := totals[userID]
		if !ok || !activePeerUserIDPattern.MatchString(userID) || total.Count < 0 ||
			total.Bytes < int64(total.Count) || (total.Count == 0 && total.Bytes != 0) {
			return false
		}
	}
	return true
}
