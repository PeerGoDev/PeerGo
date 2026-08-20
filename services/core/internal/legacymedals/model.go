package legacymedals

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
)

const (
	ptCoinToMagicRate int64 = 5000
	basisPoints       int64 = 10000

	definitionFingerprintDomain = "peergo:migration:ptyes-medal-definition:v1\x00"
	holdingFingerprintDomain    = "peergo:migration:ptyes-user-medal:v1\x00"
	settingsFingerprintDomain   = "peergo:migration:ptyes-medal-settings:v1\x00"
	benefitFingerprintDomain    = "peergo:migration:ptyes-medal-benefit:v1\x00"
	workgroupFingerprintDomain  = "peergo:migration:ptyes-workgroup-membership:v1\x00"
	workgroupMembershipDomain   = "peergo:migration:ptyes-workgroup-membership-id:v1\x00"
	workgroupTransitionDomain   = "peergo:migration:ptyes-workgroup-transition-id:v1\x00"
)

type sourceMedal struct {
	LegacyID            int64
	Name                string
	Description         *string
	ImageLarge          *string
	ImageSmall          *string
	GetType             int64
	PriceText           string
	DurationDays        int64
	DisplayOnPage       bool
	Priority            int64
	UploadBonusText     string
	DownloadBonusText   string
	MagicBonusText      string
	InviteBonus         int64
	IsWorkgroup         bool
	ConditionsRaw       *string
	PrivilegesRaw       *string
	PoolEligible        bool
	RewardMagicText     string
	RewardCreditsText   string
	RewardCycle         *string
	SaleBeginAt         pgtype.Timestamptz
	SaleEndAt           pgtype.Timestamptz
	Inventory           pgtype.Int8
	CreatedAt           pgtype.Timestamptz
	UpdatedAt           pgtype.Timestamptz
	AcquisitionMethod   string
	Price               int64
	UploadBonusBPS      int64
	DownloadBonusBPS    int64
	MagicBonusBPS       int64
	ConditionsJSON      string
	PrivilegesJSON      string
	PeriodicRewardMagic int64
}

func (row *sourceMedal) normalize(position int) error {
	row.Name = strings.TrimSpace(row.Name)
	if row.LegacyID <= 0 || row.Name == "" || len(row.Name) > 100 {
		return fmt.Errorf("PtYes medal row %d has an invalid identity", position)
	}
	var ok bool
	row.AcquisitionMethod, ok = mapAcquisitionMethod(row.GetType)
	if !ok {
		return fmt.Errorf("PtYes medal %d has unsupported get_type %d", row.LegacyID, row.GetType)
	}
	if row.DurationDays < 0 || row.InviteBonus < 0 {
		return fmt.Errorf("PtYes medal %d has a negative duration or invite bonus", row.LegacyID)
	}
	var err error
	row.Price, err = roundedNonNegative(row.PriceText)
	if err != nil {
		return fmt.Errorf("PtYes medal %d price: %w", row.LegacyID, err)
	}
	row.UploadBonusBPS, err = decimalToBPS(row.UploadBonusText)
	if err != nil {
		return fmt.Errorf("PtYes medal %d upload bonus: %w", row.LegacyID, err)
	}
	row.DownloadBonusBPS, err = decimalToBPS(row.DownloadBonusText)
	if err != nil {
		return fmt.Errorf("PtYes medal %d download discount: %w", row.LegacyID, err)
	}
	row.MagicBonusBPS, err = decimalToBPS(row.MagicBonusText)
	if err != nil {
		return fmt.Errorf("PtYes medal %d magic bonus: %w", row.LegacyID, err)
	}
	rewardMagic, err := nonNegativeRat(row.RewardMagicText)
	if err != nil {
		return fmt.Errorf("PtYes medal %d periodic magic reward: %w", row.LegacyID, err)
	}
	rewardCredits, err := nonNegativeRat(row.RewardCreditsText)
	if err != nil {
		return fmt.Errorf("PtYes medal %d periodic PT reward: %w", row.LegacyID, err)
	}
	rewardMagic.Add(rewardMagic, new(big.Rat).Mul(rewardCredits, big.NewRat(ptCoinToMagicRate, 1)))
	row.PeriodicRewardMagic, err = roundNonNegativeRat(rewardMagic)
	if err != nil {
		return fmt.Errorf("PtYes medal %d unified periodic reward: %w", row.LegacyID, err)
	}
	row.ConditionsJSON, err = normalizeJSONArray(row.ConditionsRaw)
	if err != nil {
		return fmt.Errorf("PtYes medal %d conditions: %w", row.LegacyID, err)
	}
	row.PrivilegesJSON, err = normalizeJSONArray(row.PrivilegesRaw)
	if err != nil {
		return fmt.Errorf("PtYes medal %d privileges: %w", row.LegacyID, err)
	}
	row.Description = normalizedOptional(row.Description)
	row.ImageLarge, err = normalizeImagePath(row.ImageLarge)
	if err != nil {
		return fmt.Errorf("PtYes medal %d large image: %w", row.LegacyID, err)
	}
	row.ImageSmall, err = normalizeImagePath(row.ImageSmall)
	if err != nil {
		return fmt.Errorf("PtYes medal %d small image: %w", row.LegacyID, err)
	}
	row.RewardCycle = normalizedOptional(row.RewardCycle)
	if row.RewardCycle != nil {
		value := strings.ToLower(*row.RewardCycle)
		row.RewardCycle = &value
		for _, character := range value {
			if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' {
				return fmt.Errorf("PtYes medal %d has an invalid reward cycle", row.LegacyID)
			}
		}
		if len(value) > 20 {
			return fmt.Errorf("PtYes medal %d has an invalid reward cycle", row.LegacyID)
		}
	}
	if row.Inventory.Valid && row.Inventory.Int64 < 0 {
		return fmt.Errorf("PtYes medal %d has negative inventory", row.LegacyID)
	}
	if row.SaleBeginAt.Valid && row.SaleEndAt.Valid && !row.SaleBeginAt.Time.Before(row.SaleEndAt.Time) {
		return fmt.Errorf("PtYes medal %d has an invalid sale window", row.LegacyID)
	}
	return nil
}

