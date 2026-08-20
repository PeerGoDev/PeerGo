package promotioncontrolv1

import (
	"errors"
	"testing"
	"time"
)

func TestCommandCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	command := Command{
		SchemaVersion: SchemaVersion, CampaignID: "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c1",
		Scope: ScopeGlobal, Promotion: PromotionFree,
		StartsAt:            time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		EndsAt:              time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		OverrideLowerScopes: true, ReasonCode: "staff_campaign",
	}
	encoded, err := Encode(command)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.CampaignID != command.CampaignID || decoded.Promotion != command.Promotion {
		t.Fatalf("Decode() = %+v, want %+v", decoded, command)
	}
}

func TestCommandRejectsAmbiguousScopes(t *testing.T) {
	t.Parallel()
	torrentID := int64(42)
	command := Command{
		SchemaVersion: SchemaVersion, CampaignID: "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c1",
		Scope: ScopeTorrent, TorrentID: &torrentID, Promotion: PromotionFree,
		StartsAt:            time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		EndsAt:              time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		OverrideLowerScopes: true, ReasonCode: "staff_campaign",
	}
	if err := Validate(command); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate() error = %v, want ErrInvalid", err)
	}
}
