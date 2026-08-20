package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

func TestGoWebAuthnCeremonyBeginsKnownUserVerificationRequiredAssertion(t *testing.T) {
	t.Parallel()

	ceremony, err := NewGoWebAuthnCeremony(
		"peergo.test",
		"PeerGo Staff",
		[]string{"https://peergo.test"},
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewGoWebAuthnCeremony() error = %v", err)
	}
	user := User{
		ID:          uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		Username:    "staff-demo",
		DisplayName: "Staff Demo",
	}
	credentialID := []byte("credential-one")
	credentialRecord, err := json.Marshal(gowebauthn.Credential{
		ID:        credentialID,
		PublicKey: []byte{0xa5, 0x01, 0x02},
		Transport: []protocol.AuthenticatorTransport{protocol.USB},
	})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	startedAt := time.Now()
	publicKey, encodedSession, expiresAt, err := ceremony.Begin(user, []StaffCredentialMaterial{{
		ID:     credentialID,
		Record: credentialRecord,
	}})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	var options protocol.PublicKeyCredentialRequestOptions
	if err := json.Unmarshal(publicKey, &options); err != nil {
		t.Fatalf("decode public options: %v", err)
	}
	if options.RelyingPartyID != "peergo.test" || options.UserVerification != protocol.VerificationRequired {
		t.Fatalf("public options RP=%q UV=%q", options.RelyingPartyID, options.UserVerification)
	}
	if options.Timeout != int((5*time.Minute).Milliseconds()) || len(options.Challenge) < protocol.MinimumChallengeLength {
		t.Fatalf("public options timeout=%d challenge bytes=%d", options.Timeout, len(options.Challenge))
	}
	if len(options.AllowedCredentials) != 1 || !bytes.Equal(options.AllowedCredentials[0].CredentialID, credentialID) {
		t.Fatalf("allowed credentials = %+v", options.AllowedCredentials)
	}

	var session gowebauthn.SessionData
	if err := json.Unmarshal(encodedSession, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if !bytes.Equal(session.UserID, user.ID[:]) || session.UserVerification != protocol.VerificationRequired {
		t.Fatalf("protected session user=%x UV=%q", session.UserID, session.UserVerification)
	}
	if session.RelyingPartyID != options.RelyingPartyID || session.Challenge != options.Challenge.String() {
		t.Fatal("protected session and public options describe different ceremonies")
	}
	if !expiresAt.Equal(session.Expires.UTC()) || expiresAt.Before(startedAt.Add(4*time.Minute+59*time.Second)) || expiresAt.After(time.Now().Add(5*time.Minute+time.Second)) {
		t.Fatalf("enforced expiry = %v", expiresAt)
	}
	if ceremony.provider.Config.RPAllowCrossOrigin {
		t.Fatal("staff WebAuthn unexpectedly permits cross-origin assertions")
	}
}

func TestGoWebAuthnCeremonyRejectsCredentialAndSessionInvariantFailures(t *testing.T) {
	t.Parallel()

	user := User{
		ID:          uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		Username:    "staff-demo",
		DisplayName: "Staff Demo",
	}
	credentialRecord, err := json.Marshal(gowebauthn.Credential{ID: []byte("credential-one")})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if _, err := newWebAuthnUser(user, []StaffCredentialMaterial{{ID: []byte("another-id"), Record: credentialRecord}}); err == nil {
		t.Fatal("newWebAuthnUser() accepted a lookup ID different from the protected record")
	}
	if _, err := newWebAuthnUser(user, []StaffCredentialMaterial{{ID: []byte("credential-one"), Record: []byte("not-json")}}); err == nil {
		t.Fatal("newWebAuthnUser() accepted a malformed protected credential record")
	}
	if _, err := NewGoWebAuthnCeremony("peergo.test", "PeerGo Staff", []string{"https://peergo.test"}, 30*time.Second); err == nil {
		t.Fatal("NewGoWebAuthnCeremony() accepted an assertion timeout below one minute")
	}
	if _, err := NewGoWebAuthnCeremony("peergo.test", "PeerGo Staff", []string{"https://unrelated.test"}, time.Minute); err == nil {
		t.Fatal("NewGoWebAuthnCeremony() accepted an origin outside its RP ID")
	}
	if _, err := NewGoWebAuthnCeremony("peergo.test", "PeerGo Staff", []string{"http://peergo.test"}, time.Minute); err == nil {
		t.Fatal("NewGoWebAuthnCeremony() accepted an insecure non-localhost origin")
	}
	if _, err := NewGoWebAuthnCeremony("peergo.test", "PeerGo Staff", []string{"%"}, time.Minute); err == nil {
		t.Fatal("NewGoWebAuthnCeremony() accepted a malformed origin")
	}

	ceremony, err := NewGoWebAuthnCeremony("peergo.test", "PeerGo Staff", []string{"https://peergo.test"}, time.Minute)
	if err != nil {
		t.Fatalf("NewGoWebAuthnCeremony() error = %v", err)
	}
	material := []StaffCredentialMaterial{{ID: []byte("credential-one"), Record: credentialRecord}}
	_, encodedSession, _, err := ceremony.Begin(user, material)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	var session gowebauthn.SessionData
	if err := json.Unmarshal(encodedSession, &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	session.UserVerification = protocol.VerificationPreferred
	unsafeSession, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("encode unsafe session: %v", err)
	}
	if _, err := ceremony.Finish(user, material, unsafeSession, json.RawMessage(`{}`)); err == nil || errors.Is(err, ErrStaffWebAuthnVerification) {
		t.Fatalf("Finish(unsafe protected session) error = %v, want internal invariant failure", err)
	}
	if _, err := ceremony.Finish(user, material, encodedSession, json.RawMessage(`{}`)); !errors.Is(err, ErrStaffWebAuthnVerification) {
		t.Fatalf("Finish(malformed assertion) error = %v, want ErrStaffWebAuthnVerification", err)
	}
}