func (row sourceMedal) fingerprint() [32]byte {
	payload, _ := json.Marshal(struct {
		Domain string
		Row    sourceMedalFingerprint
	}{definitionFingerprintDomain, row.fingerprintPayload()})
	return sha256.Sum256(payload)
}

type sourceMedalFingerprint struct {
	LegacyID, GetType, DurationDays, Priority, InviteBonus                   int64
	Name, Price, Upload, Download, Magic, RewardMagic, RewardCredits         string
	Description, ImageLarge, ImageSmall, Conditions, Privileges, RewardCycle *string
	DisplayOnPage, IsWorkgroup, PoolEligible                                 bool
	SaleBeginAt, SaleEndAt, CreatedAt, UpdatedAt                             *time.Time
	Inventory                                                                *int64
}

func (row sourceMedal) fingerprintPayload() sourceMedalFingerprint {
	return sourceMedalFingerprint{
		LegacyID: row.LegacyID, GetType: row.GetType, DurationDays: row.DurationDays,
		Priority: row.Priority, InviteBonus: row.InviteBonus, Name: row.Name,
		Price: row.PriceText, Upload: row.UploadBonusText, Download: row.DownloadBonusText,
		Magic: row.MagicBonusText, RewardMagic: row.RewardMagicText, RewardCredits: row.RewardCreditsText,
		Description: row.Description, ImageLarge: row.ImageLarge, ImageSmall: row.ImageSmall,
		Conditions: row.ConditionsRaw, Privileges: row.PrivilegesRaw, RewardCycle: row.RewardCycle,
		DisplayOnPage: row.DisplayOnPage, IsWorkgroup: row.IsWorkgroup, PoolEligible: row.PoolEligible,
		SaleBeginAt: timePointer(row.SaleBeginAt), SaleEndAt: timePointer(row.SaleEndAt),
		CreatedAt: timePointer(row.CreatedAt), UpdatedAt: timePointer(row.UpdatedAt),
		Inventory: intPointer(row.Inventory),
	}
}

