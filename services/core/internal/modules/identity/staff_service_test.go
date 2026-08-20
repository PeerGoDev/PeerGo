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

func TestStaffServiceCreatesAnIndependentBoundAndShortLivedSession(t *testing.T) {
	t.Parallel()

	fixture := newStaffServiceFixture(t)
	ctx := context.Background()

	options, err := fixture.service.BeginElevation(ctx, fixture.webCookie, fixture.webCSRF)
	if err != nil {
		t.Fatalf("BeginElevation() error = %v", err)
	}
	if options.ChallengeID != fixture.challengeID || options.ExpiresAt != fixture.now.Add(5*time.Minute) {
		t.Fatalf("BeginElevation() = %+v", options)
	}
	if len(fixture.authorizer.calls) != 1 {
		t.Fatalf("Authorize() calls = %d, want 1", len(fixture.authorizer.calls))
	}
	assertStaffAuthorizationRequest(
		t,
		fixture.authorizer.calls[0],
		authz.ActionStaffSessionCreateSelf,
		authz.AudienceWebSession,
		fixture.user.ID,
		time.Time{},
		authz.AuthorityBinding{},
	)
	storedChallenge := fixture.repository.challenge
	if !bytes.Equal(storedChallenge.ParentTokenHash, fixture.webTokenHash) {
		t.Fatal("BeginElevation() did not bind the challenge to the Web session digest")
	}
	if bytes.Contains(storedChallenge.Protected.Ciphertext, fixture.ceremony.sessionData) {
		t.Fatal("BeginElevation() persisted plaintext WebAuthn SessionData")
	}
	openedSession, err := fixture.protector.Open(
		staffChallengeRecordKind,
		fixture.user.ID,
		fixture.challengeID[:],
		storedChallenge.Protected,
	)
	if err != nil || !bytes.Equal(openedSession, fixture.ceremony.sessionData) {
		t.Fatalf("protected SessionData did not round trip: plaintext=%q err=%v", openedSession, err)
	}

	assertion := json.RawMessage(`{"id":"credential-one"}`)
	staffSession, err := fixture.service.CompleteElevation(ctx, fixture.webCookie, fixture.webCSRF, CompleteStaffElevationInput{
		ChallengeID: fixture.challengeID,
		Assertion:   assertion,
	})
	if err != nil {
		t.Fatalf("CompleteElevation() error = %v", err)
	}
	if fixture.ceremony.finishCalls != 1 || !bytes.Equal(fixture.ceremony.finishSessionData, fixture.ceremony.sessionData) || !bytes.Equal(fixture.ceremony.finishAssertion, assertion) {
		t.Fatal("CompleteElevation() did not replay the exact protected ceremony state and assertion")
	}
	if fixture.repository.creation == nil {
		t.Fatal("CompleteElevation() did not persist a staff session")
	}
	wantStaffHash := sha256.Sum256(fixture.staffRawToken)
	if !bytes.Equal(fixture.repository.creation.TokenHash, wantStaffHash[:]) {
		t.Fatal("CompleteElevation() did not persist the independent staff token digest")
	}
	if bytes.Equal(fixture.repository.creation.TokenHash, fixture.webTokenHash) || fixture.repository.creation.ParentTokenHash == nil {
		t.Fatal("CompleteElevation() reused or failed to bind the ordinary Web token")
	}
	if !bytes.Equal(fixture.repository.creation.ParentTokenHash, fixture.webTokenHash) {
		t.Fatal("CompleteElevation() stored the wrong parent Web token digest")
	}
	if !bytes.Equal(fixture.repository.creation.StaffCredentialID, fixture.credentialID) {
		t.Fatal("CompleteElevation() stored the wrong verified credential ID")
	}
	if fixture.repository.creation.Authority != fixture.authorizer.authority {
		t.Fatalf("persisted authority = %+v, want %+v", fixture.repository.creation.Authority, fixture.authorizer.authority)
	}
	updatedRecord, err := fixture.protector.Open(
		staffCredentialRecordKind,
		fixture.user.ID,
		fixture.credentialID,
		fixture.repository.creation.CredentialRecord,
	)
	if err != nil || !bytes.Equal(updatedRecord, fixture.ceremony.finishResult.Record) {
		t.Fatalf("updated credential record did not round trip: record=%q err=%v", updatedRecord, err)
	}
	wantExpiry := fixture.now.Add(8 * time.Minute)
	if staffSession.ExpiresAt != wantExpiry || fixture.repository.creation.ExpiresAt != wantExpiry {
		t.Fatalf("staff expiry = %v persisted=%v, want authority-capped %v", staffSession.ExpiresAt, fixture.repository.creation.ExpiresAt, wantExpiry)
	}
	wantCookie := base64.RawURLEncoding.EncodeToString(fixture.staffRawToken)
	if staffSession.CookieToken != wantCookie || staffSession.CookieToken == fixture.webCookie {
		t.Fatal("CompleteElevation() did not return an independent staff cookie token")
	}
	if staffSession.CSRFToken == "" || staffSession.CSRFToken == fixture.webCSRF {
		t.Fatal("staff CSRF token was empty or reused the Web CSRF domain")
	}

	_, err = fixture.service.CompleteElevation(ctx, fixture.webCookie, fixture.webCSRF, CompleteStaffElevationInput{
		ChallengeID: fixture.challengeID,
		Assertion:   assertion,
	})
	if !errors.Is(err, ErrStaffChallengeNotFound) {
		t.Fatalf("CompleteElevation(replay) error = %v, want ErrStaffChallengeNotFound", err)
	}
	if fixture.ceremony.finishCalls != 1 {
		t.Fatal("a replay reached WebAuthn verification")
	}

	current, err := fixture.service.CurrentSession(ctx, staffSession.CookieToken)
	if err != nil {
		t.Fatalf("CurrentSession() error = %v", err)
	}
	if current.User.ID != fixture.user.ID || current.CSRFToken != staffSession.CSRFToken {
		t.Fatalf("CurrentSession() = %+v", current)
	}
	if current.Authority != fixture.repository.creation.Authority {
		t.Fatalf("CurrentSession() authority = %+v, want %+v", current.Authority, fixture.repository.creation.Authority)
	}
	lastRequest := fixture.authorizer.calls[len(fixture.authorizer.calls)-1]
	assertStaffAuthorizationRequest(
		t,
		lastRequest,
		authz.ActionStaffSessionReadSelf,
		authz.AudienceStaffSession,
		fixture.user.ID,
		fixture.now,
		fixture.repository.creation.Authority,
	)
	if _, err := fixture.service.CurrentSession(ctx, fixture.webCookie); !errors.Is(err, ErrStaffSessionNotFound) {
		t.Fatalf("CurrentSession(Web cookie) error = %v, want ErrStaffSessionNotFound", err)
	}
	if err := fixture.service.Logout(ctx, staffSession.CookieToken, fixture.webCSRF); !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("Logout(Web csrf) error = %v, want ErrInvalidCSRF", err)
	}
	if fixture.repository.revoked {
		t.Fatal("Logout(Web csrf) revoked the staff session")
	}
	if err := fixture.service.Logout(ctx, staffSession.CookieToken, staffSession.CSRFToken); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := fixture.service.CurrentSession(ctx, staffSession.CookieToken); !errors.Is(err, ErrStaffSessionNotFound) {
		t.Fatalf("CurrentSession(after logout) error = %v, want ErrStaffSessionNotFound", err)
	}
}

