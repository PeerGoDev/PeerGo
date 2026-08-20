package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	defaultStaffBootstrapLifetime = 15 * time.Minute
	maxStaffRegistrationBytes     = 64 * 1024
)

type StaffEnrollmentRepository interface {
	IssueStaffBootstrapTicket(context.Context, IssueStaffBootstrapTicketCommand) (StaffBootstrapTicket, string, error)
	CreateStaffEnrollmentChallenge(context.Context, []byte, StaffWebAuthnEnrollmentChallenge, time.Time) (StaffWebAuthnEnrollmentChallenge, error)
	ConsumeStaffEnrollmentChallenge(context.Context, uuid.UUID, uuid.UUID, []byte, []byte, time.Time) (StaffWebAuthnEnrollmentChallenge, error)
	CreateStaffCredentialEnrollment(context.Context, CreateStaffCredentialEnrollmentCommand) error
}

type staffCredentialReader interface {
	ListActiveStaffWebAuthnCredentials(context.Context, uuid.UUID) ([]StaffWebAuthnCredential, error)
}

type StaffBootstrapIssuerConfig struct {
	Now         func() time.Time
	Random      io.Reader
	NewTicketID func() uuid.UUID
}

// StaffBootstrapIssuer is an operator-only use case composed by the CLI. It
// does not expose an HTTP route and never persists or logs the raw token.
type StaffBootstrapIssuer struct {
	repository  StaffEnrollmentRepository
	now         func() time.Time
	random      io.Reader
	newTicketID func() uuid.UUID
}

