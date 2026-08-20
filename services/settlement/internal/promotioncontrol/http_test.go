package promotioncontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/hnrcontrolv1"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/contracts/go/vipbenefitv1"
	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
)

type stubAppender struct {
	created bool
	err     error
}

func (stub stubAppender) Append(context.Context, []byte, time.Time) (bool, error) {
	return stub.created, stub.err
}

func (stub stubAppender) AppendHNR(context.Context, []byte, time.Time) (bool, error) {
	return stub.created, stub.err
}

func (stub stubAppender) AppendWorkgroupBenefit(context.Context, []byte, time.Time) (bool, error) {
	return stub.created, stub.err
}

func (stub stubAppender) AppendVIPBenefit(context.Context, []byte, time.Time) (bool, error) {
	return stub.created, stub.err
}

func (stub stubAppender) Settings(_ context.Context, now time.Time) (settlementoperationsv1.Settings, error) {
	return settlementoperationsv1.Settings{
		GeneratedAt: now,
		Seedbox:     settlementoperationsv1.SeedboxPolicy{SettlementPrimitiveSupported: true},
	}, stub.err
}

func TestHTTPHandlerAcceptsCanonicalCommand(t *testing.T) {
	t.Parallel()
	payload, campaignID := testCommand(t)
	digest := sha256.Sum256(payload)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/settlement/promotion-rules", strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer promotion-control-service-token-v1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", campaignID)
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	stub := stubAppender{created: true}
	NewHTTPHandler(stub, stub, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
}

func TestHTTPHandlerMapsScopeOverlap(t *testing.T) {
	t.Parallel()
	payload, campaignID := testCommand(t)
	digest := sha256.Sum256(payload)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/settlement/promotion-rules", strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer promotion-control-service-token-v1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", campaignID)
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	stub := stubAppender{err: ErrOverlap}
	NewHTTPHandler(stub, stub, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "promotion_scope_overlap") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPHandlerRejectsAuthenticationBeforePayload(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	stub := stubAppender{err: errors.New("must not run")}
	NewHTTPHandler(stub, stub, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(
		response, httptest.NewRequest(http.MethodPost, "/internal/v1/settlement/promotion-rules", strings.NewReader("{}")),
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestHTTPHandlerReadsBoundedSettings(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/operations/settings", nil)
	request.Header.Set("Authorization", "Bearer promotion-control-service-token-v1")
	response := httptest.NewRecorder()
	stub := stubAppender{}
	NewHTTPHandler(stub, stub, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "settlement_primitive_supported") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPHandlerAcceptsCanonicalHNRPolicyRevision(t *testing.T) {
	t.Parallel()
	id := "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c2"
	payload, err := hnrcontrolv1.Encode(hnrcontrolv1.Command{
		SchemaVersion: hnrcontrolv1.SchemaVersion, RevisionID: id,
		EffectiveAt: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
		Policy: hnrpolicyv1.Policy{
			Rule: hnrpolicyv1.RuleRef{ID: "global-default", Version: 2},
			Mode: hnrpolicyv1.ModeEnforced, RequiredSeedSeconds: 259200,
			RequiredRatioBasisPoints: 10_000, AssessmentWindowSeconds: 604800,
			GracePeriodSeconds: 86400, MaxIntervalCreditSeconds: 3600,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/settlement/hnr-policy-revisions", strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer promotion-control-service-token-v1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", id)
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	NewHTTPHandler(stubAppender{created: true}, stubAppender{}, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
}

func TestHTTPHandlerAcceptsCanonicalWorkgroupBenefit(t *testing.T) {
	t.Parallel()
	id := "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c3"
	payload, err := workgroupbenefitv1.Encode(workgroupbenefitv1.Command{
		SchemaVersion: workgroupbenefitv1.SchemaVersion,
		TransitionID:  id,
		UserID:        "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c4",
		GroupKind:     workgroupbenefitv1.GroupRetention,
		Entitlement:   workgroupbenefitv1.EntitlementDownloadChargeExempt,
		Active:        true, StateVersion: 1,
		EffectiveAt: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/settlement/workgroup-benefit-transitions", strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer promotion-control-service-token-v1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", id)
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	NewHTTPHandler(stubAppender{created: true}, stubAppender{}, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
}

func TestHTTPHandlerAcceptsCanonicalVIPBenefit(t *testing.T) {
	t.Parallel()
	id := "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c5"
	until := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	payload, err := vipbenefitv1.Encode(vipbenefitv1.Command{
		SchemaVersion: vipbenefitv1.SchemaVersion,
		TransitionID:  id,
		UserID:        "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c6",
		Entitlement:   vipbenefitv1.EntitlementDownloadChargeExempt,
		Enabled:       true, ActiveUntil: &until, StateVersion: 2,
		EffectiveAt: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/settlement/vip-benefit-transitions", strings.NewReader(string(payload)))
	request.Header.Set("Authorization", "Bearer promotion-control-service-token-v1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", id)
	request.Header.Set("X-PeerGo-Content-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	NewHTTPHandler(stubAppender{created: true}, stubAppender{}, "promotion-control-service-token-v1", time.Now, nil).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
}

func testCommand(t *testing.T) ([]byte, string) {
	t.Helper()
	id := "018f1f70-7b5a-7cc4-9c21-cd56ca3a62c1"
	payload, err := promotioncontrolv1.Encode(promotioncontrolv1.Command{
		SchemaVersion: promotioncontrolv1.SchemaVersion, CampaignID: id,
		Scope: promotioncontrolv1.ScopeGlobal, Promotion: promotioncontrolv1.PromotionFree,
		StartsAt: time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
		OverrideLowerScopes: true, ReasonCode: "staff_campaign",
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload, id
}
