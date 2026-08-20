// Package httpserver exposes only narrow, service-authenticated Vault actions.
package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/privacy-vault/internal/credentials"
)

const maxCredentialRequestBytes = 8 << 10

type credentialService interface {
	Verify(context.Context, string, string, string) (uuid.UUID, error)
	VerifyForAccountAppeal(context.Context, string, string, string) (uuid.UUID, error)
	EnableCredentialAfterAccountAppeal(context.Context, uuid.UUID) error
	ProvisionRegistration(context.Context, credentials.ProvisionRegistrationInput) (uuid.UUID, error)
	ActivateRegistration(context.Context, uuid.UUID) (uuid.UUID, error)
}

type emailVerificationService interface {
	Request(context.Context, uuid.UUID, string) (credentials.EmailVerificationRequestResult, error)
	Confirm(context.Context, string) (credentials.EmailVerificationConfirmation, error)
}

type passwordRecoveryService interface {
	Request(context.Context, string) (credentials.PasswordRecoveryDispatch, error)
	Confirm(context.Context, string, string) (credentials.PasswordRecoveryConfirmation, error)
}

type emailOperationsService interface {
	Status(context.Context) (credentials.EmailOperationsStatus, error)
	Test(context.Context, string) (time.Time, error)
}

type twoFactorService interface {
	Status(context.Context, uuid.UUID) (credentials.TwoFactorStatus, error)
	StartEnrollment(context.Context, uuid.UUID, string, string) (credentials.TOTPEnrollmentStart, error)
	ConfirmEnrollment(context.Context, uuid.UUID, uuid.UUID, string) (credentials.TOTPEnrollmentConfirmation, error)
	RotateRecoveryCodes(context.Context, uuid.UUID, uuid.UUID, string, string) (credentials.TwoFactorChange, error)
	Disable(context.Context, uuid.UUID, uuid.UUID, string, string) (credentials.TwoFactorChange, error)
}

type trackerCredentialService interface {
	GetOrCreate(context.Context, uuid.UUID) (credentials.TrackerCredential, error)
}

type emailDirectory interface {
	ListEmails(context.Context, []uuid.UUID) ([]credentials.EmailRecord, error)
}

type readinessChecker interface {
	Ping(context.Context) error
}

