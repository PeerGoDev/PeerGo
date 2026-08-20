// Package trackerruntimepolicyv1 defines the signed, hot-reloadable policy
// shared by Core and Tracker. It contains only request-path business controls;
// process capacity and secrets remain deployment configuration.
package trackerruntimepolicyv1

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SnapshotSchemaVersion = "1.0.0"
	MaxClientRules        = 16
	MaxSeedboxRules       = 4096

	signatureDomain = "peergo:tracker-runtime-policy-snapshot-signature:v1\x00"
	stateHashDomain = "peergo:tracker-runtime-policy-snapshot-state:v1\x00"
)

var (
	ErrInvalid          = signedsnapshotv1.ErrInvalid
	ErrSignatureInvalid = signedsnapshotv1.ErrSignatureInvalid
	ErrKeyUnknown       = signedsnapshotv1.ErrKeyUnknown

	revisionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	versionPattern  = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){1,3}$`)
)

type ClientMode string
type ClientFamily string

const (
	ClientModeAllowAll  ClientMode = "allow_all"
	ClientModeAllowList ClientMode = "allow_list"

	ClientFamilyQBittorrent  ClientFamily = "qbittorrent"
	ClientFamilyTransmission ClientFamily = "transmission"
	ClientFamilyDeluge       ClientFamily = "deluge"
	ClientFamilyLibtorrent   ClientFamily = "libtorrent"
	ClientFamilyUTorrent     ClientFamily = "utorrent"
)

type ClientRule struct {
	Family     ClientFamily `json:"family"`
	MinVersion string       `json:"min_version,omitempty"`
}

// SeedboxRule is a reviewed network prefix. Tracker resolves the socket peer
// against these rules, but announce events carry only the classification and
// rule ID: raw client addresses never cross the Tracker service boundary.
type SeedboxRule struct {
	ID            string `json:"id"`
	CIDR          string `json:"cidr"`
	UserNumericID int64  `json:"user_numeric_id,omitempty"`
}

// SeedboxPolicy keeps classification, upload accounting and speed evidence in
// the same signed revision. A zero speed limit means "observe no limit"; it
// does not silently invent an environment-specific default.
type SeedboxPolicy struct {
	Enabled                          bool          `json:"enabled"`
	UploadFactorBasisPoints          int           `json:"upload_factor_basis_points"`
	SeedboxSpeedLimitBytesPerSecond  int64         `json:"seedbox_speed_limit_bytes_per_second"`
	StandardSpeedLimitBytesPerSecond int64         `json:"standard_speed_limit_bytes_per_second"`
	Rules                            []SeedboxRule `json:"rules"`
}

type Policy struct {
	Revision                   string        `json:"revision"`
	AnnounceIntervalSeconds    int           `json:"announce_interval_seconds"`
	MinAnnounceIntervalSeconds int           `json:"min_announce_interval_seconds"`
	DefaultNumWant             int           `json:"default_numwant"`
	MaxNumWant                 int           `json:"max_numwant"`
	ScrapeEnabled              bool          `json:"scrape_enabled"`
	MaxScrapeHashes            int           `json:"max_scrape_hashes"`
	ClientMode                 ClientMode    `json:"client_mode"`
	AllowedClients             []ClientRule  `json:"allowed_clients"`
	UserRequestsPerMinute      int           `json:"user_requests_per_minute"`
	UserBurst                  int           `json:"user_burst"`
	AddressRequestsPerMinute   int           `json:"address_requests_per_minute"`
	AddressBurst               int           `json:"address_burst"`
	Seedbox                    SeedboxPolicy `json:"seedbox"`
}

type Snapshot struct {
	SchemaVersion   string    `json:"schema_version"`
	GeneratedAt     time.Time `json:"generated_at"`
	ControlSequence int64     `json:"control_sequence"`
	StateSHA256     string    `json:"state_sha256"`
	Policy          Policy    `json:"policy"`
}

type SignedArtifact struct {
	Bytes          []byte
	Snapshot       Snapshot
	KeyID          string
	PayloadSHA256  [sha256.Size]byte
	ArtifactSHA256 [sha256.Size]byte
}

type VerifiedSnapshot struct {
	Snapshot       Snapshot
	KeyID          string
	PayloadSHA256  [sha256.Size]byte
	ArtifactSHA256 [sha256.Size]byte
}

type Inspection struct {
	Snapshot      Snapshot
	KeyID         string
	PayloadSHA256 [sha256.Size]byte
}

// NormalizePolicy applies the same ordering and semantic validation used by
// signed snapshots. Core calls it before persisting a revision so an invalid
// policy can never reach the signing boundary.
func NormalizePolicy(policy Policy) (Policy, error) {
	normalized, err := normalizeAndValidate(Snapshot{
		GeneratedAt: time.Unix(1, 0).UTC(), ControlSequence: 1, Policy: policy,
	}, true)
	if err != nil {
		return Policy{}, err
	}
	return normalized.Policy, nil
}

func Sign(snapshot Snapshot, keyID string, privateKey ed25519.PrivateKey) (SignedArtifact, error) {
	normalized, err := normalizeAndValidate(snapshot, true)
	if err != nil || signedsnapshotv1.ValidateKeyID(keyID) != nil || len(privateKey) != ed25519.PrivateKeySize {
		return SignedArtifact{}, ErrInvalid
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return SignedArtifact{}, ErrInvalid
	}
	signed, err := signedsnapshotv1.Sign(payload, keyID, privateKey, signatureDomain)
	if err != nil {
		return SignedArtifact{}, err
	}
	return SignedArtifact{
		Bytes: signed.Bytes, Snapshot: normalized, KeyID: signed.KeyID,
		PayloadSHA256: signed.PayloadSHA256, ArtifactSHA256: signed.ArtifactSHA256,
	}, nil
}

func Verify(encoded []byte, trustedKeys map[string]ed25519.PublicKey) (VerifiedSnapshot, error) {
	verified, err := signedsnapshotv1.Verify(encoded, trustedKeys, signatureDomain)
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	snapshot, err := decodeSnapshot(verified.Payload)
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	return VerifiedSnapshot{
		Snapshot: snapshot, KeyID: verified.KeyID, PayloadSHA256: verified.PayloadSHA256,
		ArtifactSHA256: verified.ArtifactSHA256,
	}, nil
}

func InspectUnverified(encoded []byte) (Inspection, error) {
	inspection, err := signedsnapshotv1.Inspect(encoded)
	if err != nil {
		return Inspection{}, err
	}
	snapshot, err := decodeSnapshot(inspection.Payload)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Snapshot: snapshot, KeyID: inspection.KeyID, PayloadSHA256: inspection.PayloadSHA256}, nil
}

func decodeSnapshot(payload []byte) (Snapshot, error) {
	var snapshot Snapshot
	if signedsnapshotv1.StrictJSON(payload, &snapshot) != nil {
		return Snapshot{}, ErrInvalid
	}
	normalized, err := normalizeAndValidate(snapshot, false)
	if err != nil {
		return Snapshot{}, err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil || !bytes.Equal(canonical, payload) {
		return Snapshot{}, ErrInvalid
	}
	return normalized, nil
}

func normalizeAndValidate(snapshot Snapshot, preparing bool) (Snapshot, error) {
	if preparing {
		if snapshot.SchemaVersion == "" {
			snapshot.SchemaVersion = SnapshotSchemaVersion
		}
		snapshot.GeneratedAt = snapshot.GeneratedAt.UTC().Round(0)
		// Policy collections are JSON arrays by contract. Preserve an explicit
		// empty slice instead of normalizing it to nil/JSON null, which would be
		// semantically ambiguous and violate the persisted jsonb constraints.
		snapshot.Policy.AllowedClients = append(make([]ClientRule, 0, len(snapshot.Policy.AllowedClients)), snapshot.Policy.AllowedClients...)
		sort.Slice(snapshot.Policy.AllowedClients, func(i, j int) bool {
			return snapshot.Policy.AllowedClients[i].Family < snapshot.Policy.AllowedClients[j].Family
		})
		snapshot.Policy.Seedbox.Rules = append(make([]SeedboxRule, 0, len(snapshot.Policy.Seedbox.Rules)), snapshot.Policy.Seedbox.Rules...)
		for index := range snapshot.Policy.Seedbox.Rules {
			prefix, err := netip.ParsePrefix(snapshot.Policy.Seedbox.Rules[index].CIDR)
			if err != nil {
				return Snapshot{}, ErrInvalid
			}
			snapshot.Policy.Seedbox.Rules[index].CIDR = prefix.Masked().String()
		}
		sort.Slice(snapshot.Policy.Seedbox.Rules, func(i, j int) bool {
			left, right := snapshot.Policy.Seedbox.Rules[i], snapshot.Policy.Seedbox.Rules[j]
			if left.CIDR == right.CIDR {
				if left.UserNumericID != right.UserNumericID {
					return left.UserNumericID < right.UserNumericID
				}
				return left.ID < right.ID
			}
			return left.CIDR < right.CIDR
		})
	}
	policy := snapshot.Policy
	if snapshot.SchemaVersion != SnapshotSchemaVersion || snapshot.GeneratedAt.IsZero() || snapshot.ControlSequence < 1 ||
		!revisionPattern.MatchString(policy.Revision) || policy.AnnounceIntervalSeconds < 60 || policy.AnnounceIntervalSeconds > 86_400 ||
		policy.MinAnnounceIntervalSeconds < 30 || policy.MinAnnounceIntervalSeconds > policy.AnnounceIntervalSeconds ||
		policy.DefaultNumWant < 0 || policy.MaxNumWant < 1 || policy.MaxNumWant > 500 || policy.DefaultNumWant > policy.MaxNumWant ||
		policy.MaxScrapeHashes < 1 || policy.MaxScrapeHashes > 100 || len(policy.AllowedClients) > MaxClientRules ||
		policy.UserRequestsPerMinute < 1 || policy.UserRequestsPerMinute > 600 || policy.UserBurst < 1 || policy.UserBurst > 1_200 ||
		policy.AddressRequestsPerMinute < 1 || policy.AddressRequestsPerMinute > 5_000 || policy.AddressBurst < 1 || policy.AddressBurst > 10_000 ||
		policy.Seedbox.UploadFactorBasisPoints < 0 || policy.Seedbox.UploadFactorBasisPoints > 10_000 ||
		policy.Seedbox.SeedboxSpeedLimitBytesPerSecond < 0 || policy.Seedbox.StandardSpeedLimitBytesPerSecond < 0 ||
		len(policy.Seedbox.Rules) > MaxSeedboxRules {
		return Snapshot{}, ErrInvalid
	}
	_, offset := snapshot.GeneratedAt.Zone()
	if offset != 0 || (policy.ClientMode != ClientModeAllowAll && policy.ClientMode != ClientModeAllowList) ||
		(policy.ClientMode == ClientModeAllowAll && len(policy.AllowedClients) != 0) ||
		(policy.ClientMode == ClientModeAllowList && len(policy.AllowedClients) == 0) {
		return Snapshot{}, ErrInvalid
	}
	previous := ClientFamily("")
	for _, rule := range policy.AllowedClients {
		if !ValidClientFamily(rule.Family) || rule.Family <= previous ||
			(rule.MinVersion != "" && !validMinimumVersion(rule.MinVersion)) {
			return Snapshot{}, ErrInvalid
		}
		previous = rule.Family
	}
	seenRuleIDs := make(map[string]struct{}, len(policy.Seedbox.Rules))
	seenBindings := make(map[string]struct{}, len(policy.Seedbox.Rules))
	previousPrefix := ""
	var previousUserNumericID int64
	previousRuleID := ""
	for _, rule := range policy.Seedbox.Rules {
		prefix, err := netip.ParsePrefix(rule.CIDR)
		if err != nil || prefix.Masked().String() != rule.CIDR || !revisionPattern.MatchString(rule.ID) || rule.UserNumericID < 0 ||
			rule.CIDR < previousPrefix ||
			(rule.CIDR == previousPrefix && rule.UserNumericID < previousUserNumericID) ||
			(rule.CIDR == previousPrefix && rule.UserNumericID == previousUserNumericID && rule.ID <= previousRuleID) {
			return Snapshot{}, ErrInvalid
		}
		if _, duplicate := seenRuleIDs[rule.ID]; duplicate {
			return Snapshot{}, ErrInvalid
		}
		binding := fmt.Sprintf("%d\x00%s", rule.UserNumericID, rule.CIDR)
		if _, duplicate := seenBindings[binding]; duplicate {
			return Snapshot{}, ErrInvalid
		}
		seenRuleIDs[rule.ID] = struct{}{}
		seenBindings[binding] = struct{}{}
		previousPrefix, previousUserNumericID, previousRuleID = rule.CIDR, rule.UserNumericID, rule.ID
	}
	want, err := stateDigest(snapshot.ControlSequence, policy)
	if err != nil {
		return Snapshot{}, ErrInvalid
	}
	wantHex := hex.EncodeToString(want[:])
	if preparing && snapshot.StateSHA256 == "" {
		snapshot.StateSHA256 = wantHex
	}
	if snapshot.StateSHA256 != wantHex {
		return Snapshot{}, ErrInvalid
	}
	return snapshot, nil
}

func validMinimumVersion(value string) bool {
	if !versionPattern.MatchString(value) {
		return false
	}
	for _, component := range strings.Split(value, ".") {
		parsed, err := strconv.Atoi(component)
		if err != nil || parsed > 35 {
			return false
		}
	}
	return true
}

func ValidClientFamily(family ClientFamily) bool {
	switch family {
	case ClientFamilyQBittorrent, ClientFamilyTransmission, ClientFamilyDeluge, ClientFamilyLibtorrent, ClientFamilyUTorrent:
		return true
	default:
		return false
	}
}

func stateDigest(sequence int64, policy Policy) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(policy)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(stateHashDomain))
	var integer [8]byte
	binary.BigEndian.PutUint64(integer[:], uint64(sequence))
	_, _ = hasher.Write(integer[:])
	_, _ = hasher.Write(encoded)
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}
