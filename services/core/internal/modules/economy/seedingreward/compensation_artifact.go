package seedingreward

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const compensationMaximumArtifactRecords = 500_000

var compensationRevisionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// CompensationArtifact is the fully validated in-memory form of a private
// preview. Keeping parsing separate from persistence ensures the exact file is
// completely checked before an approval row or member balance can be written.
type CompensationArtifact struct {
	Header             CompensationArtifactHeader
	Records            []CompensationArtifactRecord
	RecordCount        int64
	MagicDelta         int64
	ExperienceDelta    string
	experienceBPSUnits int64
}

// DecodeCompensationArtifact performs strict JSONL and domain validation. The
// caller should hash the same reader with io.TeeReader and compare that digest
// to explicit operator approval before invoking the write path.
func DecodeCompensationArtifact(input io.Reader) (CompensationArtifact, error) {
	if input == nil {
		return CompensationArtifact{}, ErrInput
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNumber := 0
	result := CompensationArtifact{Records: make([]CompensationArtifactRecord, 0, 8192)}
	seenSources := make(map[string]struct{})
	seenCalculations := make(map[string]struct{})
	windowEvidence := make(map[string]string)
	var previousWindow time.Time
	var previousUser uuid.UUID
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return CompensationArtifact{}, fmt.Errorf("compensation artifact line %d is empty: %w", lineNumber, ErrInvariant)
		}
		if lineNumber == 1 {
			if err := decodeCompensationLine(line, &result.Header); err != nil {
				return CompensationArtifact{}, fmt.Errorf("decode compensation manifest: %w", err)
			}
			if err := validateCompensationHeader(result.Header); err != nil {
				return CompensationArtifact{}, err
			}
			continue
		}
		if len(result.Records) >= compensationMaximumArtifactRecords {
			return CompensationArtifact{}, fmt.Errorf("compensation artifact exceeds record limit: %w", ErrInvariant)
		}
		var record CompensationArtifactRecord
		if err := decodeCompensationLine(line, &record); err != nil {
			return CompensationArtifact{}, fmt.Errorf("decode compensation record %d: %w", lineNumber-1, err)
		}
		window, userID, experienceUnits, err := validateCompensationRecord(result.Header, record)
		if err != nil {
			return CompensationArtifact{}, fmt.Errorf("validate compensation record %d: %w", lineNumber-1, err)
		}
		if !previousWindow.IsZero() && (window.Before(previousWindow) ||
			(window.Equal(previousWindow) && bytes.Compare(userID[:], previousUser[:]) <= 0)) {
			return CompensationArtifact{}, fmt.Errorf("compensation records are not strictly ordered: %w", ErrInvariant)
		}
		previousWindow, previousUser = window, userID
		if _, duplicate := seenSources[record.SourceReference]; duplicate {
			return CompensationArtifact{}, fmt.Errorf("duplicate compensation source: %w", ErrInvariant)
		}
		seenSources[record.SourceReference] = struct{}{}
		if _, duplicate := seenCalculations[record.CorrectedCalculationSHA256]; duplicate {
			return CompensationArtifact{}, fmt.Errorf("duplicate corrected calculation: %w", ErrInvariant)
		}
		seenCalculations[record.CorrectedCalculationSHA256] = struct{}{}
		if evidence, exists := windowEvidence[record.WindowStart]; exists && evidence != record.CorrectedEvidenceSHA256 {
			return CompensationArtifact{}, fmt.Errorf("window evidence digest changed inside artifact: %w", ErrInvariant)
		}
		windowEvidence[record.WindowStart] = record.CorrectedEvidenceSHA256
		if result.MagicDelta > math.MaxInt64-record.MagicDelta ||
			result.experienceBPSUnits > math.MaxInt64-experienceUnits {
			return CompensationArtifact{}, ErrInvariant
		}
		result.MagicDelta += record.MagicDelta
		result.experienceBPSUnits += experienceUnits
		result.Records = append(result.Records, record)
	}
	if err := scanner.Err(); err != nil {
		return CompensationArtifact{}, fmt.Errorf("read compensation artifact: %w", err)
	}
	if lineNumber == 0 || len(result.Records) == 0 {
		return CompensationArtifact{}, fmt.Errorf("compensation artifact has no positive records: %w", ErrInvariant)
	}
	result.RecordCount = int64(len(result.Records))
	var err error
	result.ExperienceDelta, err = basisPointUnits(result.experienceBPSUnits)
	if err != nil {
		return CompensationArtifact{}, err
	}
	return result, nil
}

func decodeCompensationLine(line []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ErrInvariant
		}
		return err
	}
	return nil
}