type sourceHolding struct {
	LegacyID        int64
	LegacyUserID    int64
	LegacyMedalID   int64
	Status          int64
	Priority        int64
	ExpiresAt       pgtype.Timestamptz
	LegacyGrantedBy pgtype.Int8
	Note            *string
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
	LastRewardAt    pgtype.Timestamptz
}

func (row *sourceHolding) normalize(position int) error {
	if row.LegacyID <= 0 || row.LegacyUserID <= 0 || row.LegacyMedalID <= 0 || (row.Status != 1 && row.Status != 2) {
		return fmt.Errorf("PtYes user medal row %d has invalid identity or state", position)
	}
	if row.LegacyGrantedBy.Valid && row.LegacyGrantedBy.Int64 == 0 {
		row.LegacyGrantedBy = pgtype.Int8{}
	}
	if row.LegacyGrantedBy.Valid && row.LegacyGrantedBy.Int64 < 0 {
		return fmt.Errorf("PtYes user medal %d has an invalid granter", row.LegacyID)
	}
	row.Note = normalizedOptional(row.Note)
	if row.Note != nil && len(*row.Note) > 255 {
		return fmt.Errorf("PtYes user medal %d note is too long", row.LegacyID)
	}
	return nil
}

func (row sourceHolding) fingerprint() [32]byte {
	payload, _ := json.Marshal(struct {
		Domain                                        string
		ID, UserID, MedalID, Status, Priority         int64
		ExpiresAt, CreatedAt, UpdatedAt, LastRewardAt *time.Time
		GrantedBy                                     *int64
		Note                                          *string
	}{
		Domain: holdingFingerprintDomain, ID: row.LegacyID, UserID: row.LegacyUserID,
		MedalID: row.LegacyMedalID, Status: row.Status, Priority: row.Priority,
		ExpiresAt: timePointer(row.ExpiresAt), CreatedAt: timePointer(row.CreatedAt),
		UpdatedAt: timePointer(row.UpdatedAt), LastRewardAt: timePointer(row.LastRewardAt),
		GrantedBy: intPointer(row.LegacyGrantedBy), Note: row.Note,
	})
	return sha256.Sum256(payload)
}

type sourceSettings struct {
	Enabled                    bool
	MaximumWearCount           int64
	MaximumUploadBonusBPS      int64
	MaximumDownloadDiscountBPS int64
	MaximumMagicBonusBPS       int64
	MaximumInviteBonus         int64
	ConditionCheckDay          int64
	ConditionWarningDays       int64
}

func (settings sourceSettings) fingerprint() [32]byte {
	payload, _ := json.Marshal(struct {
		Domain   string
		Settings sourceSettings
	}{settingsFingerprintDomain, settings})
	return sha256.Sum256(payload)
}

type sourceBenefit struct {
	LegacyUserID             int64
	ActiveContributingMedals int64
	UncappedMagicBonusBPS    int64
	MagicBonusBPS            int64
}

type sourceWorkgroupMembership struct {
	LegacyUserID       int64
	UserID             uuid.UUID
	GroupKind          string
	MembershipID       uuid.UUID
	TransitionID       uuid.UUID
	StartedAt          time.Time
	LegacyUserMedalIDs []int64
	LegacyMedalIDs     []int64
	CommandJSON        *string
	CommandSHA256      []byte
}

func (membership sourceWorkgroupMembership) fingerprint(occurredAt time.Time) [32]byte {
	payload, _ := json.Marshal(struct {
		Domain             string
		LegacyUserID       int64
		UserID             uuid.UUID
		GroupKind          string
		MembershipID       uuid.UUID
		TransitionID       uuid.UUID
		StartedAt          string
		LegacyUserMedalIDs []int64
		LegacyMedalIDs     []int64
		OccurredAt         string
	}{
		Domain: workgroupFingerprintDomain, LegacyUserID: membership.LegacyUserID,
		UserID: membership.UserID, GroupKind: membership.GroupKind,
		MembershipID: membership.MembershipID, TransitionID: membership.TransitionID,
		StartedAt:          membership.StartedAt.UTC().Format(time.RFC3339Nano),
		LegacyUserMedalIDs: membership.LegacyUserMedalIDs, LegacyMedalIDs: membership.LegacyMedalIDs,
		OccurredAt: occurredAt.UTC().Format(time.RFC3339Nano),
	})
	return sha256.Sum256(payload)
}

