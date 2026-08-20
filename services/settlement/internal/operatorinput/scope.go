// Package operatorinput parses control-plane selectors shared by immutable
// Settlement timeline commands. Keeping it here prevents economic and H&R
// policy tools from drifting into subtly different selector semantics.
package operatorinput

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

type ScopeValues struct {
	UserID                 string
	TorrentID              string
	TorrentControlSequence string
	SubjectControlSequence string
}

func RequiredUUID(value, name string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s must be a UUID", name)
	}
	return parsed, nil
}

func RequiredTime(value, name string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil || parsed.IsZero() {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp", name)
	}
	return parsed.UTC(), nil
}

func ParseScope(values ScopeValues) (timeline.Scope, error) {
	var scope timeline.Scope
	if raw := strings.TrimSpace(values.UserID); raw != "" {
		parsed, err := RequiredUUID(raw, "--user-id")
		if err != nil {
			return timeline.Scope{}, err
		}
		scope.UserID = &parsed
	}
	for _, field := range []struct {
		raw    string
		target **int64
		name   string
	}{
		{values.TorrentID, &scope.TorrentID, "--torrent-id"},
		{values.TorrentControlSequence, &scope.TorrentControlSequence, "--torrent-control-sequence"},
		{values.SubjectControlSequence, &scope.SubjectControlSequence, "--subject-control-sequence"},
	} {
		if raw := strings.TrimSpace(field.raw); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 1 {
				return timeline.Scope{}, fmt.Errorf("%s must be a positive integer when provided", field.name)
			}
			*field.target = &parsed
		}
	}
	return scope, nil
}
