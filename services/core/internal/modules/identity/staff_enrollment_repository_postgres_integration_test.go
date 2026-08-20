package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestPostgresStaffEnrollmentCommitsCredentialTicketAndAuditTogether(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	username := "staff-enroll-it-" + userID.String()[:8]
	webToken := bytes.Repeat([]byte{0x54}, 32)
	webTokenHash := sha256.Sum256(webToken)
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, 'active')`, userID, uuid.New(), username); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.sessions (
    token_hash, user_id, audience, created_at, last_seen_at, expires_at
) VALUES ($1, $2, 'web', $3, $3, $4)`, webTokenHash[:], userID, now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert Web session: %v", err)
	}
	var issuedTicketID uuid.UUID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if issuedTicketID != uuid.Nil {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit.outbox WHERE payload_json::jsonb ->> 'ticket_id' = $1`, issuedTicketID.String())
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.staff_webauthn_credentials WHERE bootstrap_ticket_id = $1`, issuedTicketID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.staff_webauthn_enrollment_challenges WHERE ticket_id = $1`, issuedTicketID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.staff_credential_bootstrap_tickets WHERE id = $1`, issuedTicketID)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.sessions WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM identity.users WHERE id = $1`, userID)
	})

	eventBuilder, err := audit.NewStaffBootstrapEventBuilder(audit.RecorderConfig{
		PseudonymKey:      bytes.Repeat([]byte{0x46}, 32),
		PseudonymKeyEpoch: "integration-2026-08",
	})
	if err != nil {
		t.Fatalf("NewStaffBootstrapEventBuilder() error = %v", err)
	}
	repository, err := identity.NewPostgresStaffEnrollmentRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		t.Fatalf("NewPostgresStaffEnrollmentRepository() error = %v", err)
	}
	issuer, err := identity.NewStaffBootstrapIssuer(repository, identity.StaffBootstrapIssuerConfig{
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewStaffBootstrapIssuer() error = %v", err)
	}
	operatorReference := "integration-change-" + uuid.NewString()
	ticket, err := issuer.Issue(ctx, identity.IssueStaffBootstrapTicketInput{
		Username:          username,
		OperatorReference: operatorReference,
		Lifetime:          15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	issuedTicketID = ticket.ID
	rawTicket, err := base64.RawURLEncoding.DecodeString(ticket.RawToken)
	if err != nil {
		t.Fatalf("decode issued ticket: %v", err)
	}
	ticketHash := sha256.Sum256(rawTicket)

	protector, err := identity.NewRecordProtector(
		[]byte("0123456789abcdef0123456789abcdef"),
		"integration-epoch",
		bytes.NewReader(bytes.Repeat([]byte{0x29}, 24)),
	)
	if err != nil {
		t.Fatalf("NewRecordProtector() error = %v", err)
	}
	challengeID := uuid.New()
	protectedChallenge, err := protector.Seal("integration-enrollment-challenge", userID, challengeID[:], []byte(`{"challenge":"server-only"}`))
	if err != nil {
		t.Fatalf("protect challenge: %v", err)
	}
	challenge, err := repository.CreateStaffEnrollmentChallenge(ctx, ticketHash[:], identity.StaffWebAuthnEnrollmentChallenge{
		ID:              challengeID,
		UserID:          userID,
		ParentTokenHash: webTokenHash[:],
		Label:           "Integration passkey",
		Protected:       protectedChallenge,
		CreatedAt:       now,
		ExpiresAt:       now.Add(5 * time.Minute),
	}, now)
	if err != nil {
		t.Fatalf("CreateStaffEnrollmentChallenge() error = %v", err)
	}
	if challenge.TicketID != ticket.ID {
		t.Fatalf("challenge ticket = %s, want %s", challenge.TicketID, ticket.ID)
	}
	consumed, err := repository.ConsumeStaffEnrollmentChallenge(ctx, challengeID, userID, webTokenHash[:], ticketHash[:], now.Add(time.Second))
	if err != nil || consumed.ConsumedAt == nil {
		t.Fatalf("ConsumeStaffEnrollmentChallenge() challenge=%+v error=%v", consumed, err)
	}

	credentialID := []byte("integration-credential-" + uuid.NewString())
	protectedCredential, err := protector.Seal("integration-staff-credential", userID, credentialID, []byte(`{"public_key":"server-only"}`))
	if err != nil {
		t.Fatalf("protect credential: %v", err)
	}
	decision := authz.Decision{
		ID:             uuid.New(),
		Allow:          true,
		Reason:         authz.ReasonAllowed,
		PolicyVersion:  authz.PolicyVersion,
		GrantID:        uuid.New(),
		GrantVersion:   1,
		RoleID:         "staff_access",
		MandateID:      uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
	if err := repository.CreateStaffCredentialEnrollment(ctx, identity.CreateStaffCredentialEnrollmentCommand{
		TicketID:         ticket.ID,
		TokenHash:        ticketHash[:],
		ChallengeID:      challengeID,
		ParentTokenHash:  webTokenHash[:],
		UserID:           userID,
		CredentialID:     credentialID,
		CredentialRecord: protectedCredential,
		Label:            challenge.Label,
		CreatedAt:        now.Add(2 * time.Second),
		Authorization:    decision,
	}); err != nil {
		t.Fatalf("CreateStaffCredentialEnrollment() error = %v", err)
	}

	var consumedAt time.Time
	var bootstrapTicketID uuid.UUID
	if err := pool.QueryRow(ctx, `
SELECT ticket.consumed_at, credential.bootstrap_ticket_id
FROM identity.staff_credential_bootstrap_tickets AS ticket
JOIN identity.staff_webauthn_credentials AS credential
  ON credential.bootstrap_ticket_id = ticket.id
WHERE ticket.id = $1`, ticket.ID).Scan(&consumedAt, &bootstrapTicketID); err != nil {
		t.Fatalf("read committed enrollment: %v", err)
	}
	if consumedAt.IsZero() || bootstrapTicketID != ticket.ID {
		t.Fatalf("consumed_at=%s bootstrap_ticket_id=%s", consumedAt, bootstrapTicketID)
	}
	var eventCount int
	var leakedEvidence bool
	if err := pool.QueryRow(ctx, `
SELECT count(*), bool_or(
    payload_json LIKE '%' || $2 || '%'
    OR payload_json LIKE '%' || $3 || '%'
    OR payload_json LIKE '%' || $4 || '%'
)
FROM audit.outbox
WHERE event_type = $1
  AND payload_json::jsonb ->> 'ticket_id' = $5`,
		audit.StaffBootstrapEventType,
		ticket.RawToken,
		operatorReference,
		string(credentialID),
		ticket.ID.String(),
	).Scan(&eventCount, &leakedEvidence); err != nil {
		t.Fatalf("read enrollment audit events: %v", err)
	}
	if eventCount != 2 || leakedEvidence {
		t.Fatalf("audit event count=%d leaked evidence=%t", eventCount, leakedEvidence)
	}

	_, err = repository.ConsumeStaffEnrollmentChallenge(ctx, challengeID, userID, webTokenHash[:], ticketHash[:], now.Add(3*time.Second))
	if !errors.Is(err, identity.ErrStaffEnrollmentChallengeNotFound) {
		t.Fatalf("replay error = %v, want consumed challenge denial", err)
	}
}
