package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/peergo/peergo/services/email-relay/internal/delivery"
)

const maxRequestBytes = 8 << 10

type sender interface {
	Send(context.Context, delivery.Message) error
}

func New(renderer *delivery.Renderer, mailer sender, serviceToken string, logger *slog.Logger) (http.Handler, error) {
	if renderer == nil || mailer == nil || len(serviceToken) < 32 || logger == nil {
		return nil, errors.New("email Relay dependencies are required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("POST /internal/v1/deliveries/transactional", requireToken(serviceToken, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(response, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var input delivery.Request
		if err := decoder.Decode(&input); err != nil || ensureEOF(decoder) != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		message, err := renderer.Render(input)
		if err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_delivery"})
			return
		}
		if err := mailer.Send(request.Context(), message); err != nil {
			// Recipient addresses and action links intentionally never enter logs.
			logger.ErrorContext(request.Context(), "transactional email delivery failed", "template", input.Template, "error", err)
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "delivery_unavailable"})
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	})))
	return noStore(mux), nil
}

func requireToken(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeJSON(response, http.StatusForbidden, map[string]string{"code": "service_auth_required"})
			return
		}
		next.ServeHTTP(response, request)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request has trailing data")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