type workgroupMembershipKey struct {
	LegacyUserID int64
	GroupKind    string
}

type workgroupMembershipOrigin struct {
	LegacyUserMedalID int64
	LegacyMedalID     int64
	StartedAt         time.Time
}

// calculateWorkgroupMemberships converts only the closed Rousi workgroup
// medal vocabulary. Unknown workgroup medals stop the cutover so a new name
// can never silently grant a PeerGo entitlement.
func calculateWorkgroupMemberships(
	medals []sourceMedal,
	holdings []sourceHolding,
	userIDs map[int64]uuid.UUID,
	occurredAt time.Time,
) ([]sourceWorkgroupMembership, error) {
	definitions := make(map[int64]sourceMedal, len(medals))
	kinds := make(map[int64]string)
	for _, medal := range medals {
		if _, exists := definitions[medal.LegacyID]; exists {
			return nil, fmt.Errorf("duplicate PtYes medal ID %d", medal.LegacyID)
		}
		definitions[medal.LegacyID] = medal
		if !medal.IsWorkgroup {
			continue
		}
		kind, ok := legacyWorkgroupKind(medal.Name)
		if !ok {
			return nil, fmt.Errorf("PtYes workgroup medal %d %q has no reviewed PeerGo mapping", medal.LegacyID, medal.Name)
		}
		kinds[medal.LegacyID] = kind
	}

	origins := make(map[workgroupMembershipKey][]workgroupMembershipOrigin)
	for _, holding := range holdings {
		medal, exists := definitions[holding.LegacyMedalID]
		if !exists {
			return nil, fmt.Errorf("PtYes user medal %d references missing medal %d", holding.LegacyID, holding.LegacyMedalID)
		}
		if !medal.IsWorkgroup || (holding.ExpiresAt.Valid && !holding.ExpiresAt.Time.After(occurredAt)) {
			continue
		}
		if _, exists := userIDs[holding.LegacyUserID]; !exists {
			return nil, fmt.Errorf("PtYes workgroup medal %d references unmapped user %d", holding.LegacyID, holding.LegacyUserID)
		}
		startedAt := occurredAt
		if holding.CreatedAt.Valid {
			startedAt = holding.CreatedAt.Time.UTC().Truncate(time.Microsecond)
			if startedAt.After(occurredAt) {
				return nil, fmt.Errorf("PtYes user medal %d was created after the cutover", holding.LegacyID)
			}
		}
		key := workgroupMembershipKey{LegacyUserID: holding.LegacyUserID, GroupKind: kinds[holding.LegacyMedalID]}
		origins[key] = append(origins[key], workgroupMembershipOrigin{
			LegacyUserMedalID: holding.LegacyID,
			LegacyMedalID:     holding.LegacyMedalID,
			StartedAt:         startedAt,
		})
	}

	keys := make([]workgroupMembershipKey, 0, len(origins))
	for key := range origins {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].LegacyUserID != keys[right].LegacyUserID {
			return keys[left].LegacyUserID < keys[right].LegacyUserID
		}
		return keys[left].GroupKind < keys[right].GroupKind
	})

	result := make([]sourceWorkgroupMembership, 0, len(keys))
	for _, key := range keys {
		groupOrigins := origins[key]
		sort.Slice(groupOrigins, func(left, right int) bool {
			return groupOrigins[left].LegacyUserMedalID < groupOrigins[right].LegacyUserMedalID
		})
		identity := fmt.Sprintf("ptyes:%d:%s", key.LegacyUserID, key.GroupKind)
		membership := sourceWorkgroupMembership{
			LegacyUserID: key.LegacyUserID,
			UserID:       userIDs[key.LegacyUserID],
			GroupKind:    key.GroupKind,
			MembershipID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(workgroupMembershipDomain+identity)),
			TransitionID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(workgroupTransitionDomain+identity)),
			StartedAt:    groupOrigins[0].StartedAt,
		}
		for _, origin := range groupOrigins {
			membership.LegacyUserMedalIDs = append(membership.LegacyUserMedalIDs, origin.LegacyUserMedalID)
			membership.LegacyMedalIDs = append(membership.LegacyMedalIDs, origin.LegacyMedalID)
			if origin.StartedAt.Before(membership.StartedAt) {
				membership.StartedAt = origin.StartedAt
			}
		}
		if membership.GroupKind == workgroupbenefitv1.GroupRetention {
			command := workgroupbenefitv1.Command{
				SchemaVersion: workgroupbenefitv1.SchemaVersion,
				TransitionID:  membership.TransitionID.String(),
				UserID:        membership.UserID.String(),
				GroupKind:     workgroupbenefitv1.GroupRetention,
				Entitlement:   workgroupbenefitv1.EntitlementDownloadChargeExempt,
				Active:        true,
				StateVersion:  1,
				EffectiveAt:   occurredAt.UTC().Round(0),
			}
			encoded, err := workgroupbenefitv1.Encode(command)
			if err != nil {
				return nil, fmt.Errorf("encode migrated retention benefit for user %d: %w", membership.LegacyUserID, err)
			}
			digest, err := workgroupbenefitv1.SHA256(encoded)
			if err != nil {
				return nil, fmt.Errorf("digest migrated retention benefit for user %d: %w", membership.LegacyUserID, err)
			}
			commandJSON := string(encoded)
			membership.CommandJSON = &commandJSON
			membership.CommandSHA256 = append([]byte(nil), digest[:]...)
		}
		result = append(result, membership)
	}
	return result, nil
}