func NewStaffBootstrapIssuer(repository StaffEnrollmentRepository, config StaffBootstrapIssuerConfig) (*StaffBootstrapIssuer, error) {
	if repository == nil {
		return nil, errors.New("staff bootstrap repository is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.NewTicketID == nil {
		config.NewTicketID = uuid.New
	}
	return &StaffBootstrapIssuer{
		repository:  repository,
		now:         config.Now,
		random:      config.Random,
		newTicketID: config.NewTicketID,
	}, nil
}

func (issuer *StaffBootstrapIssuer) Issue(ctx context.Context, input IssueStaffBootstrapTicketInput) (IssuedStaffBootstrapTicket, error) {
	username := strings.TrimSpace(input.Username)
	operatorReference := strings.TrimSpace(input.OperatorReference)
	if !utf8.ValidString(username) || utf8.RuneCountInString(username) < 1 || utf8.RuneCountInString(username) > 64 ||
		!utf8.ValidString(operatorReference) || utf8.RuneCountInString(operatorReference) < 3 || utf8.RuneCountInString(operatorReference) > 200 {
		return IssuedStaffBootstrapTicket{}, ErrInvalidInput
	}
	lifetime := input.Lifetime
	if lifetime == 0 {
		lifetime = defaultStaffBootstrapLifetime
	}
	if lifetime < 5*time.Minute || lifetime > 30*time.Minute {
		return IssuedStaffBootstrapTicket{}, ErrInvalidInput
	}
	ticketID := issuer.newTicketID()
	if ticketID == uuid.Nil {
		return IssuedStaffBootstrapTicket{}, errors.New("staff bootstrap ticket ID generator returned nil")
	}
	_, tokenHash, rawToken, err := newSessionToken(issuer.random)
	if err != nil {
		return IssuedStaffBootstrapTicket{}, fmt.Errorf("generate staff bootstrap token: %w", err)
	}
	operatorDigest := sha256.Sum256([]byte(operatorReference))
	now := issuer.now().UTC()
	ticket, persistedUsername, err := issuer.repository.IssueStaffBootstrapTicket(ctx, IssueStaffBootstrapTicketCommand{
		TargetUsername:    username,
		OperatorReference: operatorReference,
		Ticket: StaffBootstrapTicket{
			ID:                      ticketID,
			TokenHash:               tokenHash,
			OperatorReferenceSHA256: operatorDigest[:],
			CreatedAt:               now,
			ExpiresAt:               now.Add(lifetime),
		},
	})
	if err != nil {
		return IssuedStaffBootstrapTicket{}, err
	}
	return IssuedStaffBootstrapTicket{
		ID:        ticket.ID,
		Username:  persistedUsername,
		RawToken:  rawToken,
		CreatedAt: ticket.CreatedAt,
		ExpiresAt: ticket.ExpiresAt,
	}, nil
}

type StaffEnrollmentServiceConfig struct {
	Now            func() time.Time
	NewChallengeID func() uuid.UUID
}

// StaffEnrollmentService coordinates the browser half of controlled staff
// credential bootstrap. A valid Web session, CSRF token, typed permission and
// the one-time operator ticket are all required independently.
type StaffEnrollmentService struct {
	webSessions    *Service
	repository     StaffEnrollmentRepository
	credentials    staffCredentialReader
	ceremony       StaffWebAuthnEnrollmentCeremony
	protector      *RecordProtector
	authorizer     staffAuthorizer
	now            func() time.Time
	newChallengeID func() uuid.UUID
}

func NewStaffEnrollmentService(webSessions *Service, repository StaffEnrollmentRepository, credentials staffCredentialReader, ceremony StaffWebAuthnEnrollmentCeremony, protector *RecordProtector, authorizer staffAuthorizer, config StaffEnrollmentServiceConfig) (*StaffEnrollmentService, error) {
	if webSessions == nil || repository == nil || credentials == nil || ceremony == nil || protector == nil || authorizer == nil {
		return nil, errors.New("staff enrollment dependencies are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewChallengeID == nil {
		config.NewChallengeID = uuid.New
	}
	return &StaffEnrollmentService{
		webSessions:    webSessions,
		repository:     repository,
		credentials:    credentials,
		ceremony:       ceremony,
		protector:      protector,
		authorizer:     authorizer,
		now:            config.Now,
		newChallengeID: config.NewChallengeID,
	}, nil
}

func (service *StaffEnrollmentService) Begin(ctx context.Context, webCookie, csrfToken string, input BeginStaffEnrollmentInput) (StaffEnrollmentOptions, error) {
	label, tokenHash, err := validateStaffEnrollmentInput(input.Label, input.BootstrapToken)
	if err != nil {
		return StaffEnrollmentOptions{}, err
	}
	webSession, err := service.webSessions.AuthenticateWrite(ctx, webCookie, csrfToken)
	if err != nil {
		return StaffEnrollmentOptions{}, err
	}
	now := service.now().UTC()
	if _, err := authorizeStaffIdentity(ctx, service.authorizer, webSession.User, authz.ActionStaffCredentialEnrollSelf, authz.AudienceWebSession, time.Time{}, now, authz.AuthorityBinding{}); err != nil {
		return StaffEnrollmentOptions{}, err
	}
	materials, err := loadStaffCredentialMaterials(ctx, service.credentials, service.protector, webSession.User, false)
	if err != nil {
		return StaffEnrollmentOptions{}, err
	}
	publicKey, sessionData, providerExpiry, err := service.ceremony.BeginEnrollment(webSession.User, materials)
	if err != nil {
		return StaffEnrollmentOptions{}, err
	}
	if !providerExpiry.After(now) || providerExpiry.After(now.Add(10*time.Minute)) {
		return StaffEnrollmentOptions{}, errors.New("staff WebAuthn provider returned an invalid registration expiry")
	}
	challengeID := service.newChallengeID()
	if challengeID == uuid.Nil {
		return StaffEnrollmentOptions{}, errors.New("staff enrollment challenge ID generator returned nil")
	}
	protected, err := service.protector.Seal(staffEnrollmentChallengeRecordKind, webSession.User.ID, challengeID[:], sessionData)
	if err != nil {
		return StaffEnrollmentOptions{}, fmt.Errorf("protect staff enrollment session: %w", err)
	}
	challenge, err := service.repository.CreateStaffEnrollmentChallenge(ctx, tokenHash, StaffWebAuthnEnrollmentChallenge{
		ID:              challengeID,
		UserID:          webSession.User.ID,
		ParentTokenHash: append([]byte(nil), webSession.TokenHash...),
		Label:           label,
		Protected:       protected,
		CreatedAt:       now,
		ExpiresAt:       providerExpiry,
	}, now)
	if err != nil {
		return StaffEnrollmentOptions{}, err
	}
	return StaffEnrollmentOptions{ChallengeID: challenge.ID, ExpiresAt: challenge.ExpiresAt, PublicKey: publicKey}, nil
}

func (service *StaffEnrollmentService) Complete(ctx context.Context, webCookie, csrfToken string, input CompleteStaffEnrollmentInput) (StaffCredentialEnrollment, error) {
	if input.ChallengeID == uuid.Nil || len(input.Credential) == 0 || len(input.Credential) > maxStaffRegistrationBytes || !json.Valid(input.Credential) {
		return StaffCredentialEnrollment{}, ErrInvalidInput
	}
	tokenHash, err := staffBootstrapTokenHash(input.BootstrapToken)
	if err != nil {
		return StaffCredentialEnrollment{}, err
	}
	webSession, err := service.webSessions.AuthenticateWrite(ctx, webCookie, csrfToken)
	if err != nil {
		return StaffCredentialEnrollment{}, err
	}
	now := service.now().UTC()
	decision, err := authorizeStaffIdentity(ctx, service.authorizer, webSession.User, authz.ActionStaffCredentialEnrollSelf, authz.AudienceWebSession, time.Time{}, now, authz.AuthorityBinding{})
	if err != nil {
		return StaffCredentialEnrollment{}, err
	}
	challenge, err := service.repository.ConsumeStaffEnrollmentChallenge(ctx, input.ChallengeID, webSession.User.ID, webSession.TokenHash, tokenHash, now)
	if err != nil {
		return StaffCredentialEnrollment{}, err
	}
	sessionData, err := service.protector.Open(staffEnrollmentChallengeRecordKind, webSession.User.ID, challenge.ID[:], challenge.Protected)
	if err != nil {
		return StaffCredentialEnrollment{}, fmt.Errorf("open staff enrollment session: %w", err)
	}
	result, err := service.ceremony.FinishEnrollment(webSession.User, sessionData, input.Credential)
	if err != nil {
		if errors.Is(err, ErrStaffEnrollmentVerification) {
			return StaffCredentialEnrollment{}, ErrStaffEnrollmentVerification
		}
		return StaffCredentialEnrollment{}, err
	}
	if len(result.CredentialID) == 0 || result.CloneWarning {
		return StaffCredentialEnrollment{}, ErrStaffEnrollmentVerification
	}
	protectedCredential, err := service.protector.Seal(staffCredentialRecordKind, webSession.User.ID, result.CredentialID, result.Record)
	if err != nil {
		return StaffCredentialEnrollment{}, fmt.Errorf("protect enrolled staff credential: %w", err)
	}
	command := CreateStaffCredentialEnrollmentCommand{
		TicketID:         challenge.TicketID,
		TokenHash:        tokenHash,
		ChallengeID:      challenge.ID,
		ParentTokenHash:  append([]byte(nil), webSession.TokenHash...),
		UserID:           webSession.User.ID,
		CredentialID:     append([]byte(nil), result.CredentialID...),
		CredentialRecord: protectedCredential,
		Label:            challenge.Label,
		CreatedAt:        now,
		Authorization:    decision,
	}
	if err := service.repository.CreateStaffCredentialEnrollment(ctx, command); err != nil {
		return StaffCredentialEnrollment{}, err
	}
	return StaffCredentialEnrollment{CredentialID: command.CredentialID, Label: command.Label, EnrolledAt: now}, nil
}

func validateStaffEnrollmentInput(label, token string) (string, []byte, error) {
	label = strings.TrimSpace(label)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) < 1 || utf8.RuneCountInString(label) > 80 {
		return "", nil, ErrInvalidInput
	}
	tokenHash, err := staffBootstrapTokenHash(token)
	if err != nil {
		return "", nil, err
	}
	return label, tokenHash, nil
}

func staffBootstrapTokenHash(encoded string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != randomTokenBytes || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, ErrStaffBootstrapTicketInvalid
	}
	digest := sha256.Sum256(raw)
	return append([]byte(nil), digest[:]...), nil
}

