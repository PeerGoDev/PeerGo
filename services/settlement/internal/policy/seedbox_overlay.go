package policy

import "fmt"

const (
	NetworkClassStandard = "standard"
	NetworkClassSeedbox  = "seedbox"
)

// NetworkEvidence is the privacy-minimized, Tracker-resolved network fact for
// one raw interval. The peer address never reaches Settlement.
type NetworkEvidence struct {
	PolicySequence            uint64
	PolicyRevision            string
	Class                     string
	RuleID                    string
	UploadFactorBasisPoints   BasisPoints
	DownloadFactorBasisPoints BasisPoints
	SpeedLimitBytesPerSecond  int64
}

// ApplySeedboxEvidence attaches both box factors after the normal promotion and
// benefit timeline has been resolved. A free/VIP download remains free because
// multiplying zero by the box download factor is still zero.
func ApplySeedboxEvidence(slices []PolicySlice, evidence *NetworkEvidence) ([]PolicySlice, error) {
	if evidence == nil {
		return append([]PolicySlice(nil), slices...), nil
	}
	if evidence.PolicySequence == 0 || evidence.PolicyRevision == "" ||
		evidence.SpeedLimitBytesPerSecond < 0 || evidence.UploadFactorBasisPoints.Validate() != nil ||
		evidence.DownloadFactorBasisPoints.Validate() != nil ||
		(evidence.Class != NetworkClassStandard && evidence.Class != NetworkClassSeedbox) ||
		(evidence.Class == NetworkClassStandard && (evidence.RuleID != "" ||
			evidence.UploadFactorBasisPoints != OneX || evidence.DownloadFactorBasisPoints != OneX)) ||
		(evidence.Class == NetworkClassSeedbox && evidence.RuleID == "") {
		return nil, fmt.Errorf("%w: invalid Tracker network evidence", ErrInvalidRule)
	}
	result := append([]PolicySlice(nil), slices...)
	if evidence.Class != NetworkClassSeedbox {
		return result, nil
	}
	for index := range result {
		result[index].Snapshot.Seedbox = &SeedboxPenalty{
			Rule: RuleRef{
				Source:  SourceSeedbox,
				ID:      evidence.RuleID,
				Version: evidence.PolicySequence,
			},
			UploadFactor:   evidence.UploadFactorBasisPoints,
			DownloadFactor: evidence.DownloadFactorBasisPoints,
		}
		if err := result[index].Snapshot.validate(); err != nil {
			return nil, fmt.Errorf("%w: apply seedbox evidence: %v", ErrInvalidRule, err)
		}
	}
	return result, nil
}
