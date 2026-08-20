package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const MaxSnapshotBytes = 64 << 10

var ErrInvalidSnapshot = errors.New("Settlement policy snapshot is invalid")

// EncodeSnapshot is the sole persisted representation of a resolved policy.
// The writer serializes the complete result of policy compilation, not mutable
// promotion settings. Replays can consequently settle old traffic without
// consulting whatever policy happens to be configured today.
func EncodeSnapshot(snapshot Snapshot) ([]byte, error) {
	if err := snapshot.validate(); err != nil {
		return nil, ErrInvalidSnapshot
	}
	encoded, err := json.Marshal(snapshotToWire(snapshot))
	if err != nil {
		return nil, ErrInvalidSnapshot
	}
	// A final LF makes the canonical representation directly usable as an
	// operator-reviewed file while strict decoding still rejects every other
	// whitespace variation. The byte sequence, including this LF, is evidence.
	encoded = append(encoded, '\n')
	if len(encoded) < 3 || len(encoded) > MaxSnapshotBytes {
		return nil, ErrInvalidSnapshot
	}
	return encoded, nil
}

// DecodeSnapshot rejects duplicate/unknown JSON fields and requires byte-for-
// byte canonical encoding. Snapshot JSON becomes Ledger evidence, so accepting
// several encodings for one logical policy would weaken evidence digests.
func DecodeSnapshot(encoded []byte) (Snapshot, error) {
	if len(encoded) < 3 || len(encoded) > MaxSnapshotBytes {
		return Snapshot{}, ErrInvalidSnapshot
	}
	var wire snapshotWire
	if err := signedsnapshotv1.StrictJSON(encoded, &wire); err != nil {
		return Snapshot{}, ErrInvalidSnapshot
	}
	snapshot := wire.toSnapshot()
	canonical, err := EncodeSnapshot(snapshot)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return Snapshot{}, ErrInvalidSnapshot
	}
	return snapshot, nil
}

