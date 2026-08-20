package identity

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
)

const vaultResponseLimit = 4 << 10
const vaultDirectoryResponseLimit = 32 << 10

// VaultClient is the narrow outbound adapter used for credential decisions.
// It intentionally has no endpoint for reading password hashes or identifiers.
type VaultClient struct {
	verifyURL                   string
	verifyAccountAppealURL      string
	provisionURL                string
	activateURL                 string
	emailVerificationRequestURL string
	emailVerificationConfirmURL string
	passwordRecoveryRequestURL  string
	passwordRecoveryConfirmURL  string
	twoFactorCredentialURL      string
	trackerCredentialURL        string
	emailDirectoryURL           string
	emailOperationsURL          string
	emailOperationsTestURL      string
	serviceToken                string
	httpClient                  *http.Client
}

// NewVaultClient validates the endpoint once at startup. Redirects are blocked
// so a misconfigured Vault cannot forward P0 credential material elsewhere.
func NewVaultClient(baseURL, serviceToken string, timeout time.Duration) (*VaultClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("vault base URL must be an absolute URL without user info")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("vault base URL must not contain query or fragment")
	}
	if len(serviceToken) < 32 {
		return nil, errors.New("vault service token must contain at least 32 bytes")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	basePath := strings.TrimRight(parsed.Path, "/")
	verifyURL := *parsed
	verifyURL.Path = basePath + "/internal/v1/credentials/verify"
	verifyAccountAppealURL := *parsed
	verifyAccountAppealURL.Path = basePath + "/internal/v1/credentials/verify-for-account-appeal"
	provisionURL := *parsed
	provisionURL.Path = basePath + "/internal/v1/registrations/provision"
	activateURL := *parsed
	activateURL.Path = basePath + "/internal/v1/registrations"
	emailVerificationRequestURL := *parsed
	emailVerificationRequestURL.Path = basePath + "/internal/v1/email-verifications/request"
	emailVerificationConfirmURL := *parsed
	emailVerificationConfirmURL.Path = basePath + "/internal/v1/email-verifications/confirm"
	passwordRecoveryRequestURL := *parsed
	passwordRecoveryRequestURL.Path = basePath + "/internal/v1/password-recoveries/request"
	passwordRecoveryConfirmURL := *parsed
	passwordRecoveryConfirmURL.Path = basePath + "/internal/v1/password-recoveries/confirm"
	twoFactorCredentialURL := *parsed
	twoFactorCredentialURL.Path = basePath + "/internal/v1/credentials"
	trackerCredentialURL := *parsed
	trackerCredentialURL.Path = basePath + "/internal/v1/credentials"
	emailDirectoryURL := *parsed
	emailDirectoryURL.Path = basePath + "/internal/v1/identifiers/emails"
	emailOperationsURL := *parsed
	emailOperationsURL.Path = basePath + "/internal/v1/operations/email"
	emailOperationsTestURL := *parsed
	emailOperationsTestURL.Path = basePath + "/internal/v1/operations/email/test"
	return &VaultClient{
		verifyURL:                   verifyURL.String(),
		verifyAccountAppealURL:      verifyAccountAppealURL.String(),
		provisionURL:                provisionURL.String(),
		activateURL:                 activateURL.String(),
		emailVerificationRequestURL: emailVerificationRequestURL.String(),
		emailVerificationConfirmURL: emailVerificationConfirmURL.String(),
		passwordRecoveryRequestURL:  passwordRecoveryRequestURL.String(),
		passwordRecoveryConfirmURL:  passwordRecoveryConfirmURL.String(),
		twoFactorCredentialURL:      strings.TrimRight(twoFactorCredentialURL.String(), "/"),
		trackerCredentialURL:        strings.TrimRight(trackerCredentialURL.String(), "/"),
		emailDirectoryURL:           emailDirectoryURL.String(),
		emailOperationsURL:          emailOperationsURL.String(),
		emailOperationsTestURL:      emailOperationsTestURL.String(),
		serviceToken:                serviceToken,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// TestEmail forwards a single authorized recipient to Vault without storing it
// in Core. Vault owns normalization, template selection and actual delivery.
func (c *VaultClient) TestEmail(ctx context.Context, recipient string) (vaultoperations.EmailTestResult, error) {
	payload, err := json.Marshal(struct {
		Recipient string `json:"recipient"`
	}{recipient})
	if err != nil {
		return vaultoperations.EmailTestResult{}, fmt.Errorf("encode Vault email delivery test: %w", err)
	}
	response, err := c.postJSON(ctx, c.emailOperationsTestURL, payload)
	if err != nil {
		return vaultoperations.EmailTestResult{}, fmt.Errorf("%w: send Vault request: %v", vaultoperations.ErrEmailTestUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		if response.StatusCode == http.StatusBadRequest {
			return vaultoperations.EmailTestResult{}, vaultoperations.ErrEmailTestInvalidRecipient
		}
		return vaultoperations.EmailTestResult{}, fmt.Errorf("%w: Vault returned status %d", vaultoperations.ErrEmailTestUnavailable, response.StatusCode)
	}
	var result vaultoperations.EmailTestResult
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.AcceptedAt.IsZero() || result.Template != "peergo-delivery-test-v1" {
		return vaultoperations.EmailTestResult{}, fmt.Errorf("%w: invalid Vault response", vaultoperations.ErrEmailTestUnavailable)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return vaultoperations.EmailTestResult{}, fmt.Errorf("%w: Vault response has trailing data", vaultoperations.ErrEmailTestUnavailable)
	}
	return result, nil
}

// EmailOperations reads only Privacy Vault's aggregate delivery health and
// deployment-safe runtime metadata. The endpoint cannot return recipients,
// action links, local paths, relay URLs, or service tokens.
func (c *VaultClient) EmailOperations(ctx context.Context) (vaultoperations.EmailStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.emailOperationsURL, nil)
	if err != nil {
		return vaultoperations.EmailStatus{}, fmt.Errorf("create Vault email operations request: %w", err)
	}
	c.authorize(request)
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return vaultoperations.EmailStatus{}, fmt.Errorf("read Vault email operations: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		return vaultoperations.EmailStatus{}, fmt.Errorf("Vault email operations returned status %d", response.StatusCode)
	}
	var status vaultoperations.EmailStatus
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status); err != nil || status.GeneratedAt.IsZero() ||
		(status.DeliveryMode != "development_outbox" && status.DeliveryMode != "https_relay") ||
		status.VerificationPublicOrigin == "" || status.PasswordRecoveryPublicOrigin == "" ||
		status.VerificationTTLSeconds < 1 || status.PasswordRecoveryTTLSeconds < 1 || status.CooldownSeconds < 1 || len(status.Templates) != 2 {
		return vaultoperations.EmailStatus{}, errors.New("Vault returned an invalid email operations response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return vaultoperations.EmailStatus{}, errors.New("Vault email operations response has trailing data")
	}
	return status, nil
}

// Emails resolves contact addresses for an already-authorized administrative
// request. Core uses the response in memory and never persists a copy.
func (c *VaultClient) Emails(ctx context.Context, credentialRefs []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(credentialRefs) < 1 || len(credentialRefs) > 50 {
		return nil, ErrManagedUserContactUnavailable
	}
	seen := make(map[uuid.UUID]struct{}, len(credentialRefs))
	for _, credentialRef := range credentialRefs {
		if credentialRef == uuid.Nil {
			return nil, ErrManagedUserContactUnavailable
		}
		if _, exists := seen[credentialRef]; exists {
			return nil, ErrManagedUserContactUnavailable
		}
		seen[credentialRef] = struct{}{}
	}
	payload, err := json.Marshal(struct {
		CredentialRefs []uuid.UUID `json:"credential_refs"`
	}{credentialRefs})
	if err != nil {
		return nil, fmt.Errorf("%w: encode email directory request", ErrManagedUserContactUnavailable)
	}
	response, err := c.postJSON(ctx, c.emailDirectoryURL, payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManagedUserContactUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultDirectoryResponseLimit))
		return nil, fmt.Errorf("%w: Vault returned status %d", ErrManagedUserContactUnavailable, response.StatusCode)
	}
	var result struct {
		Items []struct {
			CredentialRef uuid.UUID `json:"credential_ref"`
			Email         string    `json:"email"`
		} `json:"items"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultDirectoryResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode email directory", ErrManagedUserContactUnavailable)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: email directory has trailing data", ErrManagedUserContactUnavailable)
	}
	emails := make(map[uuid.UUID]string, len(result.Items))
	for _, item := range result.Items {
		if item.CredentialRef == uuid.Nil || item.Email == "" {
			return nil, fmt.Errorf("%w: Vault returned an invalid email", ErrManagedUserContactUnavailable)
		}
		if _, requested := seen[item.CredentialRef]; !requested {
			return nil, fmt.Errorf("%w: Vault returned an unrequested credential", ErrManagedUserContactUnavailable)
		}
		if _, duplicate := emails[item.CredentialRef]; duplicate {
			return nil, fmt.Errorf("%w: Vault returned a duplicate credential", ErrManagedUserContactUnavailable)
		}
		emails[item.CredentialRef] = item.Email
	}
	return emails, nil
}

// GetOrCreateTrackerCredential is the only Vault call allowed to return a raw
// Tracker passkey. The response remains in request memory, redirects are
// blocked by the shared client, and Core persists only the HMAC projection.
func (c *VaultClient) GetOrCreateTrackerCredential(ctx context.Context, credentialRef uuid.UUID) (TrackerCredential, error) {
	if credentialRef == uuid.Nil {
		return TrackerCredential{}, ErrTrackerCredentialStateConflict
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.trackerCredentialURL+"/"+credentialRef.String()+"/tracker-credential",
		nil,
	)
	if err != nil {
		return TrackerCredential{}, fmt.Errorf("%w: create Vault Tracker credential request", ErrTrackerCredentialUnavailable)
	}
	c.authorize(request)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return TrackerCredential{}, fmt.Errorf("%w: %v", ErrTrackerCredentialUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusNotFound {
			return TrackerCredential{}, ErrTrackerCredentialStateConflict
		}
		return TrackerCredential{}, fmt.Errorf("%w: Vault returned status %d", ErrTrackerCredentialUnavailable, response.StatusCode)
	}
	var result struct {
		Passkey    string    `json:"passkey"`
		LookupHMAC string    `json:"lookup_hmac"`
		Version    int64     `json:"version"`
		CreatedAt  time.Time `json:"created_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return TrackerCredential{}, fmt.Errorf("%w: decode Vault Tracker credential", ErrTrackerCredentialUnavailable)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return TrackerCredential{}, fmt.Errorf("%w: Vault Tracker credential response has trailing data", ErrTrackerCredentialUnavailable)
	}
	lookupBytes, err := hex.DecodeString(result.LookupHMAC)
	if err != nil || len(lookupBytes) != 32 || !validCoreTrackerPasskey(result.Passkey) || result.Version < 1 || result.CreatedAt.IsZero() {
		return TrackerCredential{}, fmt.Errorf("%w: Vault returned an invalid Tracker credential", ErrTrackerCredentialUnavailable)
	}
	var lookup [32]byte
	copy(lookup[:], lookupBytes)
	return TrackerCredential{
		Passkey: result.Passkey, LookupHMAC: lookup,
		Version: result.Version, CreatedAt: result.CreatedAt.UTC(),
	}, nil
}

