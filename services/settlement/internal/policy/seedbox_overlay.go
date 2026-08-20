package policy

import "fmt"

const (
	NetworkClassStandard = "standard"
	NetworkClassSeedbox  = "seedbox"
)

// NetworkEvidence is the privacy-minimized, Tracker-resolved network fact for
// one raw interval. The peer address never reaches Settlement.
type NetworkEvidence struct {
	PolicySequence           uint64
	PolicyRevision           string
	Class                    string
	RuleID                   string
	UploadFactorBasisPoints  BasisPoints
	SpeedLimitBytesPerSecond int64
}

// ApplySeedboxEvidence attaches the upload discount to every policy segment in
// the raw interval. VIP is resolved first; the legacy VIP promise therefore
// omits the box penalty without changing or erasing the underlying evidence.
func ApplySeedboxEvidence(slices []PolicySlice, evidence *NetworkEvidence) ([]PolicySlice, error) {
	if evidence == nil {
		return append([]PolicySlice(nil), slices...), nil
	}
	if evidence.PolicySequence == 0 || evidence.PolicyRevision == "" ||
		evidence.SpeedLimitBytesPerSecond < 0 || evidence.UploadFactorBasisPoints.Validate() != nil ||
		(evidence.Class != NetworkClassStandard && evidence.Class != NetworkClassSeedbox) ||
		(evidence.Class == NetworkClassStandard && (evidence.RuleID != "" || evidence.UploadFactorBasisPoints != OneX)) ||
		(evidence.Class == NetworkClassSeedbox && evidence.RuleID == "") {
		return nil, fmt.Errorf("%w: invalid Tracker network evidence", ErrInvalidRule)
	}
	result := append([]PolicySlice(nil), slices...)
	if evidence.Class != NetworkClassSeedbox {
		return result, nil
	}
	for index := range result {
		accountTier := result[index].Snapshot.Benefits.AccountTier
		if accountTier != nil && accountTier.Rule.Source == SourceVIP {
			continue
		}
		result[index].Snapshot.Seedbox = &SeedboxPenalty{
			Rule: RuleRef{
				Source:  SourceSeedbox,
				ID:      evidence.RuleID,
				Version: evidence.PolicySequence,
			},
			UploadFactor: evidence.UploadFactorBasisPoints,
		}
		if err := result[index].Snapshot.validate(); err != nil {
			return nil, fmt.Errorf("%w: apply seedbox evidence: %v", ErrInvalidRule, err)
		}
	}
	return result, nil
}