func TestStaffSessionAuthorityChangeInvalidatesUseButNotLogout(t *testing.T) {
	t.Parallel()

	fixture := newStaffServiceFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.BeginElevation(ctx, fixture.webCookie, fixture.webCSRF); err != nil {
		t.Fatalf("BeginElevation() error = %v", err)
	}
	staffSession, err := fixture.service.CompleteElevation(ctx, fixture.webCookie, fixture.webCSRF, CompleteStaffElevationInput{
		ChallengeID: fixture.challengeID,
		Assertion:   json.RawMessage(`{"id":"credential-one"}`),
	})
	if err != nil {
		t.Fatalf("CompleteElevation() error = %v", err)
	}
	issuedAuthority := fixture.repository.creation.Authority
	fixture.authorizer.authority.GrantVersion++

	if _, err := fixture.service.CurrentSession(ctx, staffSession.CookieToken); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("CurrentSession(after grant version change) error = %v, want authz.ErrForbidden", err)
	}
	lastRequest := fixture.authorizer.calls[len(fixture.authorizer.calls)-1]
	if lastRequest.Context.RequiredAuthority != issuedAuthority {
		t.Fatalf("CurrentSession() required authority = %+v, want issued %+v", lastRequest.Context.RequiredAuthority, issuedAuthority)
	}
	if fixture.repository.revoked {
		t.Fatal("authority denial silently revoked the row before explicit logout")
	}
	if err := fixture.service.Logout(ctx, staffSession.CookieToken, staffSession.CSRFToken); err != nil {
		t.Fatalf("Logout(after authority change) error = %v", err)
	}
	if !fixture.repository.revoked {
		t.Fatal("Logout(after authority change) did not revoke the staff session")
	}
}

