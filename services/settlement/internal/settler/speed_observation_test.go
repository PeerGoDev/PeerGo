package settler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/policy"
)

func TestBuildSpeedObservationUsesImmutableThresholdAndVIPTimeline(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	raw := rawInterval{
		EventID:  uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		StartsAt: start, EndsAt: start.Add(10 * time.Second), RawUploaded: 101,
		NetworkEvidence: &policy.NetworkEvidence{SpeedLimitBytesPerSecond: 10},
	}
	ordinary := policy.PolicySlice{StartsAt: start, EndsAt: raw.EndsAt, Snapshot: testObservationSnapshot()}
	observation, err := buildSpeedObservation(raw, []policy.PolicySlice{ordinary}, start.Add(time.Minute))
	if err != nil || observation.AverageBytesPerSecond != 11 || observation.Outcome != speedOutcomeExceeded {
		t.Fatalf("ordinary observation=%+v error=%v", observation, err)
	}

	vip := ordinary
	vip.Snapshot.Benefits.AccountTier = &policy.FactorGrant{
		Rule:    policy.RuleRef{Source: policy.SourceVIP, ID: "vip", Version: 1},
		Factors: policy.Factors{Upload: policy.OneX, Download: 0},
	}
	observation, err = buildSpeedObservation(raw, []policy.PolicySlice{vip}, start.Add(time.Minute))
	if err != nil || observation.Outcome != speedOutcomeVIPExempt {
		t.Fatalf("VIP observation=%+v error=%v", observation, err)
	}
}

func TestBuildSpeedObservationSkipsDisabledThreshold(t *testing.T) {
	t.Parallel()
	observation, err := buildSpeedObservation(rawInterval{}, nil, time.Now())
	if err != nil || observation != nil {
		t.Fatalf("observation=%+v error=%v", observation, err)
	}
}

func TestBuildSpeedObservationDoesNotPersistNormalTraffic(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 18, 2, 0, 0, 0, time.UTC)
	raw := rawInterval{
		EventID:  uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		StartsAt: start, EndsAt: start.Add(10 * time.Second), RawUploaded: 100,
		NetworkEvidence: &policy.NetworkEvidence{SpeedLimitBytesPerSecond: 10},
	}
	observation, err := buildSpeedObservation(raw, []policy.PolicySlice{{
		StartsAt: start, EndsAt: raw.EndsAt, Snapshot: testObservationSnapshot(),
	}}, start.Add(time.Minute))
	if err != nil || observation != nil {
		t.Fatalf("normal observation=%+v error=%v", observation, err)
	}
}

func testObservationSnapshot() policy.Snapshot {
	return policy.Snapshot{
		Revision: policy.RuleRef{Source: policy.SourcePolicyRevision, ID: "baseline", Version: 1},
		Profile:  policy.ProfilePeerGoV1,
		Promotion: policy.ResolvedPromotion{
			Profile: policy.ProfilePeerGoV1,
			Factors: policy.Factors{Upload: policy.OneX, Download: policy.OneX},
		},
	}
}