func loadStaffCredentialMaterials(ctx context.Context, repository staffCredentialReader, protector *RecordProtector, user User, required bool) ([]StaffCredentialMaterial, error) {
	records, err := repository.ListActiveStaffWebAuthnCredentials(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("load staff WebAuthn credentials: %w", err)
	}
	if required && len(records) == 0 {
		return nil, ErrStaffCredentialRequired
	}
	materials := make([]StaffCredentialMaterial, 0, len(records))
	for _, record := range records {
		if record.UserID != user.ID {
			return nil, errors.New("staff WebAuthn credential owner invariant failed")
		}
		plaintext, err := protector.Open(staffCredentialRecordKind, user.ID, record.ID, record.Protected)
		if err != nil {
			return nil, fmt.Errorf("open staff WebAuthn credential: %w", err)
		}
		materials = append(materials, StaffCredentialMaterial{ID: append([]byte(nil), record.ID...), Record: plaintext})
	}
	return materials, nil
}

func authorizeStaffIdentity(ctx context.Context, authorizer staffAuthorizer, user User, action authz.Action, audience authz.CredentialAudience, mfaAt, now time.Time, requiredAuthority authz.AuthorityBinding) (authz.Decision, error) {
	decision, err := authorizer.Authorize(ctx, authz.Request{
		Subject:            authz.Subject{ID: user.ID, Status: authz.SubjectActive},
		Action:             action,
		CredentialAudience: audience,
		Resource:           authz.Resource{OwnerID: user.ID, Scope: authz.SiteScope()},
		Context: authz.EvaluationContext{
			Now:                now,
			MFAAuthenticatedAt: mfaAt,
			RequiredAuthority:  requiredAuthority,
		},
	})
	if err != nil {
		return decision, err
	}
	if !decision.Allow || !decision.EffectiveUntil.After(now) {
		return decision, authz.ErrForbidden
	}
	decisionAuthority := decision.AuthorityBinding()
	if !decisionAuthority.IsValid() || decision.RoleID == "" {
		return decision, errors.New("staff authorization decision is missing authority evidence")
	}
	if !requiredAuthority.IsZero() && !requiredAuthority.Matches(decision.GrantID, decision.GrantVersion, decision.MandateID) {
		return decision, authz.ErrForbidden
	}
	return decision, nil
}