// New composes the Vault HTTP surface. The one directory endpoint accepts only
// opaque references and returns the full email only to an authenticated Core service.
func New(service credentialService, emailVerification emailVerificationService, passwordRecovery passwordRecoveryService, emailOperations emailOperationsService, twoFactor twoFactorService, trackerCredentials trackerCredentialService, emails emailDirectory, ready readinessChecker, serviceToken string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := ready.Ping(r.Context()); err != nil {
			logger.ErrorContext(r.Context(), "vault readiness check failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("GET /internal/v1/operations/email", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		status, err := emailOperations.Status(r.Context())
		if err != nil {
			logger.ErrorContext(r.Context(), "vault email operations read failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "email_operations_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			GeneratedAt                  time.Time                        `json:"generated_at"`
			DeliveryMode                 string                           `json:"delivery_mode"`
			VerificationPublicOrigin     string                           `json:"verification_public_origin"`
			PasswordRecoveryPublicOrigin string                           `json:"password_recovery_public_origin"`
			VerificationTTLSeconds       int64                            `json:"verification_ttl_seconds"`
			PasswordRecoveryTTLSeconds   int64                            `json:"password_recovery_ttl_seconds"`
			CooldownSeconds              int64                            `json:"cooldown_seconds"`
			Templates                    []string                         `json:"templates"`
			Stats                        credentials.EmailOperationsStats `json:"stats"`
		}{
			status.GeneratedAt, status.DeliveryMode, status.VerificationPublicOrigin,
			status.PasswordRecoveryPublicOrigin, status.VerificationTTLSeconds,
			status.PasswordRecoveryTTLSeconds, status.CooldownSeconds, status.Templates, status.Stats,
		})
	})))
	mux.Handle("POST /internal/v1/operations/email/test", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Recipient string `json:"recipient"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		acceptedAt, err := emailOperations.Test(r.Context(), request.Recipient)
		if errors.Is(err, credentials.ErrEmailTestInput) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_recipient"})
			return
		}
		if err != nil {
			// Do not log the request body: it contains the recipient address.
			logger.ErrorContext(r.Context(), "vault email delivery test failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "email_delivery_unavailable"})
			return
		}
		writeJSON(w, http.StatusAccepted, struct {
			AcceptedAt time.Time `json:"accepted_at"`
			Template   string    `json:"template"`
		}{acceptedAt, string(credentials.EmailTemplateDeliveryTest)})
	})))
	mux.Handle("POST /internal/v1/identifiers/emails", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			CredentialRefs []uuid.UUID `json:"credential_refs"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil ||
			len(request.CredentialRefs) < 1 || len(request.CredentialRefs) > 50 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		records, err := emails.ListEmails(r.Context(), request.CredentialRefs)
		if err != nil {
			logger.ErrorContext(r.Context(), "vault email directory failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "directory_unavailable"})
			return
		}
		type item struct {
			CredentialRef uuid.UUID `json:"credential_ref"`
			Email         string    `json:"email"`
		}
		response := struct {
			Items []item `json:"items"`
		}{Items: make([]item, 0, len(records))}
		for _, record := range records {
			response.Items = append(response.Items, item{
				CredentialRef: record.CredentialRef,
				Email:         record.Email,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})))
	mux.Handle("POST /internal/v1/credentials/verify", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Identifier       string `json:"identifier"`
			Password         string `json:"password"`
			SecondFactorCode string `json:"second_factor_code"`
		}
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		if err := ensureJSONEOF(decoder); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}

		credentialRef, err := service.Verify(r.Context(), request.Identifier, request.Password, request.SecondFactorCode)
		var throttle *credentials.LoginThrottleError
		if errors.As(err, &throttle) {
			writeJSON(w, http.StatusTooManyRequests, struct {
				Code    string    `json:"code"`
				RetryAt time.Time `json:"retry_at"`
			}{"login_throttled", throttle.RetryAt.UTC()})
			return
		}
		if errors.Is(err, credentials.ErrSecondFactorRequired) {
			writeJSON(w, http.StatusPreconditionRequired, map[string]string{"code": "second_factor_required"})
			return
		}
		if errors.Is(err, credentials.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "invalid_credentials"})
			return
		}
		if err != nil {
			// Never attach identifier, request body or credential reference to logs.
			logger.ErrorContext(r.Context(), "vault credential verification failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "verification_unavailable"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"credential_ref": credentialRef.String()})
	})))
	mux.Handle("POST /internal/v1/credentials/verify-for-account-appeal", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Identifier       string `json:"identifier"`
			Password         string `json:"password"`
			SecondFactorCode string `json:"second_factor_code"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		credentialRef, err := service.VerifyForAccountAppeal(r.Context(), request.Identifier, request.Password, request.SecondFactorCode)
		var throttle *credentials.LoginThrottleError
		switch {
		case errors.As(err, &throttle):
			writeJSON(w, http.StatusTooManyRequests, struct {
				Code    string    `json:"code"`
				RetryAt time.Time `json:"retry_at"`
			}{"login_throttled", throttle.RetryAt.UTC()})
		case errors.Is(err, credentials.ErrSecondFactorRequired):
			writeJSON(w, http.StatusPreconditionRequired, map[string]string{"code": "second_factor_required"})
		case errors.Is(err, credentials.ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "invalid_credentials"})
		case err != nil:
			logger.ErrorContext(r.Context(), "vault account appeal verification failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "verification_unavailable"})
		default:
			writeJSON(w, http.StatusOK, map[string]string{"credential_ref": credentialRef.String()})
		}
	})))
	mux.Handle("POST /internal/v1/credentials/{credential_ref}/enable-after-account-appeal", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, err := uuid.Parse(r.PathValue("credential_ref"))
		if err != nil || r.ContentLength > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		if err := service.EnableCredentialAfterAccountAppeal(r.Context(), credentialRef); errors.Is(err, credentials.ErrInvalidCredentials) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "credential_not_found"})
			return
		} else if err != nil {
			logger.ErrorContext(r.Context(), "vault account appeal credential enable failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "credential_enable_unavailable"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("POST /internal/v1/credentials/{credential_ref}/tracker-credential", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, err := uuid.Parse(r.PathValue("credential_ref"))
		if err != nil || r.ContentLength > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		credential, err := trackerCredentials.GetOrCreate(r.Context(), credentialRef)
		switch {
		case errors.Is(err, credentials.ErrTrackerCredentialInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		case errors.Is(err, credentials.ErrTrackerCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "credential_not_found"})
			return
		case err != nil:
			// This endpoint returns P0 material. Never log the response, credential
			// reference, passkey, lookup HMAC, or request URL.
			logger.ErrorContext(r.Context(), "vault Tracker credential operation failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "tracker_credential_unavailable"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		writeJSON(w, http.StatusOK, struct {
			Passkey    string    `json:"passkey"`
			LookupHMAC string    `json:"lookup_hmac"`
			Version    int64     `json:"version"`
			CreatedAt  time.Time `json:"created_at"`
		}{
			Passkey:    credential.Passkey,
			LookupHMAC: hex.EncodeToString(credential.LookupHMAC[:]),
			Version:    credential.Version,
			CreatedAt:  credential.CreatedAt.UTC(),
		})
	})))
	mux.Handle("POST /internal/v1/registrations/provision", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			RegistrationID uuid.UUID `json:"registration_id"`
			Username       string    `json:"username"`
			Email          string    `json:"email"`
			Password       string    `json:"password"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		credentialRef, err := service.ProvisionRegistration(r.Context(), credentials.ProvisionRegistrationInput{
			RegistrationID: request.RegistrationID,
			Username:       request.Username,
			Email:          request.Email,
			Password:       request.Password,
		})
		switch {
		case errors.Is(err, credentials.ErrRegistrationInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_registration"})
			return
		case errors.Is(err, credentials.ErrIdentifierUnavailable):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "identifier_unavailable"})
			return
		case errors.Is(err, credentials.ErrRegistrationIdempotencyConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "idempotency_conflict"})
			return
		case err != nil:
			// This path handles secrets, so the log intentionally contains only
			// the stable operation name and internal error chain.
			logger.ErrorContext(r.Context(), "vault registration provision failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "provision_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"credential_ref": credentialRef.String()})
	})))
	mux.Handle("POST /internal/v1/registrations/{registration_id}/activate", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registrationID, err := uuid.Parse(r.PathValue("registration_id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		credentialRef, err := service.ActivateRegistration(r.Context(), registrationID)
		if errors.Is(err, credentials.ErrRegistrationInput) || errors.Is(err, credentials.ErrRegistrationProvisionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "provision_not_found"})
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "vault registration activation failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "activation_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"credential_ref": credentialRef.String()})
	})))
	mux.Handle("POST /internal/v1/email-verifications/request", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			CredentialRef uuid.UUID `json:"credential_ref"`
			Email         string    `json:"email"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := emailVerification.Request(r.Context(), request.CredentialRef, request.Email)
		var cooldown *credentials.EmailVerificationCooldownError
		switch {
		case errors.Is(err, credentials.ErrEmailVerificationInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		case errors.As(err, &cooldown):
			writeJSON(w, http.StatusTooManyRequests, struct {
				Code          string    `json:"code"`
				NextRequestAt time.Time `json:"next_request_at"`
			}{"cooldown", cooldown.NextRequestAt.UTC()})
			return
		case errors.Is(err, credentials.ErrEmailVerificationDelivery):
			logger.ErrorContext(r.Context(), "vault email verification delivery failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "delivery_unavailable"})
			return
		case err != nil:
			logger.ErrorContext(r.Context(), "vault email verification request failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "verification_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			VerificationID  uuid.UUID  `json:"verification_id"`
			AcceptedAt      time.Time  `json:"accepted_at"`
			NextRequestAt   time.Time  `json:"next_request_at"`
			AlreadyVerified bool       `json:"already_verified"`
			VerifiedAt      *time.Time `json:"verified_at,omitempty"`
		}{result.VerificationID, result.AcceptedAt, result.NextRequestAt, result.AlreadyVerified, result.VerifiedAt})
	})))
	mux.Handle("POST /internal/v1/email-verifications/confirm", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Token string `json:"token"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := emailVerification.Confirm(r.Context(), request.Token)
		switch {
		case errors.Is(err, credentials.ErrEmailVerificationInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		case errors.Is(err, credentials.ErrEmailVerificationTokenInvalid):
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "verification_unavailable"})
			return
		case err != nil:
			logger.ErrorContext(r.Context(), "vault email verification confirmation failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "verification_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			VerificationID uuid.UUID `json:"verification_id"`
			CredentialRef  uuid.UUID `json:"credential_ref"`
			VerifiedAt     time.Time `json:"verified_at"`
		}{result.VerificationID, result.CredentialRef, result.VerifiedAt})
	})))
	mux.Handle("POST /internal/v1/password-recoveries/request", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Email string `json:"email"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := passwordRecovery.Request(r.Context(), request.Email)
		var deliveryError *credentials.PasswordRecoveryDeliveryError
		switch {
		case errors.Is(err, credentials.ErrPasswordRecoveryInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		case errors.As(err, &deliveryError):
			// Delivery failure must not distinguish a real account from an
			// unknown address. Preserve the accepted projection and log only the
			// operation/error chain, never the address.
			logger.ErrorContext(r.Context(), "vault password recovery delivery failed", "error", err)
			result = deliveryError.Dispatch
		case err != nil:
			logger.ErrorContext(r.Context(), "vault password recovery request failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "recovery_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			AcceptedAt    time.Time `json:"accepted_at"`
			NextRequestAt time.Time `json:"next_request_at"`
		}{result.AcceptedAt.UTC(), result.NextRequestAt.UTC()})
	})))
	mux.Handle("POST /internal/v1/password-recoveries/confirm", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Token       string `json:"token"`
			NewPassword string `json:"new_password"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := passwordRecovery.Confirm(r.Context(), request.Token, request.NewPassword)
		switch {
		case errors.Is(err, credentials.ErrPasswordRecoveryInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		case errors.Is(err, credentials.ErrPasswordRecoveryTokenInvalid):
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "recovery_unavailable"})
			return
		case err != nil:
			logger.ErrorContext(r.Context(), "vault password recovery confirmation failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "recovery_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			RecoveryID        uuid.UUID `json:"recovery_id"`
			CredentialRef     uuid.UUID `json:"credential_ref"`
			PasswordChangedAt time.Time `json:"password_changed_at"`
		}{result.RecoveryID, result.CredentialRef, result.PasswordChangedAt.UTC()})
	})))
	mux.Handle("GET /internal/v1/credentials/{credential_ref}/two-factor", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, err := uuid.Parse(r.PathValue("credential_ref"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		status, err := twoFactor.Status(r.Context(), credentialRef)
		if errors.Is(err, credentials.ErrInvalidCredentials) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "credential_not_found"})
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "vault two-factor status failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "two_factor_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Enabled                bool       `json:"enabled"`
			EnabledAt              *time.Time `json:"enabled_at,omitempty"`
			RecoveryCodesRemaining int64      `json:"recovery_codes_remaining"`
		}{status.Enabled, status.EnabledAt, status.RecoveryCodesRemaining})
	})))
	mux.Handle("POST /internal/v1/credentials/{credential_ref}/totp-enrollments", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, ok := parseCredentialPath(w, r)
		if !ok || !requireJSON(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Password    string `json:"password"`
			AccountName string `json:"account_name"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := twoFactor.StartEnrollment(r.Context(), credentialRef, request.Password, request.AccountName)
		if writeTwoFactorError(w, err) {
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "vault TOTP enrollment start failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "two_factor_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			EnrollmentID    uuid.UUID `json:"enrollment_id"`
			Secret          string    `json:"secret"`
			ProvisioningURI string    `json:"provisioning_uri"`
			ExpiresAt       time.Time `json:"expires_at"`
		}{result.EnrollmentID, result.Secret, result.ProvisioningURI, result.ExpiresAt.UTC()})
	})))
	mux.Handle("POST /internal/v1/credentials/{credential_ref}/totp-enrollments/{enrollment_id}/confirm", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, ok := parseCredentialPath(w, r)
		if !ok || !requireJSON(w, r) {
			return
		}
		enrollmentID, err := uuid.Parse(r.PathValue("enrollment_id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Code string `json:"code"`
		}
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}
		result, err := twoFactor.ConfirmEnrollment(r.Context(), credentialRef, enrollmentID, request.Code)
		if writeTwoFactorError(w, err) {
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "vault TOTP enrollment confirmation failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "two_factor_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ChangeID      uuid.UUID `json:"change_id"`
			EnabledAt     time.Time `json:"enabled_at"`
			RecoveryCodes []string  `json:"recovery_codes"`
		}{result.ChangeID, result.EnabledAt.UTC(), result.RecoveryCodes})
	})))
	mux.Handle("POST /internal/v1/credentials/{credential_ref}/totp-recovery-codes/rotate", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, ok := parseCredentialPath(w, r)
		if !ok {
			return
		}
		changeID, password, code, ok := decodeTwoFactorReauthentication(w, r)
		if !ok {
			return
		}
		result, err := twoFactor.RotateRecoveryCodes(r.Context(), credentialRef, changeID, password, code)
		if writeTwoFactorError(w, err) {
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "vault recovery code rotation failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "two_factor_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ChangeID      uuid.UUID `json:"change_id"`
			ChangedAt     time.Time `json:"changed_at"`
			RecoveryCodes []string  `json:"recovery_codes"`
		}{result.ChangeID, result.ChangedAt.UTC(), result.RecoveryCodes})
	})))
	mux.Handle("POST /internal/v1/credentials/{credential_ref}/totp/disable", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRef, ok := parseCredentialPath(w, r)
		if !ok {
			return
		}
		changeID, password, code, ok := decodeTwoFactorReauthentication(w, r)
		if !ok {
			return
		}
		result, err := twoFactor.Disable(r.Context(), credentialRef, changeID, password, code)
		if writeTwoFactorError(w, err) {
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "vault TOTP disable failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "two_factor_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ChangeID  uuid.UUID `json:"change_id"`
			ChangedAt time.Time `json:"changed_at"`
		}{result.ChangeID, result.ChangedAt.UTC()})
	})))
	return mux
}

func parseCredentialPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	credentialRef, err := uuid.Parse(r.PathValue("credential_ref"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
		return uuid.Nil, false
	}
	return credentialRef, true
}

func decodeTwoFactorReauthentication(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, string, bool) {
	if !requireJSON(w, r) {
		return uuid.Nil, "", "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		ChangeID         uuid.UUID `json:"change_id"`
		Password         string    `json:"password"`
		SecondFactorCode string    `json:"second_factor_code"`
	}
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil || request.ChangeID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
		return uuid.Nil, "", "", false
	}
	return request.ChangeID, request.Password, request.SecondFactorCode, true
}

func writeTwoFactorError(w http.ResponseWriter, err error) bool {
	var throttle *credentials.LoginThrottleError
	switch {
	case err == nil:
		return false
	case errors.Is(err, credentials.ErrTwoFactorInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
	case errors.As(err, &throttle):
		writeJSON(w, http.StatusTooManyRequests, struct {
			Code    string    `json:"code"`
			RetryAt time.Time `json:"retry_at"`
		}{"verification_throttled", throttle.RetryAt.UTC()})
	case errors.Is(err, credentials.ErrTwoFactorVerification), errors.Is(err, credentials.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "verification_failed"})
	case errors.Is(err, credentials.ErrTwoFactorAlreadyEnabled):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "two_factor_already_enabled"})
	case errors.Is(err, credentials.ErrTwoFactorIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "idempotency_conflict"})
	case errors.Is(err, credentials.ErrTwoFactorNotEnabled):
		writeJSON(w, http.StatusConflict, map[string]string{"code": "two_factor_not_enabled"})
	case errors.Is(err, credentials.ErrTwoFactorEnrollmentNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "enrollment_not_found"})
	case errors.Is(err, credentials.ErrRecoveryCodeBundleUnavailable):
		writeJSON(w, http.StatusGone, map[string]string{"code": "recovery_codes_unavailable"})
	default:
		return false
	}
	return true
}

func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
		return false
	}
	return true
}

func requireServiceToken(expected string, next http.Handler) http.Handler {
	expectedHeader := []byte("Bearer " + expected)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := []byte(r.Header.Get("Authorization"))
		if len(actual) != len(expectedHeader) || subtle.ConstantTimeCompare(actual, expectedHeader) != 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "service_auth_failed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
