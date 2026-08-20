package promotioncontrol

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/hnrcontrolv1"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/contracts/go/vipbenefitv1"
	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
)

type commandAppender interface {
	Append(context.Context, []byte, time.Time) (bool, error)
	AppendHNR(context.Context, []byte, time.Time) (bool, error)
	AppendWorkgroupBenefit(context.Context, []byte, time.Time) (bool, error)
	AppendVIPBenefit(context.Context, []byte, time.Time) (bool, error)
}

type settingsReader interface {
	Settings(context.Context, time.Time) (settlementoperationsv1.Settings, error)
}

// NewHTTPHandler exposes a narrow authenticated Settlement control surface.
// Campaign payloads, raw announce evidence and bearer tokens are never included
// in logs or responses.
func NewHTTPHandler(repository commandAppender, settings settingsReader, serviceToken string, now func() time.Time, logger *slog.Logger) http.Handler {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /internal/v1/operations/settings", requireToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.ContentLength > 0 || len(request.TransferEncoding) != 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := settings.Settings(request.Context(), now().UTC())
		if err != nil {
			logger.ErrorContext(request.Context(), "read Settlement settings failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "settlement_settings_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})))
	mux.Handle("POST /internal/v1/settlement/promotion-rules", requireToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, promotioncontrolv1.MaxCommandBytes)
		payload, err := io.ReadAll(request.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "payload_too_large"})
			return
		}
		if err != nil || !digestMatches(payload, request.Header.Get("X-PeerGo-Content-SHA256")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_command_digest"})
			return
		}
		command, err := promotioncontrolv1.Decode(payload)
		if err != nil || command.CampaignID != strings.TrimSpace(request.Header.Get("Idempotency-Key")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_command"})
			return
		}
		created, err := repository.Append(request.Context(), payload, now().UTC())
		switch {
		case errors.Is(err, ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_command"})
		case errors.Is(err, ErrConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "campaign_id_conflict"})
		case errors.Is(err, ErrOverlap):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "promotion_scope_overlap"})
		case errors.Is(err, ErrHistoricalRewrite):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "settled_traffic_conflict"})
		case err != nil:
			logger.ErrorContext(request.Context(), "append promotion rule failed", "campaign_id", command.CampaignID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "settlement_ledger_unavailable"})
		case !created:
			writeJSON(w, http.StatusOK, map[string]string{"status": "already_recorded"})
		default:
			writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
		}
	})))
	mux.Handle("POST /internal/v1/settlement/hnr-policy-revisions", requireToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, hnrcontrolv1.MaxCommandBytes)
		payload, err := io.ReadAll(request.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "payload_too_large"})
			return
		}
		if err != nil || !digestMatches(payload, request.Header.Get("X-PeerGo-Content-SHA256")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_command_digest"})
			return
		}
		command, err := hnrcontrolv1.Decode(payload)
		if err != nil || command.RevisionID != strings.TrimSpace(request.Header.Get("Idempotency-Key")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_hnr_policy_command"})
			return
		}
		created, err := repository.AppendHNR(request.Context(), payload, now().UTC())
		switch {
		case errors.Is(err, ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_hnr_policy_command"})
		case errors.Is(err, ErrHNRConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "hnr_policy_timeline_conflict"})
		case err != nil:
			logger.ErrorContext(request.Context(), "append H&R policy revision failed", "revision_id", command.RevisionID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "settlement_ledger_unavailable"})
		case !created:
			writeJSON(w, http.StatusOK, map[string]string{"status": "already_recorded"})
		default:
			writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
		}
	})))
	mux.Handle("POST /internal/v1/settlement/workgroup-benefit-transitions", requireToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, workgroupbenefitv1.MaxCommandBytes)
		payload, err := io.ReadAll(request.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "payload_too_large"})
			return
		}
		if err != nil || !digestMatches(payload, request.Header.Get("X-PeerGo-Content-SHA256")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_command_digest"})
			return
		}
		command, err := workgroupbenefitv1.Decode(payload)
		if err != nil || command.TransitionID != strings.TrimSpace(request.Header.Get("Idempotency-Key")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_workgroup_benefit_command"})
			return
		}
		created, err := repository.AppendWorkgroupBenefit(request.Context(), payload, now().UTC())
		switch {
		case errors.Is(err, ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_workgroup_benefit_command"})
		case errors.Is(err, ErrWorkgroupBenefitConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "workgroup_benefit_timeline_conflict"})
		case errors.Is(err, ErrWorkgroupBenefitHistoricalRewrite):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "settled_traffic_conflict"})
		case err != nil:
			logger.ErrorContext(request.Context(), "append workgroup benefit transition failed", "transition_id", command.TransitionID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "settlement_ledger_unavailable"})
		case !created:
			writeJSON(w, http.StatusOK, map[string]string{"status": "already_recorded"})
		default:
			writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
		}
	})))
	mux.Handle("POST /internal/v1/settlement/vip-benefit-transitions", requireToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, vipbenefitv1.MaxCommandBytes)
		payload, err := io.ReadAll(request.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "payload_too_large"})
			return
		}
		if err != nil || !digestMatches(payload, request.Header.Get("X-PeerGo-Content-SHA256")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_command_digest"})
			return
		}
		command, err := vipbenefitv1.Decode(payload)
		if err != nil || command.TransitionID != strings.TrimSpace(request.Header.Get("Idempotency-Key")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_vip_benefit_command"})
			return
		}
		created, err := repository.AppendVIPBenefit(request.Context(), payload, now().UTC())
		switch {
		case errors.Is(err, ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_vip_benefit_command"})
		case errors.Is(err, ErrVIPBenefitConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "vip_benefit_timeline_conflict"})
		case errors.Is(err, ErrVIPBenefitHistoricalRewrite):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "settled_traffic_conflict"})
		case err != nil:
			logger.ErrorContext(request.Context(), "append VIP benefit transition failed", "transition_id", command.TransitionID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "settlement_ledger_unavailable"})
		case !created:
			writeJSON(w, http.StatusOK, map[string]string{"status": "already_recorded"})
		default:
			writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
		}
	})))
	return noStore(mux)
}

func requireToken(expected string, next http.Handler) http.Handler {
	want := []byte("Bearer " + expected)
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		got := []byte(request.Header.Get("Authorization"))
		if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "service_auth_failed"})
			return
		}
		next.ServeHTTP(w, request)
	})
}

func digestMatches(payload []byte, value string) bool {
	want, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256(payload)
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, request)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
