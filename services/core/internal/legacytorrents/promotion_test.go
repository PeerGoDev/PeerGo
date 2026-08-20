package legacytorrents

import (
	"testing"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func TestCatalogPromotionPreservesPtYesCutoverSemantics(t *testing.T) {
	t.Parallel()

	cutover := time.Date(2026, time.August, 12, 8, 0, 0, 0, time.UTC)
	activeUntil := cutover.Add(24 * time.Hour)
	expiredUntil := cutover.Add(-time.Second)
	tests := []struct {
		name      string
		source    sourceTorrent
		promotion catalog.Promotion
		hasEndsAt bool
	}{
		{name: "follow global is not an independent label", source: sourceTorrent{PromotionType: 2, PromotionTimeType: 0}, promotion: catalog.PromotionNone},
		{name: "normal permanent", source: sourceTorrent{PromotionType: 1, PromotionTimeType: 1}, promotion: catalog.PromotionNone},
		{name: "permanent double upload", source: sourceTorrent{PromotionType: 3, PromotionTimeType: 1}, promotion: catalog.PromotionDoubleUpload},
		{name: "active timed double free", source: sourceTorrent{PromotionType: 4, PromotionTimeType: 2, PromotionUntil: &activeUntil}, promotion: catalog.PromotionDoubleUploadFree, hasEndsAt: true},
		{name: "expired timed free", source: sourceTorrent{PromotionType: 2, PromotionTimeType: 2, PromotionUntil: &expiredUntil}, promotion: catalog.PromotionNone},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			promotion, endsAt, err := test.source.catalogPromotion(cutover)
			if err != nil || promotion != test.promotion || (endsAt != nil) != test.hasEndsAt {
				t.Fatalf("catalogPromotion() = %q, %v, %v", promotion, endsAt, err)
			}
		})
	}
}

func TestCatalogPromotionRejectsIncompleteTimedSource(t *testing.T) {
	t.Parallel()

	_, _, err := (sourceTorrent{PromotionType: 2, PromotionTimeType: 2}).catalogPromotion(time.Now().UTC())
	if err == nil {
		t.Fatal("catalogPromotion() accepted a timed promotion without promotion_until")
	}
}
