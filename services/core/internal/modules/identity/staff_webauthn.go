package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
)

// StaffCredentialMaterial is the decrypted, complete WebAuthn credential
// record used only in request memory while beginning or verifying a ceremony.
type StaffCredentialMaterial struct {
	ID     []byte
	Record []byte
}

// StaffWebAuthnResult carries the library-updated complete credential record.
// Persisting it after every success is required for signature-counter and
// backup-state checks on the next assertion.
type StaffWebAuthnResult struct {
	CredentialID []byte
	Record       []byte
	CloneWarning bool
}

// StaffWebAuthnCeremony hides the third-party protocol types from the service
// and persistence ports while keeping the complete SessionData opaque.
type StaffWebAuthnCeremony interface {
	Begin(User, []StaffCredentialMaterial) (publicKey json.RawMessage, sessionData []byte, expiresAt time.Time, err error)
	Finish(User, []StaffCredentialMaterial, []byte, json.RawMessage) (StaffWebAuthnResult, error)
}

// StaffWebAuthnEnrollmentCeremony is kept separate from assertion so service
// fakes and callers cannot accidentally accept a registration response where
// a privileged assertion is required.
type StaffWebAuthnEnrollmentCeremony interface {
	BeginEnrollment(User, []StaffCredentialMaterial) (publicKey json.RawMessage, sessionData []byte, expiresAt time.Time, err error)
	FinishEnrollment(User, []byte, json.RawMessage) (StaffWebAuthnResult, error)
}

// GoWebAuthnCeremony is the production FIDO2/WebAuthn relying-party adapter.
type GoWebAuthnCeremony struct {
	provider *gowebauthn.WebAuthn
}

// NewGoWebAuthnCeremony configures a same-origin, user-verifying assertion
// flow. Cross-origin ceremonies remain disabled and server-side timeouts are
// enforced even if a user agent ignores the advertised timeout.
func NewGoWebAuthnCeremony(rpID, displayName string, origins []string, timeout time.Duration) (*GoWebAuthnCeremony, error) {
	if timeout < time.Minute || timeout > 10*time.Minute {
		return nil, errors.New("staff WebAuthn timeout must be between one and ten minutes")
	}
	if err := validateStaffWebAuthnOrigins(rpID, origins); err != nil {
		return nil, err
	}
	provider, err := gowebauthn.New(&gowebauthn.Config{
		RPID:               rpID,
		RPDisplayName:      displayName,
		RPOrigins:          append([]string(nil), origins...),
		RPAllowCrossOrigin: false,
		Timeouts: gowebauthn.TimeoutsConfig{
			Login: gowebauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    timeout,
				TimeoutUVD: timeout,
			},
			Registration: gowebauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    timeout,
				TimeoutUVD: timeout,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("configure staff WebAuthn relying party: %w", err)
	}
	return &GoWebAuthnCeremony{provider: provider}, nil
}

func (ceremony *GoWebAuthnCeremony) BeginEnrollment(user User, materials []StaffCredentialMaterial) (json.RawMessage, []byte, time.Time, error) {
	waUser, err := newWebAuthnEnrollmentUser(user, materials)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	creation, session, err := ceremony.provider.BeginRegistration(
		waUser,
		gowebauthn.WithExclusions(gowebauthn.Credentials(waUser.WebAuthnCredentials()).CredentialDescriptors()),
		gowebauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired,
		}),
		gowebauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		gowebauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("begin staff WebAuthn registration: %w", err)
	}
	if session.UserVerification != protocol.VerificationRequired || session.Expires.IsZero() {
		return nil, nil, time.Time{}, errors.New("staff WebAuthn provider returned unsafe registration session data")
	}
	publicKey, err := json.Marshal(creation.Response)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("encode staff WebAuthn registration options: %w", err)
	}
	sessionData, err := json.Marshal(session)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("encode staff WebAuthn registration session: %w", err)
	}
	return publicKey, sessionData, session.Expires.UTC(), nil
}

func (ceremony *GoWebAuthnCeremony) FinishEnrollment(user User, encodedSession []byte, response json.RawMessage) (StaffWebAuthnResult, error) {
	waUser, err := newWebAuthnEnrollmentUser(user, nil)
	if err != nil {
		return StaffWebAuthnResult{}, err
	}
	var session gowebauthn.SessionData
	if err := json.Unmarshal(encodedSession, &session); err != nil {
		return StaffWebAuthnResult{}, errors.New("decode protected staff WebAuthn registration session")
	}
	if session.UserVerification != protocol.VerificationRequired || !bytes.Equal(session.UserID, waUser.WebAuthnID()) {
		return StaffWebAuthnResult{}, errors.New("protected staff WebAuthn registration session invariant failed")
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return StaffWebAuthnResult{}, ErrStaffEnrollmentVerification
	}
	credential, err := ceremony.provider.CreateCredential(waUser, session, parsed)
	if err != nil || credential == nil || !credential.Flags.UserVerified {
		return StaffWebAuthnResult{}, ErrStaffEnrollmentVerification
	}
	encodedCredential, err := json.Marshal(credential)
	if err != nil {
		return StaffWebAuthnResult{}, fmt.Errorf("encode enrolled staff WebAuthn credential: %w", err)
	}
	return StaffWebAuthnResult{
		CredentialID: append([]byte(nil), credential.ID...),
		Record:       encodedCredential,
		CloneWarning: credential.Authenticator.CloneWarning,
	}, nil
}

