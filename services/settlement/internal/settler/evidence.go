package settler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/settlement/internal/policy"
)

type rawInterval struct {
	EventID                uuid.UUID
	UserID                 uuid.UUID
	TorrentID              int64
	StartsAt               time.Time
	EndsAt                 time.Time
	RawUploaded            int64
	RawDownloaded          int64
	TorrentControlSequence int64
	SubjectControlSequence int64
	NetworkEvidence        *policy.NetworkEvidence
}

type finalizedSettlement struct {
	Raw              rawInterval
	SettledAt        time.Time
	Result           policy.IntervalResult
	Segments         []finalizedSegment
	SettlementSHA256 [sha256.Size]byte
	TrafficEvent     settlementtrafficv1.Event
	TrafficPayload   []byte
	TrafficSHA256    [sha256.Size]byte
}

type finalizedSegment struct {
	StartsAt             time.Time
	EndsAt               time.Time
	PolicyRevision       policy.RuleRef
	Profile              policy.Profile
	PolicySnapshotSHA256 [sha256.Size]byte
	ApplicationsJSON     []byte
	ApplicationsSHA256   [sha256.Size]byte
	RawUploaded          int64
	RawDownloaded        int64
	CreditedUploaded     int64
	ChargedDownloaded    int64
}

type ruleEvidence struct {
	Source  policy.Source `json:"source"`
	ID      string        `json:"id"`
	Version uint64        `json:"version"`
}

type factorsEvidence struct {
	Upload   policy.BasisPoints `json:"upload"`
	Download policy.BasisPoints `json:"download"`
}

type applicationEvidence struct {
	Rule      ruleEvidence     `json:"rule"`
	Operation policy.Operation `json:"operation"`
	Factors   factorsEvidence  `json:"factors"`
}

type segmentEvidence struct {
	StartsAt             time.Time      `json:"starts_at"`
	EndsAt               time.Time      `json:"ends_at"`
	PolicyRevision       ruleEvidence   `json:"policy_revision"`
	Profile              policy.Profile `json:"profile"`
	PolicySnapshotSHA256 string         `json:"policy_snapshot_sha256"`
	ApplicationsSHA256   string         `json:"applications_sha256"`
	RawUploaded          int64          `json:"raw_uploaded"`
	RawDownloaded        int64          `json:"raw_downloaded"`
	CreditedUploaded     int64          `json:"credited_uploaded"`
	ChargedDownloaded    int64          `json:"charged_downloaded"`
}

type settlementEvidence struct {
	SettlementID           string            `json:"settlement_id"`
	UserID                 string            `json:"user_id"`
	TorrentID              int64             `json:"torrent_id"`
	TorrentControlSequence int64             `json:"torrent_control_sequence"`
	SubjectControlSequence int64             `json:"subject_control_sequence"`
	IntervalStartsAt       time.Time         `json:"interval_starts_at"`
	IntervalEndsAt         time.Time         `json:"interval_ends_at"`
	RawUploaded            int64             `json:"raw_uploaded"`
	RawDownloaded          int64             `json:"raw_downloaded"`
	CreditedUploaded       int64             `json:"credited_uploaded"`
	ChargedDownloaded      int64             `json:"charged_downloaded"`
	Segments               []segmentEvidence `json:"segments"`
}

