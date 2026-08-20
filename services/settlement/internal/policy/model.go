package policy

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidProfile      = errors.New("unknown settlement policy profile")
	ErrInvalidRule         = errors.New("invalid policy rule")
	ErrUnsupportedFeature  = errors.New("feature is not supported by the selected policy profile")
	ErrAmbiguousPromotion  = errors.New("multiple exclusive promotions are active")
	ErrInvalidPolicyWindow = errors.New("invalid policy time window")
)

// Profile fixes composition and rounding semantics for an immutable policy
// revision. PtYesV1 exists only so migrated accounting can change deliberately
// at a future effective time instead of silently changing during cutover.
type Profile string

const (
	ProfilePeerGoV1 Profile = "peergo-v1"
	ProfilePtYesV1  Profile = "ptyes-v1"
)

func (profile Profile) validate() error {
	switch profile {
	case ProfilePeerGoV1, ProfilePtYesV1:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidProfile, profile)
	}
}

// Source identifies the owning rule family. It is persisted with settlement
// evidence so an operator can explain a result without reconstructing UI text.
type Source string

const (
	SourcePolicyRevision    Source = "policy_revision"
	SourceGlobalCampaign    Source = "global_campaign"
	SourceTorrentPromotion  Source = "torrent_promotion"
	SourceCategoryPromotion Source = "category_promotion"
	SourceFeaturedTorrent   Source = "featured_torrent"
	SourceUserGroup         Source = "user_group"
	SourceVIP               Source = "vip"
	SourceDonor             Source = "donor"
	SourcePersonalFreeleech Source = "personal_freeleech"
	SourceFreeleechToken    Source = "freeleech_token"
	SourceUploader          Source = "uploader"
	SourceMedal             Source = "medal"
	SourceSeedbox           Source = "seedbox"
	SourceSpeedPenalty      Source = "speed_penalty"
)

// RuleRef is the stable provenance of one immutable rule revision.
type RuleRef struct {
	Source  Source
	ID      string
	Version uint64
}

func (reference RuleRef) validate() error {
	trimmed := strings.TrimSpace(reference.ID)
	if reference.Source == "" || trimmed == "" || trimmed != reference.ID || len(reference.ID) > 128 || reference.Version == 0 {
		return fmt.Errorf("%w: source=%q id=%q version=%d", ErrInvalidRule, reference.Source, reference.ID, reference.Version)
	}
	return nil
}

// Window uses a closed-open interval. That convention makes a rule ending at
// the exact instant its replacement starts unambiguous during replay.
type Window struct {
	StartsAt time.Time
	EndsAt   *time.Time
}

func (window Window) validate() error {
	if window.StartsAt.IsZero() {
		return fmt.Errorf("%w: missing start", ErrInvalidPolicyWindow)
	}
	if window.EndsAt != nil && !window.EndsAt.After(window.StartsAt) {
		return fmt.Errorf("%w: end must be after start", ErrInvalidPolicyWindow)
	}
	return nil
}

func (window Window) active(at time.Time) bool {
	return !at.Before(window.StartsAt) && (window.EndsAt == nil || at.Before(*window.EndsAt))
}

// FactorGrant is a favorable user entitlement. Penalties have separate types so
// a producer cannot accidentally place an adverse rule in the benefit pipeline.
type FactorGrant struct {
	Rule    RuleRef
	Factors Factors
}

func (grant FactorGrant) validate(allowedSources ...Source) error {
	if err := grant.Rule.validate(); err != nil {
		return err
	}
	if err := grant.Factors.validate(); err != nil {
		return err
	}
	if grant.Factors.Upload < OneX || grant.Factors.Download > OneX {
		return fmt.Errorf("%w: benefit must not reduce upload or increase download", ErrInvalidRule)
	}
	for _, source := range allowedSources {
		if grant.Rule.Source == source {
			return nil
		}
	}
	return fmt.Errorf("%w: source %q is not valid for this benefit", ErrInvalidRule, grant.Rule.Source)
}

// Benefits is explicit rather than a generic rule bag. Adding another economic
// privilege therefore requires a reviewed precedence decision and test.
type Benefits struct {
	Group             *FactorGrant
	AccountTier       *FactorGrant
	PersonalFreeleech *RuleRef
	FreeleechToken    *RuleRef
	Uploader          *FactorGrant
	Medal             *FactorGrant
}

// SeedboxPenalty discounts credited upload after ordinary benefits. A VIP
// exemption is resolved by the policy snapshot builder by omitting this rule.
type SeedboxPenalty struct {
	Rule         RuleRef
	UploadFactor BasisPoints
}

func (penalty SeedboxPenalty) validate() error {
	if err := penalty.Rule.validate(); err != nil {
		return err
	}
	if penalty.Rule.Source != SourceSeedbox || penalty.UploadFactor > OneX {
		return fmt.Errorf("%w: invalid seedbox penalty", ErrInvalidRule)
	}
	return penalty.UploadFactor.Validate()
}

// SpeedPenalty is an adverse override. It intentionally ignores freeleech and
// upload bonuses so a user cannot turn a sanction into another benefit.
type SpeedPenalty struct {
	Rule           RuleRef
	SuppressUpload bool
	DownloadFactor BasisPoints
}

func (penalty SpeedPenalty) validate() error {
	if err := penalty.Rule.validate(); err != nil {
		return err
	}
	if penalty.Rule.Source != SourceSpeedPenalty || penalty.DownloadFactor < OneX {
		return fmt.Errorf("%w: invalid speed penalty", ErrInvalidRule)
	}
	if !penalty.SuppressUpload && penalty.DownloadFactor == OneX {
		return fmt.Errorf("%w: speed penalty has no effect", ErrInvalidRule)
	}
	return penalty.DownloadFactor.Validate()
}