func legacyWorkgroupKind(name string) (string, bool) {
	switch strings.TrimSpace(name) {
	case "转种组", "官种组":
		return "reseed", true
	case "种审组":
		return "review", true
	case "保种组":
		return "retention", true
	default:
		return "", false
	}
}

func (benefit sourceBenefit) fingerprint(occurredAt time.Time) [32]byte {
	payload, _ := json.Marshal(struct {
		Domain  string
		At      string
		Benefit sourceBenefit
	}{benefitFingerprintDomain, occurredAt.UTC().Format(time.RFC3339Nano), benefit})
	return sha256.Sum256(payload)
}

func mapAcquisitionMethod(value int64) (string, bool) {
	methods := map[int64]string{1: "purchase", 2: "grant", 3: "sponsor", 4: "workgroup", 5: "developer"}
	method, ok := methods[value]
	return method, ok
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func normalizeImagePath(value *string) (*string, error) {
	value = normalizedOptional(value)
	if value == nil {
		return nil, nil
	}
	if !strings.HasPrefix(*value, "/medal/") || path.Clean(*value) != *value || strings.Contains(*value, "..") {
		return nil, errors.New("path must be a canonical /medal/ asset")
	}
	return value, nil
}

func normalizeJSONArray(value *string) (string, error) {
	value = normalizedOptional(value)
	if value == nil {
		return "[]", nil
	}
	var array []any
	if err := json.Unmarshal([]byte(*value), &array); err != nil {
		return "", err
	}
	normalized, err := json.Marshal(array)
	return string(normalized), err
}

func decimalToBPS(value string) (int64, error) {
	rational, err := nonNegativeRat(value)
	if err != nil {
		return 0, err
	}
	rational.Mul(rational, big.NewRat(basisPoints, 1))
	result, err := roundNonNegativeRat(rational)
	if err != nil {
		return 0, err
	}
	if result > 100000 {
		return 0, errors.New("value exceeds the supported 1000% boundary")
	}
	return result, nil
}

func roundedNonNegative(value string) (int64, error) {
	rational, err := nonNegativeRat(value)
	if err != nil {
		return 0, err
	}
	return roundNonNegativeRat(rational)
}

func nonNegativeRat(value string) (*big.Rat, error) {
	rational, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok || rational.Sign() < 0 {
		return nil, errors.New("value must be a non-negative decimal")
	}
	return rational, nil
}

func roundNonNegativeRat(value *big.Rat) (int64, error) {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, errors.New("rounded value exceeds int64")
	}
	return quotient.Int64(), nil
}

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	normalized := value.Time.UTC().Truncate(time.Microsecond)
	return &normalized
}

func intPointer(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func stateName(value int64) string {
	if value == 2 {
		return "wearing"
	}
	return "owned"
}
