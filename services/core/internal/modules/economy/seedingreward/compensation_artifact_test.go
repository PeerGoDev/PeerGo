package seedingreward

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeCompensationArtifactValidatesTotalsAndOrder(t *testing.T) {
	header, records := compensationArtifactFixture()
	artifact, err := DecodeCompensationArtifact(encodeCompensationFixture(t, header, records))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RecordCount != 2 || artifact.MagicDelta != 8 || artifact.ExperienceDelta != "0.16" {
		t.Fatalf("artifact totals = %+v", artifact)
	}
	if err := validateArtifactForApply(artifact); err != nil {
		t.Fatalf("validateArtifactForApply() error = %v", err)
	}
}

func TestDecodeCompensationArtifactRejectsNonCanonicalOrDuplicateRecords(t *testing.T) {
	header, records := compensationArtifactFixture()
	records[0].ExperienceDelta = "0.100"
	if _, err := DecodeCompensationArtifact(encodeCompensationFixture(t, header, records)); err == nil {
		t.Fatal("non-canonical experience was accepted")
	}

	_, records = compensationArtifactFixture()
	records[1] = records[0]
	if _, err := DecodeCompensationArtifact(encodeCompensationFixture(t, header, records)); err == nil {
		t.Fatal("duplicate compensation source was accepted")
	}
}

func TestDecodeCompensationArtifactRejectsUnknownFields(t *testing.T) {
	header, records := compensationArtifactFixture()
	encoded := encodeCompensationFixture(t, header, records).String()
	encoded = strings.Replace(encoded, `"record_type":"manifest"`, `"record_type":"manifest","unexpected":true`, 1)
	if _, err := DecodeCompensationArtifact(strings.NewReader(encoded)); err == nil {
		t.Fatal("unknown manifest field was accepted")
	}
}

func TestValidateArtifactForApplyRechecksCrossRecordBindings(t *testing.T) {
	header, records := compensationArtifactFixture()
	artifact, err := DecodeCompensationArtifact(encodeCompensationFixture(t, header, records))
	if err != nil {
		t.Fatal(err)
	}

	duplicateCalculation := artifact
	duplicateCalculation.Records = append([]CompensationArtifactRecord(nil), artifact.Records...)
	duplicateCalculation.Records[1].CorrectedCalculationSHA256 =
		duplicateCalculation.Records[0].CorrectedCalculationSHA256
	if err := validateArtifactForApply(duplicateCalculation); err == nil {
		t.Fatal("duplicate corrected calculation was accepted by the apply boundary")
	}

	changedWindowEvidence := artifact
	changedWindowEvidence.Records = append([]CompensationArtifactRecord(nil), artifact.Records...)
	changedWindowEvidence.Records[1].CorrectedEvidenceSHA256 = strings.Repeat("4", 64)
	if err := validateArtifactForApply(changedWindowEvidence); err == nil {
		t.Fatal("inconsistent corrected window evidence was accepted by the apply boundary")
	}
}

func TestParseCompensationArtifactSHA256RequiresCanonicalDigest(t *testing.T) {
	valid := strings.Repeat("ab", 32)
	parsed, err := ParseCompensationArtifactSHA256(valid)
	if err != nil || parsed[0] != 0xab {
		t.Fatalf("ParseCompensationArtifactSHA256(valid) = %x, %v", parsed, err)
	}
	for _, invalid := range []string{strings.ToUpper(valid), strings.Repeat("0", 64), "ab"} {
		if _, err := ParseCompensationArtifactSHA256(invalid); err == nil {
			t.Fatalf("invalid digest %q was accepted", invalid)
		}
	}
}

func compensationArtifactFixture() (CompensationArtifactHeader, []CompensationArtifactRecord) {
	header := CompensationArtifactHeader{
		SchemaVersion: CompensationPreviewSchemaVersion, RecordType: "manifest",
		TrackerSourceStream: "PEERGO_TRACKER_ANNOUNCE_V1", TrackerFenceSequence: 42,
		MaximumIntervalSeconds: 2100,
		FirstWindow:            "2026-08-21T05:00:00Z", LastWindow: "2026-08-21T05:00:00Z",
	}
	users := []string{
		"0198f20a-6da8-7e51-9c64-111111111111",
		"0198f20a-6da8-7e51-9c64-222222222222",
	}
	records := make([]CompensationArtifactRecord, len(users))
	for index, user := range users {
		delta := int64(5 - index*2)
		records[index] = CompensationArtifactRecord{
			SchemaVersion: CompensationPreviewSchemaVersion, RecordType: "positive_delta",
			SourceReference: "seeding_compensation:v1:1787288400:" + user,
			WindowStart:     "2026-08-21T05:00:00Z", UserID: user,
			PolicyRevision: "rousi-reward-v1", BenefitRevision: "benefit-v1.e1.l1.rousi-v1",
			CorrectedCalculationSHA256: strings.Repeat(string(rune('1'+index)), 64),
			CorrectedEvidenceSHA256:    strings.Repeat("3", 64),
			OriginalReward:             0, CorrectedReward: delta, MagicDelta: delta,
			ExperienceDelta:      map[bool]string{true: "0.1", false: "0.06"}[index == 0],
			EligibleTorrentCount: 1,
		}
	}
	return header, records
}

func encodeCompensationFixture(
	t *testing.T,
	header CompensationArtifactHeader,
	records []CompensationArtifactRecord,
) *bytes.Buffer {
	t.Helper()
	var result bytes.Buffer
	encoder := json.NewEncoder(&result)
	if err := encoder.Encode(header); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	return &result
}