func validateStaffWebAuthnOrigins(rpID string, origins []string) error {
	canonicalRPID := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rpID), "."))
	if canonicalRPID == "" || canonicalRPID != rpID || len(origins) == 0 {
		return errors.New("staff WebAuthn RP ID and origins must be canonical")
	}
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed == nil {
			return fmt.Errorf("staff WebAuthn origin %q is invalid", origin)
		}
		hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return fmt.Errorf("staff WebAuthn origin %q is invalid", origin)
		}
		if parsed.Scheme == "http" && hostname != "localhost" {
			return fmt.Errorf("staff WebAuthn origin %q must use https outside localhost", origin)
		}
		// A valid RP ID is either the origin host itself or a registrable parent
		// selected on a dot boundary. The WebAuthn library validates the domain
		// syntax; this relation check catches a configuration that browsers would
		// reject before Core opens its listener.
		if hostname != canonicalRPID && !strings.HasSuffix(hostname, "."+canonicalRPID) {
			return fmt.Errorf("staff WebAuthn origin %q is outside RP ID %q", origin, canonicalRPID)
		}
	}
	return nil
}

func (ceremony *GoWebAuthnCeremony) Begin(user User, materials []StaffCredentialMaterial) (json.RawMessage, []byte, time.Time, error) {
	waUser, err := newWebAuthnUser(user, materials)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	assertion, session, err := ceremony.provider.BeginLogin(
		waUser,
		gowebauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("begin staff WebAuthn assertion: %w", err)
	}
	if session.UserVerification != protocol.VerificationRequired || session.Expires.IsZero() {
		return nil, nil, time.Time{}, errors.New("staff WebAuthn provider returned unsafe session data")
	}
	publicKey, err := json.Marshal(assertion.Response)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("encode staff WebAuthn options: %w", err)
	}
	sessionData, err := json.Marshal(session)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("encode staff WebAuthn session: %w", err)
	}
	return publicKey, sessionData, session.Expires.UTC(), nil
}

func (ceremony *GoWebAuthnCeremony) Finish(user User, materials []StaffCredentialMaterial, encodedSession []byte, assertion json.RawMessage) (StaffWebAuthnResult, error) {
	waUser, err := newWebAuthnUser(user, materials)
	if err != nil {
		return StaffWebAuthnResult{}, err
	}
	var session gowebauthn.SessionData
	if err := json.Unmarshal(encodedSession, &session); err != nil {
		return StaffWebAuthnResult{}, errors.New("decode protected staff WebAuthn session")
	}
	if session.UserVerification != protocol.VerificationRequired || !bytes.Equal(session.UserID, waUser.WebAuthnID()) {
		return StaffWebAuthnResult{}, errors.New("protected staff WebAuthn session invariant failed")
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(assertion)
	if err != nil {
		return StaffWebAuthnResult{}, ErrStaffWebAuthnVerification
	}
	credential, err := ceremony.provider.ValidateLogin(waUser, session, parsed)
	if err != nil {
		return StaffWebAuthnResult{}, ErrStaffWebAuthnVerification
	}
	if credential == nil || !credential.Flags.UserVerified {
		return StaffWebAuthnResult{}, ErrStaffWebAuthnVerification
	}
	encodedCredential, err := json.Marshal(credential)
	if err != nil {
		return StaffWebAuthnResult{}, fmt.Errorf("encode updated staff WebAuthn credential: %w", err)
	}
	return StaffWebAuthnResult{
		CredentialID: append([]byte(nil), credential.ID...),
		Record:       encodedCredential,
		CloneWarning: credential.Authenticator.CloneWarning,
	}, nil
}

type webAuthnUser struct {
	user        User
	credentials []gowebauthn.Credential
}

func newWebAuthnUser(user User, materials []StaffCredentialMaterial) (webAuthnUser, error) {
	if user.ID == [16]byte{} || len(materials) == 0 || len(materials) > 32 {
		return webAuthnUser{}, ErrStaffCredentialRequired
	}
	return decodeWebAuthnUser(user, materials)
}

func newWebAuthnEnrollmentUser(user User, materials []StaffCredentialMaterial) (webAuthnUser, error) {
	if user.ID == [16]byte{} || len(materials) > 32 {
		return webAuthnUser{}, ErrInvalidInput
	}
	return decodeWebAuthnUser(user, materials)
}

func decodeWebAuthnUser(user User, materials []StaffCredentialMaterial) (webAuthnUser, error) {
	credentials := make([]gowebauthn.Credential, 0, len(materials))
	for _, material := range materials {
		var credential gowebauthn.Credential
		if err := json.Unmarshal(material.Record, &credential); err != nil {
			return webAuthnUser{}, errors.New("decode protected staff WebAuthn credential")
		}
		if len(material.ID) == 0 || !bytes.Equal(material.ID, credential.ID) {
			return webAuthnUser{}, errors.New("protected staff WebAuthn credential ID mismatch")
		}
		credentials = append(credentials, credential)
	}
	return webAuthnUser{user: user, credentials: credentials}, nil
}

func (user webAuthnUser) WebAuthnID() []byte {
	return append([]byte(nil), user.user.ID[:]...)
}

func (user webAuthnUser) WebAuthnName() string {
	return user.user.Username
}

func (user webAuthnUser) WebAuthnDisplayName() string {
	return user.user.DisplayName
}

func (user webAuthnUser) WebAuthnCredentials() []gowebauthn.Credential {
	return append([]gowebauthn.Credential(nil), user.credentials...)
}

var _ StaffWebAuthnCeremony = (*GoWebAuthnCeremony)(nil)
var _ StaffWebAuthnEnrollmentCeremony = (*GoWebAuthnCeremony)(nil)
var _ gowebauthn.User = webAuthnUser{}
