package swarm

import (
	"context"
	"time"
)

// SeedingSwarmsByUser scans the live swarms once for a page of users and
// returns each matching swarm once per user, regardless of client count.
func (engine *Engine) SeedingSwarmsByUser(ctx context.Context, userIDs []string, now time.Time) (map[string][][20]byte, error) {
	matches := make(map[string][][20]byte, len(userIDs))
	for _, userID := range userIDs {
		matches[userID] = nil
	}
	for index := range engine.shards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		shard := &engine.shards[index]
		shard.mu.Lock()
		for infoHash, state := range shard.swarms {
			if state.complete == 0 {
				continue
			}
			for _, peer := range state.peers {
				if peer.left != 0 || !peer.expiresAt.After(now) || peer.lastAnnounce.After(now) {
					continue
				}
				hashes, requested := matches[peer.userID]
				if requested && (len(hashes) == 0 || hashes[len(hashes)-1] != infoHash) {
					matches[peer.userID] = append(hashes, infoHash)
				}
			}
		}
		shard.mu.Unlock()
	}
	return matches, ctx.Err()
}
