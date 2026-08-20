package trackerruntimepolicyv1

import "net/netip"

// SeedboxClassification is a resolved policy fact. Longest-prefix selection
// makes overlapping provider ranges deterministic while the signed snapshot
// still rejects duplicate prefixes and rule identities.
type SeedboxClassification struct {
	Seedbox                  bool
	RuleID                   string
	UploadFactorBasisPoints  int
	SpeedLimitBytesPerSecond int64
}

func (policy Policy) ClassifyAddress(address netip.Addr) (SeedboxClassification, error) {
	return policy.ClassifyAddressForUser(address, 0)
}

// ClassifyAddressForUser resolves global provider ranges (UserNumericID=0)
// together with reviewed member-bound addresses. A member-bound rule never
// applies to another account, even when both clients use the same provider.
func (policy Policy) ClassifyAddressForUser(address netip.Addr, userNumericID int64) (SeedboxClassification, error) {
	normalized, err := NormalizePolicy(policy)
	if err != nil || !address.IsValid() || userNumericID < 0 {
		return SeedboxClassification{}, ErrInvalid
	}
	standard := SeedboxClassification{
		UploadFactorBasisPoints:  10_000,
		SpeedLimitBytesPerSecond: normalized.Seedbox.StandardSpeedLimitBytesPerSecond,
	}
	if !normalized.Seedbox.Enabled {
		return standard, nil
	}
	bestBits := -1
	bestUserBound := false
	result := standard
	for _, rule := range normalized.Seedbox.Rules {
		if rule.UserNumericID != 0 && rule.UserNumericID != userNumericID {
			continue
		}
		prefix, parseErr := netip.ParsePrefix(rule.CIDR)
		if parseErr != nil {
			return SeedboxClassification{}, ErrInvalid
		}
		userBound := rule.UserNumericID != 0
		if prefix.Contains(address) && (prefix.Bits() > bestBits ||
			(prefix.Bits() == bestBits && userBound && !bestUserBound)) {
			bestBits = prefix.Bits()
			bestUserBound = userBound
			result = SeedboxClassification{
				Seedbox: true, RuleID: rule.ID,
				UploadFactorBasisPoints:  normalized.Seedbox.UploadFactorBasisPoints,
				SpeedLimitBytesPerSecond: normalized.Seedbox.SeedboxSpeedLimitBytesPerSecond,
			}
		}
	}
	return result, nil
}
