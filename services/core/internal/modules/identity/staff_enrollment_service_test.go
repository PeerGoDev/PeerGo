package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

func TestStaffBootstrapIssuerReturnsRawTokenOnceAndPersistsItsDigest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.UTC)
	ticketID := uuid.New()
	tokenBytes := bytes.Repeat([]byte{0x72}, randomTokenBytes)
	repository := &memoryStaffEnrollmentRepository{targetUserID: uuid.New()}
	issuer, err := NewStaffBootstrapIssuer(repository, StaffBootstrapIssuerConfig{
		Now:         func() time.Time { return now },
		Random:      bytes.NewReader(tokenBytes),
		NewTicketID: func() uuid.UUID { return ticketID },
	})
	if err != nil {
		t.Fatalf("NewStaffBootstrapIssuer() error = %v", err)
	}

	issued, err := issuer.Issue(context.Background(), IssueStaffBootstrapTicketInput{
		Username:          " demo ",
		OperatorReference: " local-change-42 ",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	wantRawToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	if issued.ID != ticketID || issued.Username != "demo" || issued.RawToken != wantRawToken || issued.ExpiresAt != now.Add(defaultStaffBootstrapLifetime) {
		t.Fatalf("Issue() = %+v", issued)
	}
	wantTokenHash := sha256.Sum256(tokenBytes)
	wantOperatorHash := sha256.Sum256([]byte("local-change-42"))
	command := repository.issueCommand
	if command.TargetUsername != "demo" || command.OperatorReference != "local-change-42" || !bytes.Equal(command.Ticket.TokenHash, wantTokenHash[:]) || !bytes.Equal(command.Ticket.OperatorReferenceSHA256, wantOperatorHash[:]) {
		t.Fatalf("persisted issue command = %+v", command)
	}
	if bytes.Contains(command.Ticket.TokenHash, []byte(issued.RawToken)) {
		t.Fatal("persisted ticket digest contains the raw token")
	}
}

func TestStaffEnrollmentConsumesChallengeBeforeVerificationAndProtectsCredential(t *testing.T) {
	t.Parallel()

	fixture := newStaffEnrollmentFixture(t)
	ctx := context.Background()
	options, err := fixture.service.Begin(ctx, fixture.webCookie, fixture.webCSRF, BeginStaffEnrollmentInput{
		BootstrapToken: fixture.bootstrapToken,
		Label:          " Mac passkey ",
	})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if options.ChallengeID != fixture.challengeID || options.ExpiresAt != fixture.now.Add(5*time.Minute) {
		t.Fatalf("Begin() = %+v", options)
	}
	assertStaffAuthorizationRequest(
		t,
		fixture.authorizer.calls[0],
		authz.ActionStaffCredentialEnrollSelf,
		authz.AudienceWebSession,
		fixture.user.ID,
		time.Time{},
		authz.AuthorityBinding{},
	)
	challenge := fixture.repository.challenge
	if challenge.Label != "Mac passkey" || challenge.TicketID != fixture.ticketID || !bytes.Equal(challenge.ParentTokenHash, fixture.webTokenHash) {
		t.Fatalf("persisted challenge = %+v", challenge)
	}
	if bytes.Contains(challenge.Protected.Ciphertext, fixture.ceremony.sessionData) {
		t.Fatal("registration SessionData was stored in plaintext")
	}
	openedSession, err := fixture.protector.Open(staffEnrollmentChallengeRecordKind, fixture.user.ID, fixture.challengeID[:], challenge.Protected)
	if err != nil || !bytes.Equal(openedSession, fixture.ceremony.sessionData) {
		t.Fatalf("protected registration SessionData = %q, error = %v", openedSession, err)
	}

	registration := json.RawMessage(`{"id":"new-staff-credential"}`)
	result, err := fixture.service.Complete(ctx, fixture.webCookie, fixture.webCSRF, CompleteStaffEnrollmentInput{
		BootstrapToken: fixture.bootstrapToken,
		ChallengeID:    fixture.challengeID,
		Credential:     registration,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if !fixture.ceremony.challengeConsumedAtFinish {
		t.Fatal("WebAuthn verification ran before the one-time challenge was consumed")
	}
	if result.Label != "Mac passkey" || !bytes.Equal(result.CredentialID, fixture.ceremony.result.CredentialID) || result.EnrolledAt != fixture.now {
		t.Fatalf("Complete() = %+v", result)
	}
	if fixture.repository.creation == nil {
		t.Fatal("Complete() did not finalize a credential enrollment")
	}
	creation := fixture.repository.creation
	if creation.TicketID != fixture.ticketID || creation.ChallengeID != fixture.challengeID || creation.UserID != fixture.user.ID || !bytes.Equal(creation.ParentTokenHash, fixture.webTokenHash) {
		t.Fatalf("enrollment creation binding = %+v", creation)
	}
	if bytes.Contains(creation.CredentialRecord.Ciphertext, fixture.ceremony.result.Record) {
		t.Fatal("complete WebAuthn credential record was stored in plaintext")
	}
	openedCredential, err := fixture.protector.Open(staffCredentialRecordKind, fixture.user.ID, fixture.ceremony.result.CredentialID, creation.CredentialRecord)
	if err != nil || !bytes.Equal(openedCredential, fixture.ceremony.result.Record) {
		t.Fatalf("protected credential = %q, error = %v", openedCredential, err)
	}

	_, err = fixture.service.Complete(ctx, fixture.webCookie, fixture.webCSRF, CompleteStaffEnrollmentInput{
		BootstrapToken: fixture.bootstrapToken,
		ChallengeID:    fixture.challengeID,
		Credential:     registration,
	})
	if !errors.Is(err, ErrStaffEnrollmentChallengeNotFound) {
		t.Fatalf("Complete(replay) error = %v, want ErrStaffEnrollmentChallengeNotFound", err)
	}
	if fixture.ceremony.finishCalls != 1 {
		t.Fatal("replayed registration reached WebAuthn verification")
	}
}

func TestStaffEnrollmentRejectsMalformedTicketBeforeSessionOrCeremony(t *testing.T) {
	t.Parallel()

	fixture := newStaffEnrollmentFixture(t)
	_, err := fixture.service.Begin(context.Background(), fixture.webCookie, fixture.webCSRF, BeginStaffEnrollmentInput{
		BootstrapToken: "not-a-ticket",
		Label:          "Mac passkey",
	})
	if !errors.Is(err, ErrStaffBootstrapTicketInvalid) {
		t.Fatalf("Begin() error = %v, want ErrStaffBootstrapTicketInvalid", err)
	}
	if len(fixture.authorizer.calls) != 0 || fixture.ceremony.beginCalls != 0 || fixture.repository.challengeLive {
		t.Fatal("malformed ticket reached authenticated enrollment work")
	}
}

type staffEnrollmentFixture struct {
	now            time.Time
	user           User
	ticketID       uuid.UUID
	challengeID    uuid.UUID
	bootstrapToken string
	webCookie      string
	webCSRF        string
	webTokenHash   []byte
	protector      *RecordProtector
	repository     *memoryStaffEnrollmentRepository
	ceremony       *recordingStaffEnrollmentCeremony
	authorizer     *recordingStaffAuthorizer
	service        *StaffEnrollmentService
}

func newStaffEnrollmentFixture(t *testing.T) *staffEnrollmentFixture {
	t.Helper()

	now := time.Date(2026, time.August, 5, 14, 0, 0, 0, time.UTC)
	user := User{
		ID:            uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		CredentialRef: uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222"),
		Username:      "demo",
		DisplayName:   "PeerGo Demo",
	}
	webRawToken := bytes.Repeat([]byte{0x41}, randomTokenBytes)
	webDigest := sha256.Sum256(webRawToken)
	webRepository := &memoryRepository{user: user, session: SessionRecord{
		TokenHash: append([]byte(nil), webDigest[:]...),
		User:      user,
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(20 * time.Minute),
	}}
	webSessions, err := NewService(webRepository, fixedVerifier{reference: user.CredentialRef}, ServiceConfig{
		CSRFKey: []byte("0123456789abcdef0123456789abcdef"),
		Now:     func() time.Time { return now },
		Random:  bytes.NewReader(bytes.Repeat([]byte{0x42}, randomTokenBytes)),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	protectorEntropy := make([]byte, 24)
	for index := range protectorEntropy {
		protectorEntropy[index] = byte(index + 1)
	}
	protector, err := NewRecordProtector(
		[]byte("abcdef0123456789abcdef0123456789"),
		"test-epoch",
		bytes.NewReader(protectorEntropy),
	)
	if err != nil {
		t.Fatalf("NewRecordProtector() error = %v", err)
	}
	bootstrapRaw := bytes.Repeat([]byte{0x61}, randomTokenBytes)
	bootstrapDigest := sha256.Sum256(bootstrapRaw)
	ticketID := uuid.MustParse("0198f20a-6da8-7e51-9c64-777777777777")
	challengeID := uuid.MustParse("0198f20a-6da8-7e51-9c64-888888888888")
	repository := &memoryStaffEnrollmentRepository{
		targetUserID: user.ID,
		ticketID:     ticketID,
		tokenHash:    append([]byte(nil), bootstrapDigest[:]...),
	}
	ceremony := &recordingStaffEnrollmentCeremony{
		publicKey:   json.RawMessage(`{"rp":{"id":"peergo.test","name":"PeerGo"},"user":{"id":"dXNlcg","name":"demo","displayName":"Demo"},"challenge":"Y2hhbGxlbmdl","pubKeyCredParams":[{"type":"public-key","alg":-7}],"timeout":300000,"authenticatorSelection":{"residentKey":"preferred","requireResidentKey":false,"userVerification":"required"},"attestation":"none"}`),
		sessionData: []byte(`{"challenge":"server-only-registration"}`),
		expiresAt:   now.Add(5 * time.Minute),
		result: StaffWebAuthnResult{
			CredentialID: []byte("new-staff-credential"),
			Record:       []byte(`{"counter":0,"public_key":"secret"}`),
		},
		repository: repository,
	}
	authorizer := &recordingStaffAuthorizer{
		allow:          true,
		effectiveUntil: now.Add(time.Hour),
		authority: authz.AuthorityBinding{
			GrantID:      uuid.New(),
			GrantVersion: 2,
			MandateID:    uuid.New(),
		},
		roleID: "staff_access",
	}
	service, err := NewStaffEnrollmentService(
		webSessions,
		repository,
		repository,
		ceremony,
		protector,
		authorizer,
		StaffEnrollmentServiceConfig{
			Now:            func() time.Time { return now },
			NewChallengeID: func() uuid.UUID { return challengeID },
		},
	)
	if err != nil {
		t.Fatalf("NewStaffEnrollmentService() error = %v", err)
	}
	return &staffEnrollmentFixture{
		now:            now,
		user:           user,
		ticketID:       ticketID,
		challengeID:    challengeID,
		bootstrapToken: base64.RawURLEncoding.EncodeToString(bootstrapRaw),
		webCookie:      base64.RawURLEncoding.EncodeToString(webRawToken),
		webCSRF:        webSessions.csrfToken(webRawToken),
		webTokenHash:   append([]byte(nil), webDigest[:]...),
		protector:      protector,
		repository:     repository,
		ceremony:       ceremony,
		authorizer:     authorizer,
		service:        service,
	}
}

type memoryStaffEnrollmentRepository struct {
	targetUserID  uuid.UUID
	ticketID      uuid.UUID
	tokenHash     []byte
	issueCommand  IssueStaffBootstrapTicketCommand
	challenge     StaffWebAuthnEnrollmentChallenge
	challengeLive bool
	creation      *CreateStaffCredentialEnrollmentCommand
}

func (repository *memoryStaffEnrollmentRepository) IssueStaffBootstrapTicket(_ context.Context, command IssueStaffBootstrapTicketCommand) (StaffBootstrapTicket, string, error) {
	repository.issueCommand = command
	ticket := command.Ticket
	ticket.UserID = repository.targetUserID
	return ticket, command.TargetUsername, nil
}

func (repository *memoryStaffEnrollmentRepository) ListActiveStaffWebAuthnCredentials(context.Context, uuid.UUID) ([]StaffWebAuthnCredential, error) {
	return nil, nil
}

func (repository *memoryStaffEnrollmentRepository) CreateStaffEnrollmentChallenge(_ context.Context, tokenHash []byte, challenge StaffWebAuthnEnrollmentChallenge, _ time.Time) (StaffWebAuthnEnrollmentChallenge, error) {
	if !bytes.Equal(tokenHash, repository.tokenHash) {
		return StaffWebAuthnEnrollmentChallenge{}, ErrStaffBootstrapTicketInvalid
	}
	challenge.TicketID = repository.ticketID
	repository.challenge = challenge
	repository.challengeLive = true
	return challenge, nil
}

func (repository *memoryStaffEnrollmentRepository) ConsumeStaffEnrollmentChallenge(_ context.Context, challengeID, userID uuid.UUID, parentTokenHash, tokenHash []byte, asOf time.Time) (StaffWebAuthnEnrollmentChallenge, error) {
	if !repository.challengeLive || repository.challenge.ID != challengeID || repository.challenge.UserID != userID || !bytes.Equal(repository.challenge.ParentTokenHash, parentTokenHash) || !bytes.Equal(repository.tokenHash, tokenHash) || !asOf.Before(repository.challenge.ExpiresAt) {
		return StaffWebAuthnEnrollmentChallenge{}, ErrStaffEnrollmentChallengeNotFound
	}
	repository.challengeLive = false
	consumed := asOf
	repository.challenge.ConsumedAt = &consumed
	return repository.challenge, nil
}

func (repository *memoryStaffEnrollmentRepository) CreateStaffCredentialEnrollment(_ context.Context, command CreateStaffCredentialEnrollmentCommand) error {
	copyOfCommand := command
	repository.creation = &copyOfCommand
	return nil
}

type recordingStaffEnrollmentCeremony struct {
	publicKey                 json.RawMessage
	sessionData               []byte
	expiresAt                 time.Time
	result                    StaffWebAuthnResult
	repository                *memoryStaffEnrollmentRepository
	beginCalls                int
	finishCalls               int
	challengeConsumedAtFinish bool
}

func (ceremony *recordingStaffEnrollmentCeremony) BeginEnrollment(_ User, _ []StaffCredentialMaterial) (json.RawMessage, []byte, time.Time, error) {
	ceremony.beginCalls++
	return ceremony.publicKey, ceremony.sessionData, ceremony.expiresAt, nil
}

func (ceremony *recordingStaffEnrollmentCeremony) FinishEnrollment(_ User, sessionData []byte, _ json.RawMessage) (StaffWebAuthnResult, error) {
	ceremony.finishCalls++
	ceremony.challengeConsumedAtFinish = !ceremony.repository.challengeLive
	if !bytes.Equal(sessionData, ceremony.sessionData) {
		return StaffWebAuthnResult{}, errors.New("unexpected registration SessionData")
	}
	return ceremony.result, nil
}