func finalize(raw rawInterval, slices []policy.PolicySlice, result policy.IntervalResult, settledAt time.Time) (finalizedSettlement, error) {
	if raw.EventID == uuid.Nil || raw.UserID == uuid.Nil || raw.TorrentID < 1 || raw.StartsAt.IsZero() ||
		!raw.EndsAt.After(raw.StartsAt) || raw.RawUploaded < 0 || raw.RawDownloaded < 0 ||
		raw.TorrentControlSequence < 1 || raw.SubjectControlSequence < 1 || len(slices) == 0 || len(slices) != len(result.Segments) {
		return finalizedSettlement{}, ErrInvariant
	}
	if settledAt.Before(raw.EndsAt) {
		settledAt = raw.EndsAt
	}
	settledAt = settledAt.UTC().Round(0)
	segments := make([]finalizedSegment, len(result.Segments))
	evidenceSegments := make([]segmentEvidence, len(result.Segments))
	for index, resultSegment := range result.Segments {
		if !resultSegment.StartsAt.Equal(slices[index].StartsAt) || !resultSegment.EndsAt.Equal(slices[index].EndsAt) {
			return finalizedSettlement{}, ErrInvariant
		}
		applicationsJSON, applicationsDigest, err := applicationsEvidence(resultSegment.Result.Applications)
		if err != nil {
			return finalizedSettlement{}, err
		}
		snapshotDigest, err := policy.SnapshotSHA256(slices[index].Snapshot)
		if err != nil {
			return finalizedSettlement{}, fmt.Errorf("encode policy snapshot evidence: %w", err)
		}
		rawUploaded, err := int64Value(resultSegment.Result.RawUploaded)
		if err != nil {
			return finalizedSettlement{}, err
		}
		rawDownloaded, err := int64Value(resultSegment.Result.RawDownloaded)
		if err != nil {
			return finalizedSettlement{}, err
		}
		creditedUploaded, err := int64Value(resultSegment.Result.CreditedUploaded)
		if err != nil {
			return finalizedSettlement{}, err
		}
		chargedDownloaded, err := int64Value(resultSegment.Result.ChargedDownloaded)
		if err != nil {
			return finalizedSettlement{}, err
		}
		segments[index] = finalizedSegment{
			StartsAt: resultSegment.StartsAt.UTC().Round(0), EndsAt: resultSegment.EndsAt.UTC().Round(0),
			PolicyRevision: resultSegment.Result.PolicyRevision, Profile: resultSegment.Result.Profile,
			PolicySnapshotSHA256: snapshotDigest, ApplicationsJSON: applicationsJSON, ApplicationsSHA256: applicationsDigest,
			RawUploaded: rawUploaded, RawDownloaded: rawDownloaded, CreditedUploaded: creditedUploaded, ChargedDownloaded: chargedDownloaded,
		}
		evidenceSegments[index] = segmentEvidence{
			StartsAt: segments[index].StartsAt, EndsAt: segments[index].EndsAt,
			PolicyRevision: ruleToEvidence(segments[index].PolicyRevision), Profile: segments[index].Profile,
			PolicySnapshotSHA256: hex.EncodeToString(snapshotDigest[:]), ApplicationsSHA256: hex.EncodeToString(applicationsDigest[:]),
			RawUploaded: rawUploaded, RawDownloaded: rawDownloaded, CreditedUploaded: creditedUploaded, ChargedDownloaded: chargedDownloaded,
		}
	}
	creditedUploaded, err := int64Value(result.CreditedUploaded)
	if err != nil {
		return finalizedSettlement{}, err
	}
	chargedDownloaded, err := int64Value(result.ChargedDownloaded)
	if err != nil {
		return finalizedSettlement{}, err
	}
	evidence := settlementEvidence{
		SettlementID: raw.EventID.String(), UserID: raw.UserID.String(), TorrentID: raw.TorrentID,
		TorrentControlSequence: raw.TorrentControlSequence, SubjectControlSequence: raw.SubjectControlSequence,
		IntervalStartsAt: raw.StartsAt.UTC().Round(0), IntervalEndsAt: raw.EndsAt.UTC().Round(0),
		RawUploaded: raw.RawUploaded, RawDownloaded: raw.RawDownloaded,
		CreditedUploaded: creditedUploaded, ChargedDownloaded: chargedDownloaded, Segments: evidenceSegments,
	}
	evidenceBytes, err := json.Marshal(evidence)
	if err != nil {
		return finalizedSettlement{}, fmt.Errorf("encode settlement evidence: %w", err)
	}
	evidenceDigest := sha256.Sum256(evidenceBytes)
	explanation, err := publicExplanation(segments)
	if err != nil {
		return finalizedSettlement{}, err
	}
	trafficEvent := settlementtrafficv1.Event{
		SchemaVersion: settlementtrafficv1.SchemaVersion, EventID: raw.EventID.String(), OccurredAt: settledAt,
		UserID: raw.UserID.String(), TorrentID: raw.TorrentID,
		IntervalStartsAt: raw.StartsAt.UTC().Round(0), IntervalEndsAt: raw.EndsAt.UTC().Round(0),
		RawUploaded: raw.RawUploaded, RawDownloaded: raw.RawDownloaded,
		CreditedUploaded: creditedUploaded, ChargedDownloaded: chargedDownloaded,
		SettlementSHA256: hex.EncodeToString(evidenceDigest[:]), Explanation: explanation,
	}
	payload, err := settlementtrafficv1.Encode(trafficEvent)
	if err != nil {
		return finalizedSettlement{}, fmt.Errorf("encode Core traffic projection event: %w", err)
	}
	return finalizedSettlement{
		Raw: raw, SettledAt: settledAt, Result: result, Segments: segments, SettlementSHA256: evidenceDigest,
		TrafficEvent: trafficEvent, TrafficPayload: payload, TrafficSHA256: sha256.Sum256(payload),
	}, nil
}

// publicExplanation deliberately projects only times and reconciled byte
// values. Internal rule IDs, profiles, applications and evidence digests stay
// in Tracker Ledger. Excessive fragmentation is represented explicitly so a
// display concern can never prevent the immutable settlement from committing.
func publicExplanation(segments []finalizedSegment) (*settlementtrafficv1.Explanation, error) {
	if len(segments) == 0 || len(segments) > math.MaxInt32 {
		return nil, ErrInvariant
	}
	result := &settlementtrafficv1.Explanation{SegmentCount: int32(len(segments))}
	if len(segments) > settlementtrafficv1.MaxExplanationSegments {
		result.Status = settlementtrafficv1.ExplanationOmitted
		return result, nil
	}
	result.Status = settlementtrafficv1.ExplanationComplete
	result.Segments = make([]settlementtrafficv1.Segment, len(segments))
	for index, segment := range segments {
		result.Segments[index] = settlementtrafficv1.Segment{
			StartsAt: segment.StartsAt, EndsAt: segment.EndsAt,
			RawUploaded: segment.RawUploaded, RawDownloaded: segment.RawDownloaded,
			CreditedUploaded: segment.CreditedUploaded, ChargedDownloaded: segment.ChargedDownloaded,
		}
	}
	return result, nil
}

func applicationsEvidence(applications []policy.Application) ([]byte, [sha256.Size]byte, error) {
	items := make([]applicationEvidence, len(applications))
	for index, application := range applications {
		items[index] = applicationEvidence{
			Rule: ruleToEvidence(application.Rule), Operation: application.Operation,
			Factors: factorsEvidence{Upload: application.Factors.Upload, Download: application.Factors.Download},
		}
	}
	encoded, err := json.Marshal(items)
	if err != nil || len(encoded) < 2 {
		return nil, [sha256.Size]byte{}, ErrInvariant
	}
	return encoded, sha256.Sum256(encoded), nil
}

func ruleToEvidence(rule policy.RuleRef) ruleEvidence {
	return ruleEvidence{Source: rule.Source, ID: rule.ID, Version: rule.Version}
}

func int64Value(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("%w: policy result exceeds PostgreSQL bigint", ErrInvariant)
	}
	return int64(value), nil
}
