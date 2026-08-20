// Package ratelimit provides bounded process-local protection for Tracker
// request paths. It stores only domain-separated SHA-256 keys, never raw IP
// addresses, passkeys or user identifiers.
package ratelimit

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

var ErrConfig = errors.New("Tracker rate limiter configuration is invalid")

const staleAfter = 10 * time.Minute

type bucket struct {
	tokens float64
	seenAt time.Time
}

type shard struct {
	mu      sync.Mutex
	buckets map[[sha256.Size]byte]bucket
}

type Limiter struct {
	shards             []shard
	maxEntriesPerShard int
}

func New(shardCount, maxEntries int) (*Limiter, error) {
	if shardCount < 1 || shardCount > 256 || maxEntries < shardCount || maxEntries > 10_000_000 {
		return nil, ErrConfig
	}
	limiter := &Limiter{shards: make([]shard, shardCount), maxEntriesPerShard: maxEntries / shardCount}
	for index := range limiter.shards {
		limiter.shards[index].buckets = make(map[[sha256.Size]byte]bucket)
	}
	return limiter, nil
}

func (limiter *Limiter) AllowAddress(address string, requestsPerMinute, burst int, now time.Time) bool {
	return limiter.allow("address\x00", address, requestsPerMinute, burst, now)
}

func (limiter *Limiter) AllowUser(userID string, requestsPerMinute, burst int, now time.Time) bool {
	return limiter.allow("user\x00", userID, requestsPerMinute, burst, now)
}

func (limiter *Limiter) allow(domain, value string, requestsPerMinute, burst int, now time.Time) bool {
	if value == "" || requestsPerMinute < 1 || burst < 1 || now.IsZero() {
		return false
	}
	digest := sha256.Sum256(append([]byte(domain), []byte(value)...))
	index := int(binary.BigEndian.Uint64(digest[:8]) % uint64(len(limiter.shards)))
	part := &limiter.shards[index]
	part.mu.Lock()
	defer part.mu.Unlock()
	current, exists := part.buckets[digest]
	if !exists {
		if len(part.buckets) >= limiter.maxEntriesPerShard {
			for key, candidate := range part.buckets {
				if now.Sub(candidate.seenAt) > staleAfter {
					delete(part.buckets, key)
				}
			}
			if len(part.buckets) >= limiter.maxEntriesPerShard {
				return false
			}
		}
		part.buckets[digest] = bucket{tokens: float64(burst - 1), seenAt: now}
		return true
	}
	elapsed := now.Sub(current.seenAt).Seconds()
	if elapsed < 0 {
		return false
	}
	current.tokens = min(float64(burst), current.tokens+elapsed*float64(requestsPerMinute)/60)
	current.seenAt = now
	if current.tokens < 1 {
		part.buckets[digest] = current
		return false
	}
	current.tokens--
	part.buckets[digest] = current
	return true
}