func TestStaffServiceFailsClosedBeforeCreatingAuthority(t *testing.T) {
	t.Parallel()

	t.Run("missing credential", func(t *testing.T) {
		t.Parallel()
		fixture := newStaffServiceFixture(t)
		fixture.repository.credentials = nil
		_, err := fixture.service.BeginElevation(context.Background(), fixture.webCookie, fixture.webCSRF)
		if !errors.Is(err, ErrStaffCredentialRequired) {
			t.Fatalf("BeginElevation() error = %v, want ErrStaffCredentialRequired", err)
		}
		if fixture.ceremony.beginCalls != 0 || fixture.repository.challengeLive {
			t.Fatal("missing credentials created ceremony state")
		}
	})

	t.Run("denied decision without adapter error", func(t *testing.T) {
		t.Parallel()
		fixture := newStaffServiceFixture(t)
		fixture.authorizer.allow = false
		_, err := fixture.service.BeginElevation(context.Background(), fixture.webCookie, fixture.webCSRF)
		if !errors.Is(err, authz.ErrForbidden) {
			t.Fatalf("BeginElevation() error = %v, want authz.ErrForbidden", err)
		}
		if fixture.ceremony.beginCalls != 0 || fixture.repository.challengeLive {
			t.Fatal("a denied authorization decision created ceremony state")
		}
	})

	t.Run("clone warning", func(t *testing.T) {
		t.Parallel()
		fixture := newStaffServiceFixture(t)
		if _, err := fixture.service.BeginElevation(context.Background(), fixture.webCookie, fixture.webCSRF); err != nil {
			t.Fatalf("BeginElevation() error = %v", err)
		}
		fixture.ceremony.finishResult.CloneWarning = true
		_, err := fixture.service.CompleteElevation(
			context.Background(),
			fixture.webCookie,
			fixture.webCSRF,
			CompleteStaffElevationInput{ChallengeID: fixture.challengeID, Assertion: json.RawMessage(`{}`)},
		)
		if !errors.Is(err, ErrStaffAuthenticatorCloneDetected) {
			t.Fatalf("CompleteElevation() error = %v, want clone detection", err)
		}
		if fixture.repository.creation != nil {
			t.Fatal("clone warning created a staff session")
		}
		if fixture.repository.challengeLive {
			t.Fatal("failed assertion challenge remained replayable")
		}
	})
}

type staffServiceFixture struct {
	now           time.Time
	user          User
	credentialID  []byte
	challengeID   uuid.UUID
	webCookie     string
	webCSRF       string
	webTokenHash  []byte
	staffRawToken []byte
	protector     *RecordProtector
	repository    *memoryStaffRepository
	ceremony      *recordingStaffCeremony
	authorizer    *recordingStaffAuthorizer
	service       *StaffService
}