// RequestPasswordRecovery forwards a transient normalized email. Vault owns
// both lookup and delivery and returns no account-match indicator.
func (c *VaultClient) RequestPasswordRecovery(ctx context.Context, email string) (PasswordRecoveryDispatch, error) {
	payload, err := json.Marshal(struct {
		Email string `json:"email"`
	}{email})
	if err != nil {
		return PasswordRecoveryDispatch{}, fmt.Errorf("encode vault password recovery request: %w", err)
	}
	response, err := c.postJSON(ctx, c.passwordRecoveryRequestURL, payload)
	if err != nil {
		return PasswordRecoveryDispatch{}, fmt.Errorf("%w: %v", ErrPasswordRecoveryServiceUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		if response.StatusCode == http.StatusBadRequest {
			return PasswordRecoveryDispatch{}, ErrInvalidInput
		}
		return PasswordRecoveryDispatch{}, fmt.Errorf("%w: vault returned status %d", ErrPasswordRecoveryServiceUnavailable, response.StatusCode)
	}
	var result struct {
		AcceptedAt    time.Time `json:"accepted_at"`
		NextRequestAt time.Time `json:"next_request_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.AcceptedAt.IsZero() || result.NextRequestAt.IsZero() {
		return PasswordRecoveryDispatch{}, fmt.Errorf("%w: decode vault password recovery dispatch", ErrPasswordRecoveryServiceUnavailable)
	}
	return PasswordRecoveryDispatch{AcceptedAt: result.AcceptedAt.UTC(), NextRequestAt: result.NextRequestAt.UTC()}, nil
}

func (c *VaultClient) ConfirmPasswordRecovery(ctx context.Context, token, newPassword string) (VaultPasswordRecoveryConfirmation, error) {
	payload, err := json.Marshal(struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}{token, newPassword})
	if err != nil {
		return VaultPasswordRecoveryConfirmation{}, fmt.Errorf("encode vault password recovery confirmation: %w", err)
	}
	response, err := c.postJSON(ctx, c.passwordRecoveryConfirmURL, payload)
	if err != nil {
		return VaultPasswordRecoveryConfirmation{}, fmt.Errorf("%w: %v", ErrPasswordRecoveryServiceUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		switch response.StatusCode {
		case http.StatusBadRequest:
			return VaultPasswordRecoveryConfirmation{}, ErrInvalidInput
		case http.StatusNotFound:
			return VaultPasswordRecoveryConfirmation{}, ErrPasswordRecoveryTokenInvalid
		default:
			return VaultPasswordRecoveryConfirmation{}, fmt.Errorf("%w: vault returned status %d", ErrPasswordRecoveryServiceUnavailable, response.StatusCode)
		}
	}
	var result struct {
		RecoveryID        uuid.UUID `json:"recovery_id"`
		CredentialRef     uuid.UUID `json:"credential_ref"`
		PasswordChangedAt time.Time `json:"password_changed_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.RecoveryID == uuid.Nil || result.CredentialRef == uuid.Nil || result.PasswordChangedAt.IsZero() {
		return VaultPasswordRecoveryConfirmation{}, fmt.Errorf("%w: decode vault password recovery confirmation", ErrPasswordRecoveryServiceUnavailable)
	}
	return VaultPasswordRecoveryConfirmation{
		RecoveryID: result.RecoveryID, CredentialRef: result.CredentialRef,
		PasswordChangedAt: result.PasswordChangedAt.UTC(),
	}, nil
}

// RequestEmailVerification sends the re-entered address only to Vault. The
// response contains timing and opaque identifiers, never the address or token.
func (c *VaultClient) RequestEmailVerification(ctx context.Context, credentialRef uuid.UUID, email string) (VaultEmailVerificationDispatch, error) {
	payload, err := json.Marshal(struct {
		CredentialRef uuid.UUID `json:"credential_ref"`
		Email         string    `json:"email"`
	}{credentialRef, email})
	if err != nil {
		return VaultEmailVerificationDispatch{}, fmt.Errorf("encode vault email verification request: %w", err)
	}
	response, err := c.postJSON(ctx, c.emailVerificationRequestURL, payload)
	if err != nil {
		return VaultEmailVerificationDispatch{}, fmt.Errorf("%w: %v", ErrEmailVerificationServiceUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code          string    `json:"code"`
			NextRequestAt time.Time `json:"next_request_at"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit)).Decode(&problem)
		switch {
		case response.StatusCode == http.StatusBadRequest:
			return VaultEmailVerificationDispatch{}, ErrInvalidInput
		case response.StatusCode == http.StatusTooManyRequests && !problem.NextRequestAt.IsZero():
			return VaultEmailVerificationDispatch{}, &EmailVerificationCooldownError{NextRequestAt: problem.NextRequestAt.UTC()}
		case response.StatusCode == http.StatusServiceUnavailable && problem.Code == "delivery_unavailable":
			return VaultEmailVerificationDispatch{}, ErrEmailVerificationDeliveryUnavailable
		default:
			return VaultEmailVerificationDispatch{}, fmt.Errorf("%w: vault returned status %d", ErrEmailVerificationServiceUnavailable, response.StatusCode)
		}
	}
	var result struct {
		VerificationID  uuid.UUID  `json:"verification_id"`
		AcceptedAt      time.Time  `json:"accepted_at"`
		NextRequestAt   time.Time  `json:"next_request_at"`
		AlreadyVerified bool       `json:"already_verified"`
		VerifiedAt      *time.Time `json:"verified_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.AcceptedAt.IsZero() || result.NextRequestAt.IsZero() ||
		(result.AlreadyVerified && (result.VerificationID == uuid.Nil || result.VerifiedAt == nil)) {
		return VaultEmailVerificationDispatch{}, fmt.Errorf("%w: decode vault email verification dispatch", ErrEmailVerificationServiceUnavailable)
	}
	return VaultEmailVerificationDispatch{
		VerificationID:  result.VerificationID,
		AcceptedAt:      result.AcceptedAt.UTC(),
		NextRequestAt:   result.NextRequestAt.UTC(),
		AlreadyVerified: result.AlreadyVerified,
		VerifiedAt:      result.VerifiedAt,
	}, nil
}

func (c *VaultClient) ConfirmEmailVerification(ctx context.Context, token string) (VaultEmailVerificationConfirmation, error) {
	payload, err := json.Marshal(struct {
		Token string `json:"token"`
	}{token})
	if err != nil {
		return VaultEmailVerificationConfirmation{}, fmt.Errorf("encode vault email verification confirmation: %w", err)
	}
	response, err := c.postJSON(ctx, c.emailVerificationConfirmURL, payload)
	if err != nil {
		return VaultEmailVerificationConfirmation{}, fmt.Errorf("%w: %v", ErrEmailVerificationServiceUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		switch response.StatusCode {
		case http.StatusBadRequest:
			return VaultEmailVerificationConfirmation{}, ErrInvalidInput
		case http.StatusNotFound:
			return VaultEmailVerificationConfirmation{}, ErrEmailVerificationTokenInvalid
		default:
			return VaultEmailVerificationConfirmation{}, fmt.Errorf("%w: vault returned status %d", ErrEmailVerificationServiceUnavailable, response.StatusCode)
		}
	}
	var responseBody struct {
		VerificationID uuid.UUID `json:"verification_id"`
		CredentialRef  uuid.UUID `json:"credential_ref"`
		VerifiedAt     time.Time `json:"verified_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&responseBody); err != nil || responseBody.VerificationID == uuid.Nil || responseBody.CredentialRef == uuid.Nil || responseBody.VerifiedAt.IsZero() {
		return VaultEmailVerificationConfirmation{}, fmt.Errorf("%w: decode vault email verification confirmation", ErrEmailVerificationServiceUnavailable)
	}
	return VaultEmailVerificationConfirmation{
		VerificationID: responseBody.VerificationID,
		CredentialRef:  responseBody.CredentialRef,
		VerifiedAt:     responseBody.VerifiedAt.UTC(),
	}, nil
}

func (c *VaultClient) TwoFactorStatus(ctx context.Context, credentialRef uuid.UUID) (TwoFactorStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.twoFactorURL(credentialRef, "/two-factor"), nil)
	if err != nil {
		return TwoFactorStatus{}, fmt.Errorf("create vault two-factor status request: %w", err)
	}
	c.authorize(request)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return TwoFactorStatus{}, fmt.Errorf("%w: %v", ErrTwoFactorServiceUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		return TwoFactorStatus{}, fmt.Errorf("%w: vault returned status %d", ErrTwoFactorServiceUnavailable, response.StatusCode)
	}
	var result struct {
		Enabled                bool       `json:"enabled"`
		EnabledAt              *time.Time `json:"enabled_at"`
		RecoveryCodesRemaining int64      `json:"recovery_codes_remaining"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.RecoveryCodesRemaining < 0 ||
		(result.Enabled != (result.EnabledAt != nil)) {
		return TwoFactorStatus{}, fmt.Errorf("%w: decode vault two-factor status", ErrTwoFactorServiceUnavailable)
	}
	if result.EnabledAt != nil {
		value := result.EnabledAt.UTC()
		result.EnabledAt = &value
	}
	return TwoFactorStatus{
		Enabled: result.Enabled, EnabledAt: result.EnabledAt,
		RecoveryCodesRemaining: result.RecoveryCodesRemaining,
	}, nil
}

func (c *VaultClient) StartTOTPEnrollment(ctx context.Context, credentialRef uuid.UUID, password, accountName string) (TOTPEnrollmentStart, error) {
	payload, err := json.Marshal(struct {
		Password    string `json:"password"`
		AccountName string `json:"account_name"`
	}{password, accountName})
	if err != nil {
		return TOTPEnrollmentStart{}, fmt.Errorf("encode vault TOTP enrollment request: %w", err)
	}
	response, err := c.postTwoFactor(ctx, credentialRef, "/totp-enrollments", payload)
	if err != nil {
		return TOTPEnrollmentStart{}, err
	}
	defer response.Body.Close()
	if err := decodeTwoFactorProblem(response); err != nil {
		return TOTPEnrollmentStart{}, err
	}
	var result struct {
		EnrollmentID    uuid.UUID `json:"enrollment_id"`
		Secret          string    `json:"secret"`
		ProvisioningURI string    `json:"provisioning_uri"`
		ExpiresAt       time.Time `json:"expires_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.EnrollmentID == uuid.Nil || result.Secret == "" || result.ProvisioningURI == "" || result.ExpiresAt.IsZero() {
		return TOTPEnrollmentStart{}, fmt.Errorf("%w: decode TOTP enrollment", ErrTwoFactorServiceUnavailable)
	}
	return TOTPEnrollmentStart{
		EnrollmentID: result.EnrollmentID, Secret: result.Secret,
		ProvisioningURI: result.ProvisioningURI, ExpiresAt: result.ExpiresAt.UTC(),
	}, nil
}

func (c *VaultClient) ConfirmTOTPEnrollment(ctx context.Context, credentialRef, enrollmentID uuid.UUID, code string) (TOTPEnrollmentConfirmation, error) {
	payload, err := json.Marshal(struct {
		Code string `json:"code"`
	}{code})
	if err != nil {
		return TOTPEnrollmentConfirmation{}, fmt.Errorf("encode vault TOTP confirmation: %w", err)
	}
	response, err := c.postTwoFactor(ctx, credentialRef, "/totp-enrollments/"+enrollmentID.String()+"/confirm", payload)
	if err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	defer response.Body.Close()
	if err := decodeTwoFactorProblem(response); err != nil {
		return TOTPEnrollmentConfirmation{}, err
	}
	var result struct {
		ChangeID      uuid.UUID `json:"change_id"`
		EnabledAt     time.Time `json:"enabled_at"`
		RecoveryCodes []string  `json:"recovery_codes"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.ChangeID == uuid.Nil || result.EnabledAt.IsZero() || len(result.RecoveryCodes) == 0 {
		return TOTPEnrollmentConfirmation{}, fmt.Errorf("%w: decode TOTP confirmation", ErrTwoFactorServiceUnavailable)
	}
	return TOTPEnrollmentConfirmation{ChangeID: result.ChangeID, EnabledAt: result.EnabledAt.UTC(), RecoveryCodes: result.RecoveryCodes}, nil
}

func (c *VaultClient) RotateTOTPRecoveryCodes(ctx context.Context, credentialRef, changeID uuid.UUID, password, code string) (TwoFactorVaultChange, error) {
	return c.twoFactorReauthentication(ctx, credentialRef, changeID, "/totp-recovery-codes/rotate", password, code, true)
}

func (c *VaultClient) DisableTOTP(ctx context.Context, credentialRef, changeID uuid.UUID, password, code string) (TwoFactorVaultChange, error) {
	return c.twoFactorReauthentication(ctx, credentialRef, changeID, "/totp/disable", password, code, false)
}

func (c *VaultClient) twoFactorReauthentication(ctx context.Context, credentialRef, changeID uuid.UUID, path, password, code string, expectRecoveryCodes bool) (TwoFactorVaultChange, error) {
	payload, err := json.Marshal(struct {
		ChangeID         uuid.UUID `json:"change_id"`
		Password         string    `json:"password"`
		SecondFactorCode string    `json:"second_factor_code"`
	}{changeID, password, code})
	if err != nil {
		return TwoFactorVaultChange{}, fmt.Errorf("encode vault two-factor reauthentication: %w", err)
	}
	response, err := c.postTwoFactor(ctx, credentialRef, path, payload)
	if err != nil {
		return TwoFactorVaultChange{}, err
	}
	defer response.Body.Close()
	if err := decodeTwoFactorProblem(response); err != nil {
		return TwoFactorVaultChange{}, err
	}
	var result struct {
		ChangeID      uuid.UUID `json:"change_id"`
		ChangedAt     time.Time `json:"changed_at"`
		RecoveryCodes []string  `json:"recovery_codes"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.ChangeID == uuid.Nil || result.ChangedAt.IsZero() ||
		(expectRecoveryCodes && len(result.RecoveryCodes) == 0) || (!expectRecoveryCodes && len(result.RecoveryCodes) != 0) {
		return TwoFactorVaultChange{}, fmt.Errorf("%w: decode two-factor change", ErrTwoFactorServiceUnavailable)
	}
	return TwoFactorVaultChange{ChangeID: result.ChangeID, ChangedAt: result.ChangedAt.UTC(), RecoveryCodes: result.RecoveryCodes}, nil
}

func (c *VaultClient) postTwoFactor(ctx context.Context, credentialRef uuid.UUID, path string, payload []byte) (*http.Response, error) {
	response, err := c.postJSON(ctx, c.twoFactorURL(credentialRef, path), payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTwoFactorServiceUnavailable, err)
	}
	return response, nil
}

func (c *VaultClient) twoFactorURL(credentialRef uuid.UUID, path string) string {
	return c.twoFactorCredentialURL + "/" + credentialRef.String() + path
}

func (c *VaultClient) authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	request.Header.Set("Accept", "application/json")
}

func decodeTwoFactorProblem(response *http.Response) error {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	var problem struct {
		Code    string    `json:"code"`
		RetryAt time.Time `json:"retry_at"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit)).Decode(&problem)
	switch {
	case response.StatusCode == http.StatusBadRequest:
		return ErrInvalidInput
	case response.StatusCode == http.StatusUnauthorized:
		return ErrTwoFactorVerification
	case response.StatusCode == http.StatusTooManyRequests && !problem.RetryAt.IsZero():
		return &LoginThrottleError{RetryAt: problem.RetryAt.UTC()}
	case response.StatusCode == http.StatusConflict && problem.Code == "two_factor_already_enabled":
		return ErrTwoFactorAlreadyEnabled
	case response.StatusCode == http.StatusConflict && problem.Code == "two_factor_not_enabled":
		return ErrTwoFactorNotEnabled
	case response.StatusCode == http.StatusConflict && problem.Code == "idempotency_conflict":
		return ErrTwoFactorIdempotencyConflict
	case response.StatusCode == http.StatusNotFound:
		return ErrTwoFactorEnrollmentNotFound
	case response.StatusCode == http.StatusGone:
		return ErrRecoveryCodeBundleUnavailable
	default:
		return fmt.Errorf("%w: vault returned status %d", ErrTwoFactorServiceUnavailable, response.StatusCode)
	}
}

func (c *VaultClient) postJSON(ctx context.Context, endpoint string, payload []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	c.authorize(request)
	request.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(request)
}

// Verify implements CredentialVerifier.
func (c *VaultClient) Verify(ctx context.Context, input LoginInput) (uuid.UUID, error) {
	return c.verifyCredential(ctx, c.verifyURL, input)
}

// VerifyForAccountAppeal proves a restricted account's credentials without
// using the ordinary login endpoint or creating a Core session.
func (c *VaultClient) VerifyForAccountAppeal(ctx context.Context, input LoginInput) (uuid.UUID, error) {
	return c.verifyCredential(ctx, c.verifyAccountAppealURL, input)
}

func (c *VaultClient) verifyCredential(ctx context.Context, endpoint string, input LoginInput) (uuid.UUID, error) {
	payload := struct {
		Identifier       string `json:"identifier"`
		Password         string `json:"password"`
		SecondFactorCode string `json:"second_factor_code,omitempty"`
	}{
		Identifier:       input.Identifier,
		Password:         input.Password,
		SecondFactorCode: input.SecondFactorCode,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("encode vault verification request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return uuid.Nil, fmt.Errorf("create vault verification request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrCredentialVerifierUnavailable, err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		var problem struct {
			RetryAt time.Time `json:"retry_at"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit)).Decode(&problem)
		if problem.RetryAt.IsZero() {
			return uuid.Nil, fmt.Errorf("%w: vault returned an invalid throttle response", ErrCredentialVerifierUnavailable)
		}
		return uuid.Nil, &LoginThrottleError{RetryAt: problem.RetryAt.UTC()}
	}
	if response.StatusCode == http.StatusPreconditionRequired {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		return uuid.Nil, ErrSecondFactorRequired
	}
	if response.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		return uuid.Nil, ErrInvalidCredentials
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
		return uuid.Nil, fmt.Errorf("%w: vault returned status %d", ErrCredentialVerifierUnavailable, response.StatusCode)
	}

	var result struct {
		CredentialRef string `json:"credential_ref"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode vault decision: %v", ErrCredentialVerifierUnavailable, err)
	}
	credentialRef, err := uuid.Parse(result.CredentialRef)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: vault returned an invalid credential reference", ErrCredentialVerifierUnavailable)
	}
	return credentialRef, nil
}

// EnableAfterAccountAppeal is a service-authenticated, idempotent lifecycle
// command. Core calls it only after a staff decision preflight and still keeps
// the account disabled locally until its own transaction commits.
func (c *VaultClient) EnableAfterAccountAppeal(ctx context.Context, credentialRef uuid.UUID) error {
	if credentialRef == uuid.Nil {
		return ErrInvalidInput
	}
	endpoint := c.twoFactorCredentialURL + "/" + credentialRef.String() + "/enable-after-account-appeal"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Vault credential enable request: %w", err)
	}
	c.authorize(request)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCredentialVerifierUnavailable, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, vaultResponseLimit))
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode == http.StatusNotFound {
		return ErrInvalidCredentials
	}
	return fmt.Errorf("%w: Vault credential enable returned status %d", ErrCredentialVerifierUnavailable, response.StatusCode)
}

// ProvisionRegistration implements RegistrationCredentialVault. Direct
// identifiers and the password are sent only to the service-authenticated
// Vault endpoint over the configured bounded client and are never logged.
func (c *VaultClient) ProvisionRegistration(ctx context.Context, input RegistrationInput) (uuid.UUID, error) {
	payload := struct {
		RegistrationID uuid.UUID `json:"registration_id"`
		Username       string    `json:"username"`
		Email          string    `json:"email"`
		Password       string    `json:"password"`
	}{input.ID, input.Username, input.Email, input.Password}
	return c.registrationRequest(ctx, c.provisionURL, payload, true)
}

// ActivateRegistration makes the provision usable only after Core has stored
// its pending identity row. Redirects remain blocked by the shared client.
func (c *VaultClient) ActivateRegistration(ctx context.Context, registrationID uuid.UUID) (uuid.UUID, error) {
	return c.registrationRequest(ctx, c.activateURL+"/"+registrationID.String()+"/activate", nil, false)
}

func (c *VaultClient) registrationRequest(ctx context.Context, endpoint string, payload any, hasBody bool) (uuid.UUID, error) {
	var body io.Reader
	if hasBody {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return uuid.Nil, fmt.Errorf("encode vault registration request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create vault registration request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	request.Header.Set("Accept", "application/json")
	if hasBody {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrRegistrationServiceUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit)).Decode(&problem)
		switch {
		case response.StatusCode == http.StatusBadRequest:
			return uuid.Nil, ErrInvalidInput
		case response.StatusCode == http.StatusConflict && problem.Code == "idempotency_conflict":
			return uuid.Nil, ErrRegistrationIdempotencyConflict
		case response.StatusCode == http.StatusConflict:
			return uuid.Nil, ErrRegistrationUnavailable
		case response.StatusCode == http.StatusNotFound:
			return uuid.Nil, ErrRegistrationStateConflict
		default:
			return uuid.Nil, fmt.Errorf("%w: vault returned status %d", ErrRegistrationServiceUnavailable, response.StatusCode)
		}
	}
	var result struct {
		CredentialRef string `json:"credential_ref"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, vaultResponseLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode vault registration response", ErrRegistrationServiceUnavailable)
	}
	credentialRef, err := uuid.Parse(result.CredentialRef)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: vault returned an invalid credential reference", ErrRegistrationServiceUnavailable)
	}
	return credentialRef, nil
}

var _ CredentialVerifier = (*VaultClient)(nil)
var _ RegistrationCredentialVault = (*VaultClient)(nil)
var _ EmailVerificationVault = (*VaultClient)(nil)
var _ PasswordRecoveryVault = (*VaultClient)(nil)
var _ TwoFactorVault = (*VaultClient)(nil)
var _ TrackerCredentialVault = (*VaultClient)(nil)