func validateCompensationHeader(header CompensationArtifactHeader) error {
	first, firstErr := time.Parse(time.RFC3339, header.FirstWindow)
	last, lastErr := time.Parse(time.RFC3339, header.LastWindow)
	if header.SchemaVersion != CompensationPreviewSchemaVersion || header.RecordType != "manifest" ||
		header.TrackerSourceStream != "PEERGO_TRACKER_ANNOUNCE_V1" || header.TrackerFenceSequence < 1 ||
		header.MaximumIntervalSeconds != int64(compensationMaxIntervalCredit/time.Second) ||
		firstErr != nil || lastErr != nil || header.FirstWindow != first.UTC().Format(time.RFC3339) ||
		header.LastWindow != last.UTC().Format(time.RFC3339) ||
		first.Nanosecond() != 0 || last.Nanosecond() != 0 || first.Minute() != 0 || last.Minute() != 0 ||
		first.Second() != 0 || last.Second() != 0 || last.Before(first) {
		return fmt.Errorf("invalid compensation manifest: %w", ErrInvariant)
	}
	return nil
}

func validateCompensationRecord(
	header CompensationArtifactHeader,
	record CompensationArtifactRecord,
) (time.Time, uuid.UUID, int64, error) {
	window, windowErr := time.Parse(time.RFC3339, record.WindowStart)
	first, _ := time.Parse(time.RFC3339, header.FirstWindow)
	last, _ := time.Parse(time.RFC3339, header.LastWindow)
	userID, userErr := uuid.Parse(record.UserID)
	expectedSource := ""
	if windowErr == nil && userErr == nil {
		expectedSource = fmt.Sprintf("seeding_compensation:v1:%d:%s", window.Unix(), userID.String())
	}
	experienceUnits, experienceErr := parseCanonicalBasisPointAmount(record.ExperienceDelta)
	if record.SchemaVersion != CompensationPreviewSchemaVersion || record.RecordType != "positive_delta" ||
		windowErr != nil || record.WindowStart != window.UTC().Format(time.RFC3339) || window.Nanosecond() != 0 ||
		window.Minute() != 0 || window.Second() != 0 || window.Before(first) || window.After(last) ||
		userErr != nil || userID == uuid.Nil || record.SourceReference != expectedSource ||
		!compensationRevisionPattern.MatchString(record.PolicyRevision) ||
		!compensationRevisionPattern.MatchString(record.BenefitRevision) ||
		!validOptionalCompensationDigest(record.OriginalCalculationSHA256) ||
		!validCompensationDigest(record.CorrectedCalculationSHA256) ||
		!validCompensationDigest(record.CorrectedEvidenceSHA256) ||
		record.OriginalReward < 0 || record.CorrectedReward <= record.OriginalReward ||
		record.MagicDelta <= 0 || record.CorrectedReward-record.OriginalReward != record.MagicDelta ||
		(record.OriginalReward > 0 && record.OriginalCalculationSHA256 == "") ||
		experienceErr != nil || experienceUnits < 0 || record.EligibleTorrentCount < 0 {
		return time.Time{}, uuid.Nil, 0, ErrInvariant
	}
	return window, userID, experienceUnits, nil
}

func validOptionalCompensationDigest(value string) bool {
	return value == "" || validCompensationDigest(value)
}

func validCompensationDigest(value string) bool {
	if len(value) != 2*32 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && !bytes.Equal(decoded, make([]byte, 32))
}

// ParseCompensationArtifactSHA256 accepts only the canonical lowercase digest
// form printed by the preview command and rejects the all-zero sentinel.
func ParseCompensationArtifactSHA256(value string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if !validCompensationDigest(value) {
		return result, ErrInput
	}
	decoded, _ := hex.DecodeString(value)
	copy(result[:], decoded)
	return result, nil
}

func parseCanonicalBasisPointAmount(value string) (int64, error) {
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, ErrInvariant
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts[0]) > 1 && parts[0][0] == '0') {
		return 0, ErrInvariant
	}
	integer, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || integer < 0 || integer > math.MaxInt64/10_000 {
		return 0, ErrInvariant
	}
	fraction := int64(0)
	if len(parts) == 2 {
		if parts[1] == "" || len(parts[1]) > 4 || strings.HasSuffix(parts[1], "0") {
			return 0, ErrInvariant
		}
		parsed, err := strconv.ParseInt(parts[1]+strings.Repeat("0", 4-len(parts[1])), 10, 64)
		if err != nil {
			return 0, ErrInvariant
		}
		fraction = parsed
	}
	if integer > (math.MaxInt64-fraction)/10_000 {
		return 0, ErrInvariant
	}
	units := integer*10_000 + fraction
	canonical, err := basisPointUnits(units)
	if err != nil || canonical != value {
		return 0, ErrInvariant
	}
	return units, nil
}