func newStaffServiceFixture(t *testing.T) *staffServiceFixture {
	t.Helper()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
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

	protectorEntropy := make([]byte, 12*4)
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
	credentialID := []byte("credential-one")
	initialCredential := []byte(`{"counter":1,"public_key":"secret"}`)
	protectedCredential, err := protector.Seal(staffCredentialRecordKind, user.ID, credentialID, initialCredential)
	if err != nil {
		t.Fatalf("protect test credential: %v", err)
	}
	repository := &memoryStaffRepository{
		user: user,
		credentials: []StaffWebAuthnCredential{{
			ID:        append([]byte(nil), credentialID...),
			UserID:    user.ID,
			Protected: protectedCredential,
		}},
	}
	ceremony := &recordingStaffCeremony{
		publicKey:   json.RawMessage(`{"challenge":"c2VydmVyLWNoYWxsZW5nZQ","timeout":300000,"rpId":"peergo.test","allowCredentials":[{"type":"public-key","id":"Y3JlZGVudGlhbC1vbmU"}],"userVerification":"required"}`),
		sessionData: []byte(`{"challenge":"server-only-session"}`),
		expiresAt:   now.Add(5 * time.Minute),
		finishResult: StaffWebAuthnResult{
			CredentialID: append([]byte(nil), credentialID...),
			Record:       []byte(`{"counter":2,"public_key":"secret"}`),
		},
	}
	authorizer := &recordingStaffAuthorizer{
		allow:          true,
		effectiveUntil: now.Add(8 * time.Minute),
		authority: authz.AuthorityBinding{
			GrantID:      uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444"),
			GrantVersion: 7,
			MandateID:    uuid.MustParse("0198f20a-6da8-7e51-9c64-666666666666"),
		},
		roleID: "staff_access",
	}
	challengeID := uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333")
	staffRawToken := bytes.Repeat([]byte{0x51}, randomTokenBytes)
	staffService, err := NewStaffService(webSessions, repository, ceremony, protector, authorizer, StaffServiceConfig{
		CSRFKey:         []byte("fedcba9876543210fedcba9876543210"),
		SessionDuration: 15 * time.Minute,
		Now:             func() time.Time { return now },
		Random:          bytes.NewReader(staffRawToken),
		NewChallengeID:  func() uuid.UUID { return challengeID },
	})
	if err != nil {
		t.Fatalf("NewStaffService() error = %v", err)
	}

	return &staffServiceFixture{
		now:           now,
		user:          user,
		credentialID:  credentialID,
		challengeID:   challengeID,
		webCookie:     base64.RawURLEncoding.EncodeToString(webRawToken),
		webCSRF:       webSessions.csrfToken(webRawToken),
		webTokenHash:  append([]byte(nil), webDigest[:]...),
		staffRawToken: staffRawToken,
		protector:     protector,
		repository:    repository,
		ceremony:      ceremony,
		authorizer:    authorizer,
		service:       staffService,
	}
}

type memoryStaffRepository struct {
	user          User
	credentials   []StaffWebAuthnCredential
	challenge     StaffWebAuthnChallenge
	challengeLive bool
	creation      *StaffSessionCreation
	session       StaffSessionRecord
	revoked       bool
}

func (repository *memoryStaffRepository) ListActiveStaffWebAuthnCredentials(context.Context, uuid.UUID) ([]StaffWebAuthnCredential, error) {
	return append([]StaffWebAuthnCredential(nil), repository.credentials...), nil
}

func (repository *memoryStaffRepository) CreateStaffWebAuthnChallenge(_ context.Context, challenge StaffWebAuthnChallenge) error {
	repository.challenge = challenge
	repository.challengeLive = true
	return nil
}

func (repository *memoryStaffRepository) ConsumeStaffWebAuthnChallenge(_ context.Context, challengeID, userID uuid.UUID, parentTokenHash []byte, asOf time.Time) (StaffWebAuthnChallenge, error) {
	if !repository.challengeLive ||
		repository.challenge.ID != challengeID ||
		repository.challenge.UserID != userID ||
		!bytes.Equal(repository.challenge.ParentTokenHash, parentTokenHash) ||
		!asOf.Before(repository.challenge.ExpiresAt) {
		return StaffWebAuthnChallenge{}, ErrStaffChallengeNotFound
	}
	repository.challengeLive = false
	return repository.challenge, nil
}

func (repository *memoryStaffRepository) CreateStaffSession(_ context.Context, creation StaffSessionCreation) (time.Time, error) {
	copyOfCreation := creation
	repository.creation = &copyOfCreation
	repository.session = StaffSessionRecord{
		TokenHash:               append([]byte(nil), creation.TokenHash...),
		ParentTokenHash:         append([]byte(nil), creation.ParentTokenHash...),
		StaffCredentialID:       append([]byte(nil), creation.StaffCredentialID...),
		Authority:               creation.Authority,
		User:                    repository.user,
		CreatedAt:               creation.CreatedAt,
		ExpiresAt:               creation.ExpiresAt,
		WebAuthnAuthenticatedAt: creation.WebAuthnAuthenticatedAt,
	}
	return creation.ExpiresAt, nil
}