func SnapshotSHA256(snapshot Snapshot) ([sha256.Size]byte, error) {
	encoded, err := EncodeSnapshot(snapshot)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

type ruleRefWire struct {
	Source  Source `json:"source"`
	ID      string `json:"id"`
	Version uint64 `json:"version"`
}

type factorsWire struct {
	Upload   BasisPoints `json:"upload"`
	Download BasisPoints `json:"download"`
}

type factorGrantWire struct {
	Rule    ruleRefWire `json:"rule"`
	Factors factorsWire `json:"factors"`
}

type promotionMatchWire struct {
	Rule      ruleRefWire   `json:"rule"`
	Promotion PromotionType `json:"promotion"`
	Factors   factorsWire   `json:"factors"`
}

type promotionWire struct {
	Profile Profile              `json:"profile"`
	Factors factorsWire          `json:"factors"`
	Matches []promotionMatchWire `json:"matches,omitempty"`
}

type benefitsWire struct {
	Group             *factorGrantWire `json:"group,omitempty"`
	AccountTier       *factorGrantWire `json:"account_tier,omitempty"`
	PersonalFreeleech *ruleRefWire     `json:"personal_freeleech,omitempty"`
	FreeleechToken    *ruleRefWire     `json:"freeleech_token,omitempty"`
	Uploader          *factorGrantWire `json:"uploader,omitempty"`
	Medal             *factorGrantWire `json:"medal,omitempty"`
}

type seedboxWire struct {
	Rule           ruleRefWire `json:"rule"`
	UploadFactor   BasisPoints `json:"upload_factor"`
	DownloadFactor BasisPoints `json:"download_factor"`
}

type speedWire struct {
	Rule           ruleRefWire `json:"rule"`
	SuppressUpload bool        `json:"suppress_upload"`
	DownloadFactor BasisPoints `json:"download_factor"`
}

type snapshotWire struct {
	Revision  ruleRefWire   `json:"revision"`
	Profile   Profile       `json:"profile"`
	Promotion promotionWire `json:"promotion"`
	Benefits  benefitsWire  `json:"benefits"`
	Seedbox   *seedboxWire  `json:"seedbox,omitempty"`
	Speed     *speedWire    `json:"speed,omitempty"`
}

func snapshotToWire(snapshot Snapshot) snapshotWire {
	return snapshotWire{
		Revision: ruleRefToWire(snapshot.Revision), Profile: snapshot.Profile,
		Promotion: promotionToWire(snapshot.Promotion), Benefits: benefitsToWire(snapshot.Benefits),
		Seedbox: seedboxToWire(snapshot.Seedbox), Speed: speedToWire(snapshot.Speed),
	}
}

func (wire snapshotWire) toSnapshot() Snapshot {
	return Snapshot{
		Revision: wire.Revision.toRuleRef(), Profile: wire.Profile,
		Promotion: wire.Promotion.toResolvedPromotion(), Benefits: wire.Benefits.toBenefits(),
		Seedbox: wire.Seedbox.toSeedbox(), Speed: wire.Speed.toSpeed(),
	}
}

func ruleRefToWire(reference RuleRef) ruleRefWire {
	return ruleRefWire{Source: reference.Source, ID: reference.ID, Version: reference.Version}
}

func (wire ruleRefWire) toRuleRef() RuleRef {
	return RuleRef{Source: wire.Source, ID: wire.ID, Version: wire.Version}
}

func factorsToWire(factors Factors) factorsWire {
	return factorsWire{Upload: factors.Upload, Download: factors.Download}
}

func (wire factorsWire) toFactors() Factors {
	return Factors{Upload: wire.Upload, Download: wire.Download}
}

func factorGrantToWire(grant *FactorGrant) *factorGrantWire {
	if grant == nil {
		return nil
	}
	return &factorGrantWire{Rule: ruleRefToWire(grant.Rule), Factors: factorsToWire(grant.Factors)}
}

func (wire *factorGrantWire) toFactorGrant() *FactorGrant {
	if wire == nil {
		return nil
	}
	return &FactorGrant{Rule: wire.Rule.toRuleRef(), Factors: wire.Factors.toFactors()}
}

func promotionToWire(promotion ResolvedPromotion) promotionWire {
	matches := make([]promotionMatchWire, len(promotion.Matches))
	for index, match := range promotion.Matches {
		matches[index] = promotionMatchWire{Rule: ruleRefToWire(match.Rule), Promotion: match.Promotion, Factors: factorsToWire(match.Factors)}
	}
	return promotionWire{Profile: promotion.Profile, Factors: factorsToWire(promotion.Factors), Matches: matches}
}

func (wire promotionWire) toResolvedPromotion() ResolvedPromotion {
	matches := make([]PromotionMatch, len(wire.Matches))
	for index, match := range wire.Matches {
		matches[index] = PromotionMatch{Rule: match.Rule.toRuleRef(), Promotion: match.Promotion, Factors: match.Factors.toFactors()}
	}
	return ResolvedPromotion{Profile: wire.Profile, Factors: wire.Factors.toFactors(), Matches: matches}
}

func benefitsToWire(benefits Benefits) benefitsWire {
	return benefitsWire{
		Group: factorGrantToWire(benefits.Group), AccountTier: factorGrantToWire(benefits.AccountTier),
		PersonalFreeleech: ruleRefToWirePointer(benefits.PersonalFreeleech), FreeleechToken: ruleRefToWirePointer(benefits.FreeleechToken),
		Uploader: factorGrantToWire(benefits.Uploader), Medal: factorGrantToWire(benefits.Medal),
	}
}

func (wire benefitsWire) toBenefits() Benefits {
	return Benefits{
		Group: wire.Group.toFactorGrant(), AccountTier: wire.AccountTier.toFactorGrant(),
		PersonalFreeleech: wire.PersonalFreeleech.toRuleRefPointer(), FreeleechToken: wire.FreeleechToken.toRuleRefPointer(),
		Uploader: wire.Uploader.toFactorGrant(), Medal: wire.Medal.toFactorGrant(),
	}
}

func ruleRefToWirePointer(reference *RuleRef) *ruleRefWire {
	if reference == nil {
		return nil
	}
	result := ruleRefToWire(*reference)
	return &result
}

func (wire *ruleRefWire) toRuleRefPointer() *RuleRef {
	if wire == nil {
		return nil
	}
	result := wire.toRuleRef()
	return &result
}

func seedboxToWire(seedbox *SeedboxPenalty) *seedboxWire {
	if seedbox == nil {
		return nil
	}
	return &seedboxWire{
		Rule: ruleRefToWire(seedbox.Rule), UploadFactor: seedbox.UploadFactor,
		DownloadFactor: seedbox.DownloadFactor,
	}
}

func (wire *seedboxWire) toSeedbox() *SeedboxPenalty {
	if wire == nil {
		return nil
	}
	return &SeedboxPenalty{
		Rule: wire.Rule.toRuleRef(), UploadFactor: wire.UploadFactor,
		DownloadFactor: wire.DownloadFactor,
	}
}

func speedToWire(speed *SpeedPenalty) *speedWire {
	if speed == nil {
		return nil
	}
	return &speedWire{Rule: ruleRefToWire(speed.Rule), SuppressUpload: speed.SuppressUpload, DownloadFactor: speed.DownloadFactor}
}

func (wire *speedWire) toSpeed() *SpeedPenalty {
	if wire == nil {
		return nil
	}
	return &SpeedPenalty{Rule: wire.Rule.toRuleRef(), SuppressUpload: wire.SuppressUpload, DownloadFactor: wire.DownloadFactor}
}
