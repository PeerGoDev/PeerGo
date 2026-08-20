package seedingreward

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultPolicyListLimit = 30
	MaximumPolicyListLimit = 100
)

type PolicyPage struct {
	Items                []PublishedPolicy
	Total                int64
	Limit                int
	Offset               int
	MinimumEffectiveFrom time.Time
}

type PreviewResult struct {
	Name                 string
	Description          string
	EligibleTorrentCount int32
	Reward               int64
	ExperienceAmount     string
	Capped               bool
}

type PolicyPreview struct {
	PolicySHA256 [32]byte
	Results      []PreviewResult
}

type AdministrationRepository interface {
	TimelineRepository
	List(context.Context, int, int) ([]PublishedPolicy, int64, error)
}

type AdministrationService struct {
	repository AdministrationRepository
	timeline   *TimelineService
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewAdministrationService(repository AdministrationRepository, authorizer authz.Authorizer, now func() time.Time) (*AdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("seeding reward administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	timeline, err := NewTimelineService(repository, now)
	if err != nil {
		return nil, err
	}
	return &AdministrationService{repository: repository, timeline: timeline, authorizer: authorizer, now: now}, nil
}

func (service *AdministrationService) List(ctx context.Context, actor authz.StaffActor, limit, offset int) (PolicyPage, error) {
	if limit < 1 || limit > MaximumPolicyListLimit || offset < 0 || offset > 1_000_000 {
		return PolicyPage{}, ErrInput
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomySeedingRewardPolicyRead, authz.SiteScope(), now, "seeding-reward-policy-administration"); err != nil {
		return PolicyPage{}, err
	}
	items, total, err := service.repository.List(ctx, limit, offset)
	if err != nil {
		return PolicyPage{}, err
	}
	return PolicyPage{Items: items, Total: total, Limit: limit, Offset: offset, MinimumEffectiveFrom: minimumEffectiveFrom(now)}, nil
}

func (service *AdministrationService) Preview(ctx context.Context, actor authz.StaffActor, policy PolicyRevision) (PolicyPreview, error) {
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomySeedingRewardPolicyRead, authz.SiteScope(), now, "seeding-reward-policy-preview"); err != nil {
		return PolicyPreview{}, err
	}
	policy.CreatedAt = now
	if policy.EffectiveFrom.Before(minimumEffectiveFrom(now)) {
		return PolicyPreview{}, ErrInput
	}
	normalized, _, err := NormalizePolicy(policy)
	if err != nil {
		return PolicyPreview{}, err
	}
	scenarios := []struct {
		name, description string
		count             int32
		sizeBytes         int64
		age               time.Duration
		seeders           int32
		uploaded          int64
		official          bool
		vip               bool
		medalBPS          int64
		levelBPS          int64
		levelCountBonus   int32
	}{
		{name: "普通做种", description: "1 个 20 GiB、30 天资源，10 个做种者", count: 1, sizeBytes: 20 << 30, age: 30 * 24 * time.Hour, seeders: 10},
		{name: "长尾保种", description: "10 个 50 GiB、1 年资源，2 个做种者", count: 10, sizeBytes: 50 << 30, age: 365 * 24 * time.Hour, seeders: 2},
		{name: "稀缺贡献", description: "25 个 80 GiB、2 年资源，含上传贡献与官方资源", count: 25, sizeBytes: 80 << 30, age: 2 * 365 * 24 * time.Hour, seeders: 1, uploaded: 1, official: true},
		{name: "最高权益", description: "稀缺贡献场景叠加 VIP、勋章和等级上限", count: 25, sizeBytes: 80 << 30, age: 2 * 365 * 24 * time.Hour, seeders: 1, uploaded: 1, official: true, vip: true, medalBPS: normalized.MaximumMedalBonusBPS, levelBPS: normalized.MaximumLevelBonusBPS, levelCountBonus: normalized.MaximumLevelTorrentBonus},
	}
	preview := PolicyPreview{PolicySHA256: normalized.SnapshotSHA256, Results: make([]PreviewResult, 0, len(scenarios))}
	for index, scenario := range scenarios {
		calculation, err := Calculate(normalized, previewInput(normalized, index, scenario.count, scenario.sizeBytes, scenario.age, scenario.seeders, scenario.uploaded, scenario.official, scenario.vip, scenario.medalBPS, scenario.levelBPS, scenario.levelCountBonus))
		if err != nil {
			return PolicyPreview{}, fmt.Errorf("preview %s: %w", scenario.name, err)
		}
		preview.Results = append(preview.Results, PreviewResult{
			Name: scenario.name, Description: scenario.description,
			EligibleTorrentCount: calculation.EligibleTorrentCount,
			Reward:               calculation.Reward, ExperienceAmount: calculation.ExperienceAmount,
			Capped: calculation.Capped,
		})
	}
	return preview, nil
}

func (service *AdministrationService) Issue(ctx context.Context, actor authz.StaffActor, policy PolicyRevision, reason string) (PublishedPolicy, error) {
	now := canonicalTime(service.now())
	if policy.EffectiveFrom.Before(minimumEffectiveFrom(now)) {
		return PublishedPolicy{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionEconomySeedingRewardPolicyIssue, authz.SiteScope(), now, "seeding-reward-policy-issue")
	if err != nil {
		return PublishedPolicy{}, err
	}
	return service.timeline.Publish(ctx, policy, actor.Subject.ID, decision.ID, reason)
}

func minimumEffectiveFrom(now time.Time) time.Time {
	// A policy must be known for more than one complete hour before it can
	// settle evidence, and the effective boundary itself is always an UTC hour.
	return now.UTC().Add(time.Hour).Truncate(time.Hour).Add(time.Hour)
}

func previewInput(policy PolicyRevision, scenario int, count int32, sizeBytes int64, age time.Duration, seeders int32, uploaded int64, official, vip bool, medalBPS, levelBPS int64, levelCountBonus int32) CalculationInput {
	windowStart := policy.EffectiveFrom
	windowEnd := windowStart.Add(WindowDuration)
	hash := func(value string) [32]byte { return sha256.Sum256([]byte(value)) }
	items := make([]ItemInput, 0, count)
	for index := int32(0); index < count; index++ {
		items = append(items, ItemInput{
			TorrentID: int64(index + 1), SizeBytes: sizeBytes,
			PublishedAt: windowEnd.Add(-age), ActiveSeconds: int64(WindowDuration / time.Second),
			RawUploadedBytes: uploaded, SnapshotSeeders: seeders, Official: official,
			TrackerEvidenceSHA256: hash(fmt.Sprintf("preview-tracker-%d-%d", scenario, index)),
			MetadataSHA256:        hash(fmt.Sprintf("preview-metadata-%d-%d", scenario, index)),
		})
	}
	return CalculationInput{
		UserID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("preview-user-%d", scenario))),
		WindowStart: windowStart, WindowEnd: windowEnd,
		WindowEvidenceSHA256: hash(fmt.Sprintf("preview-window-%d", scenario)),
		SnapshotID:           uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("preview-snapshot-%d", scenario))),
		SnapshotSequence:     int64(scenario + 1), SnapshotObservedAt: windowEnd,
		Benefits: BenefitInput{
			Revision: fmt.Sprintf("preview-%d", scenario), SnapshotSHA256: hash(fmt.Sprintf("preview-benefit-%d", scenario)),
			VIPActive: vip, MedalBonusBPS: medalBPS, LevelBonusBPS: levelBPS,
			LevelLinearTorrentBonus: levelCountBonus,
		},
		Items: items,
	}
}
