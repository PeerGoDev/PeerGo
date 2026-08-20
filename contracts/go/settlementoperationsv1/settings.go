// Package settlementoperationsv1 defines the deployment-safe policy summary
// shared by Settlement and Core. It never exposes raw announce evidence.
package settlementoperationsv1

import "time"

type HNRPolicy struct {
	Configured               bool       `json:"configured"`
	RevisionID               string     `json:"revision_id"`
	EffectiveAt              *time.Time `json:"effective_at"`
	RuleID                   string     `json:"rule_id"`
	RuleVersion              int64      `json:"rule_version"`
	Mode                     string     `json:"mode"`
	RequiredSeedSeconds      int64      `json:"required_seed_seconds"`
	RequiredRatioBasisPoints int64      `json:"required_ratio_basis_points"`
	AssessmentWindowSeconds  int64      `json:"assessment_window_seconds"`
	GracePeriodSeconds       int64      `json:"grace_period_seconds"`
	MaxIntervalCreditSeconds int64      `json:"max_interval_credit_seconds"`
}

type SeedboxPolicy struct {
	SettlementPrimitiveSupported bool  `json:"settlement_primitive_supported"`
	GlobalPolicyConfigured       bool  `json:"global_policy_configured"`
	UploadFactorBasisPoints      int64 `json:"upload_factor_basis_points"`
	DownloadFactorBasisPoints    int64 `json:"download_factor_basis_points"`
	ClassificationConnected      bool  `json:"classification_connected"`
	RegistryConnected            bool  `json:"registry_connected"`
	SpeedObservationConnected    bool  `json:"speed_observation_connected"`
}

type Settings struct {
	GeneratedAt               time.Time     `json:"generated_at"`
	HNR                       HNRPolicy     `json:"hnr"`
	Seedbox                   SeedboxPolicy `json:"seedbox"`
	GlobalRatioWatchConnected bool          `json:"global_ratio_watch_connected"`
}

func (settings Settings) Valid() bool {
	if settings.GeneratedAt.IsZero() || !settings.Seedbox.SettlementPrimitiveSupported ||
		settings.Seedbox.UploadFactorBasisPoints < 0 || settings.Seedbox.UploadFactorBasisPoints > 10_000 ||
		settings.Seedbox.DownloadFactorBasisPoints < 10_000 || settings.Seedbox.DownloadFactorBasisPoints > 100_000 {
		return false
	}
	if !settings.HNR.Configured {
		return settings.HNR.RevisionID == "" && settings.HNR.EffectiveAt == nil && settings.HNR.Mode == ""
	}
	return settings.HNR.RevisionID != "" && settings.HNR.EffectiveAt != nil && !settings.HNR.EffectiveAt.IsZero() &&
		settings.HNR.RuleID != "" && settings.HNR.RuleVersion >= 1 &&
		(settings.HNR.Mode == "disabled" || settings.HNR.Mode == "exempt" || settings.HNR.Mode == "enforced") &&
		settings.HNR.RequiredSeedSeconds >= 0 && settings.HNR.RequiredRatioBasisPoints >= 0 &&
		settings.HNR.AssessmentWindowSeconds >= 0 && settings.HNR.GracePeriodSeconds >= 0 &&
		settings.HNR.MaxIntervalCreditSeconds >= 0
}