func (repository *memoryStaffRepository) ActiveStaffSession(_ context.Context, tokenHash []byte, asOf time.Time) (StaffSessionRecord, error) {
	if repository.creation == nil || repository.revoked || !bytes.Equal(repository.session.TokenHash, tokenHash) || !asOf.Before(repository.session.ExpiresAt) {
		return StaffSessionRecord{}, ErrStaffSessionNotFound
	}
	return repository.session, nil
}

func (repository *memoryStaffRepository) RevokeStaffSession(_ context.Context, tokenHash []byte, _ time.Time) error {
	if repository.revoked || !bytes.Equal(repository.session.TokenHash, tokenHash) {
		return ErrStaffSessionNotFound
	}
	repository.revoked = true
	return nil
}

type recordingStaffCeremony struct {
	publicKey         json.RawMessage
	sessionData       []byte
	expiresAt         time.Time
	finishResult      StaffWebAuthnResult
	beginCalls        int
	finishCalls       int
	beginMaterials    []StaffCredentialMaterial
	finishSessionData []byte
	finishAssertion   json.RawMessage
}

func (ceremony *recordingStaffCeremony) Begin(_ User, materials []StaffCredentialMaterial) (json.RawMessage, []byte, time.Time, error) {
	ceremony.beginCalls++
	ceremony.beginMaterials = append([]StaffCredentialMaterial(nil), materials...)
	return ceremony.publicKey, ceremony.sessionData, ceremony.expiresAt, nil
}

func (ceremony *recordingStaffCeremony) Finish(_ User, _ []StaffCredentialMaterial, sessionData []byte, assertion json.RawMessage) (StaffWebAuthnResult, error) {
	ceremony.finishCalls++
	ceremony.finishSessionData = append([]byte(nil), sessionData...)
	ceremony.finishAssertion = append(json.RawMessage(nil), assertion...)
	return ceremony.finishResult, nil
}

type recordingStaffAuthorizer struct {
	allow          bool
	effectiveUntil time.Time
	authority      authz.AuthorityBinding
	roleID         string
	calls          []authz.Request
}

func (authorizer *recordingStaffAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	authorizer.calls = append(authorizer.calls, request)
	decision := authz.Decision{
		Allow:          authorizer.allow,
		EffectiveUntil: authorizer.effectiveUntil,
		GrantID:        authorizer.authority.GrantID,
		GrantVersion:   authorizer.authority.GrantVersion,
		MandateID:      authorizer.authority.MandateID,
		RoleID:         authorizer.roleID,
	}
	if request.Context.RequiredAuthority.IsValid() && !request.Context.RequiredAuthority.Matches(decision.GrantID, decision.GrantVersion, decision.MandateID) {
		decision.Allow = false
		decision.Reason = authz.ReasonAuthorityBindingMismatch
		return decision, authz.DeniedError{Decision: decision}
	}
	return decision, nil
}

func assertStaffAuthorizationRequest(t *testing.T, request authz.Request, action authz.Action, audience authz.CredentialAudience, userID uuid.UUID, mfaAt time.Time, requiredAuthority authz.AuthorityBinding) {
	t.Helper()
	if request.Action != action || request.CredentialAudience != audience {
		t.Fatalf("authorization request action=%q audience=%q", request.Action, request.CredentialAudience)
	}
	if request.Subject.ID != userID || request.Resource.OwnerID != userID || request.Resource.Scope != authz.SiteScope() {
		t.Fatalf("authorization request identity/scope = %+v", request)
	}
	if request.Context.MFAAuthenticatedAt != mfaAt {
		t.Fatalf("authorization request MFA time = %v, want %v", request.Context.MFAAuthenticatedAt, mfaAt)
	}
	if request.Context.RequiredAuthority != requiredAuthority {
		t.Fatalf("authorization request authority = %+v, want %+v", request.Context.RequiredAuthority, requiredAuthority)
	}
}
