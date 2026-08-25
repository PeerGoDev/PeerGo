package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type registrationRepositoryFixture struct {
	mode           RegistrationMode
	prepareCommand PrepareRegistrationCommand
	record         RegistrationRecord
	prepareErr     error
	cancelErr      error
	cancelCalls    int
	attachCalls    int
	completeCalls  int
}

func (fixture *registrationRepositoryFixture) CancelRegistration(_ context.Context, registrationID uuid.UUID) error {
	fixture.cancelCalls++
	if registrationID != fixture.prepareCommand.ID {
		return ErrRegistrationStateConflict
	}
	return fixture.cancelErr
}

func (fixture *registrationRepositoryFixture) PublicRegistrationPolicy(context.Context) (RegistrationPublicPolicy, error) {
	return RegistrationPublicPolicy{
		Mode: fixture.mode, UsernameMinCharacters: 3, UsernameMaxCharacters: 32,
		EmailDomainMode: EmailDomainModeAny,
	}, nil
}

func (fixture *registrationRepositoryFixture) PrepareRegistration(_ context.Context, command PrepareRegistrationCommand) (RegistrationRecord, error) {
	fixture.prepareCommand = command
	if fixture.prepareErr != nil {
		return RegistrationRecord{}, fixture.prepareErr
	}
	record := fixture.record
	record.ID = command.ID
	record.UserID = command.UserID
	record.Username = command.Username
	record.DisplayName = command.DisplayName
	return record, nil
}

func (fixture *registrationRepositoryFixture) AttachRegistrationCredential(_ context.Context, registrationID, credentialRef uuid.UUID, _ time.Time) (RegistrationRecord, error) {
	fixture.attachCalls++
	record := fixture.record
	record.ID = registrationID
	record.CredentialRef = &credentialRef
	record.State = RegistrationStateCredentialProvisioned
	record.Username = fixture.prepareCommand.Username
	record.DisplayName = fixture.prepareCommand.DisplayName
	record.UserID = fixture.prepareCommand.UserID
	return record, nil
}

func (fixture *registrationRepositoryFixture) CompleteRegistration(_ context.Context, registrationID uuid.UUID, occurredAt time.Time) (RegistrationRecord, error) {
	fixture.completeCalls++
	record := fixture.record
	record.ID = registrationID
	record.UserID = fixture.prepareCommand.UserID
	record.Username = fixture.prepareCommand.Username
	record.DisplayName = fixture.prepareCommand.DisplayName
	record.State = RegistrationStateCompleted
	record.CompletedAt = &occurredAt
	return record, nil
}

type registrationVaultFixture struct {
	credentialRef uuid.UUID
	provisioned   RegistrationInput
	provisionErr  error
	activateCalls int
}

func (fixture *registrationVaultFixture) ProvisionRegistration(_ context.Context, input RegistrationInput) (uuid.UUID, error) {
	fixture.provisioned = input
	return fixture.credentialRef, fixture.provisionErr
}

func (fixture *registrationVaultFixture) ActivateRegistration(context.Context, uuid.UUID) (uuid.UUID, error) {
	fixture.activateCalls++
	return fixture.credentialRef, nil
}

func TestRegistrationServiceCompletesNormalizedInviteRegistration(t *testing.T) {
	now := time.Date(2026, time.August, 6, 8, 0, 0, 0, time.UTC)
	registrationID := uuid.MustParse("0198f20a-6da8-7e51-9c64-101010101010")
	credentialRef := uuid.MustParse("0198f20a-6da8-7e51-9c64-202020202020")
	token := "cGVlcmdvLWRldmVsb3BtZW50LWludml0ZS12MSEhISE"
	repository := &registrationRepositoryFixture{record: RegistrationRecord{
		Mode: RegistrationModeInvite, State: RegistrationStateReserved,
	}}
	vault := &registrationVaultFixture{credentialRef: credentialRef}
	service, err := NewRegistrationService(repository, vault, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewRegistrationService() error = %v", err)
	}

	result, err := service.Register(context.Background(), RegistrationInput{
		ID: registrationID, Username: " New_Member ", DisplayName: " 新成员 ",
		Email: " Member@Example.COM ", Password: "PeerGo-member-2026!", InvitationToken: token,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	digest := sha256.Sum256([]byte(token))
	if string(repository.prepareCommand.InvitationDigest) != string(digest[:]) {
		t.Fatal("PrepareRegistration() did not receive the invitation digest")
	}
	wantEmailBinding := invitationEmailBindingHMAC(token, "member@example.com")
	if !bytes.Equal(repository.prepareCommand.InvitationEmailBinding, wantEmailBinding) ||
		bytes.Contains(repository.prepareCommand.InvitationEmailBinding, []byte("member@example.com")) {
		t.Fatal("PrepareRegistration() did not receive the privacy-preserving email binding")
	}
	if vault.provisioned.Username != "new_member" || vault.provisioned.Email != "member@example.com" {
		t.Fatalf("normalized Vault input = %+v", vault.provisioned)
	}
	if repository.attachCalls != 1 || repository.completeCalls != 1 || vault.activateCalls != 1 {
		t.Fatalf("attach=%d complete=%d activate=%d", repository.attachCalls, repository.completeCalls, vault.activateCalls)
	}
	if result.UserID == uuid.Nil || result.Username != "new_member" || !result.EmailVerificationRequired || !result.CompletedAt.Equal(now) {
		t.Fatalf("Register() result = %+v", result)
	}
}

func TestNormalizeRegistrationInputAcceptsLegacyInvitationToken(t *testing.T) {
	token := "a1" + strings.Repeat("0", 62)
	_, digest, emailBinding, err := normalizeRegistrationInput(RegistrationInput{
		ID: uuid.New(), Username: "legacy_member", DisplayName: "旧站成员",
		Email: "legacy@example.com", Password: "PeerGo-member-2026!",
		InvitationToken: token,
	})
	if err != nil {
		t.Fatalf("normalizeRegistrationInput() error = %v", err)
	}
	want := sha256.Sum256([]byte(token))
	if !bytes.Equal(digest, want[:]) {
		t.Fatal("legacy invitation token was not reduced to its digest")
	}
	if !bytes.Equal(emailBinding, invitationEmailBindingHMAC(token, "legacy@example.com")) {
		t.Fatal("legacy invitation token did not bind its registration email")
	}
}

func TestRegistrationServiceStopsBeforeVaultWhenAdmissionRejects(t *testing.T) {
	repository := &registrationRepositoryFixture{prepareErr: ErrRegistrationClosed}
	vault := &registrationVaultFixture{credentialRef: uuid.New()}
	service, err := NewRegistrationService(repository, vault, time.Now)
	if err != nil {
		t.Fatalf("NewRegistrationService() error = %v", err)
	}
	_, err = service.Register(context.Background(), RegistrationInput{
		ID: uuid.New(), Username: "new_member", DisplayName: "新成员",
		Email: "member@example.com", Password: "PeerGo-member-2026!",
	})
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register() error = %v, want ErrRegistrationClosed", err)
	}
	if vault.provisioned.ID != uuid.Nil || vault.activateCalls != 0 {
		t.Fatal("Vault was called after Core admission rejected the request")
	}
}

func TestRegistrationServiceLeavesReservationRecoverableWhenVaultFails(t *testing.T) {
	repository := &registrationRepositoryFixture{record: RegistrationRecord{
		Mode: RegistrationModeOpen, State: RegistrationStateReserved,
	}}
	vault := &registrationVaultFixture{provisionErr: ErrRegistrationServiceUnavailable}
	service, err := NewRegistrationService(repository, vault, time.Now)
	if err != nil {
		t.Fatalf("NewRegistrationService() error = %v", err)
	}
	_, err = service.Register(context.Background(), RegistrationInput{
		ID: uuid.New(), Username: "new_member", DisplayName: "新成员",
		Email: "member@example.com", Password: "PeerGo-member-2026!",
	})
	if !errors.Is(err, ErrRegistrationServiceUnavailable) {
		t.Fatalf("Register() error = %v", err)
	}
	if repository.attachCalls != 0 || repository.completeCalls != 0 || vault.activateCalls != 0 {
		t.Fatal("later saga steps ran after Vault provisioning failed")
	}
	if repository.cancelCalls != 0 {
		t.Fatal("transient Vault failure released a recoverable reservation")
	}
}

func TestRegistrationServiceReleasesReservationWhenVaultRejectsIdentifiers(t *testing.T) {
	repository := &registrationRepositoryFixture{record: RegistrationRecord{
		Mode: RegistrationModeInvite, State: RegistrationStateReserved,
	}}
	vault := &registrationVaultFixture{provisionErr: ErrRegistrationUnavailable}
	service, err := NewRegistrationService(repository, vault, time.Now)
	if err != nil {
		t.Fatalf("NewRegistrationService() error = %v", err)
	}
	_, err = service.Register(context.Background(), RegistrationInput{
		ID: uuid.New(), Username: "taken_member", DisplayName: "新成员",
		Email: "taken@example.com", Password: "PeerGo-member-2026!",
		InvitationToken: "i" + strings.Repeat("a", 42),
	})
	if !errors.Is(err, ErrRegistrationUnavailable) {
		t.Fatalf("Register() error = %v", err)
	}
	if repository.cancelCalls != 1 || repository.attachCalls != 0 || repository.completeCalls != 0 {
		t.Fatalf("cancel=%d attach=%d complete=%d", repository.cancelCalls, repository.attachCalls, repository.completeCalls)
	}
}

func TestRegistrationAdmissionModePreservesOptionalInviteDuringOpenRegistration(t *testing.T) {
	digest := make([]byte, invitationTokenDigestBytes)
	tests := []struct {
		name       string
		policyMode RegistrationMode
		digest     []byte
		want       RegistrationMode
		wantErr    error
	}{
		{name: "open direct", policyMode: RegistrationModeOpen, want: RegistrationModeOpen},
		{name: "open invited", policyMode: RegistrationModeOpen, digest: digest, want: RegistrationModeInvite},
		{name: "invite invited", policyMode: RegistrationModeInvite, digest: digest, want: RegistrationModeInvite},
		{name: "invite missing", policyMode: RegistrationModeInvite, wantErr: ErrRegistrationInvitationInvalid},
		{name: "closed", policyMode: RegistrationModeClosed, digest: digest, wantErr: ErrRegistrationClosed},
		{name: "malformed digest", policyMode: RegistrationModeOpen, digest: []byte("short"), wantErr: ErrRegistrationInvitationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := registrationAdmissionMode(test.policyMode, test.digest)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("registrationAdmissionMode() error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("registrationAdmissionMode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRegistrationPolicyRejectsReservedUsernameAndUnlistedEmailDomain(t *testing.T) {
	t.Parallel()

	policy := RegistrationPolicy{
		UsernameMinCharacters: 3, UsernameMaxCharacters: 20,
		ReservedUsernames: []string{"admin", "root"},
		EmailDomainMode:   EmailDomainModeAllowlist,
		EmailDomains:      []string{"example.com"},
	}
	if err := registrationPolicyAllowsNewAccount(policy, "admin", "example.com"); !errors.Is(err, ErrRegistrationUnavailable) {
		t.Fatalf("reserved username error = %v", err)
	}
	if err := registrationPolicyAllowsNewAccount(policy, "member", "other.example"); !errors.Is(err, ErrRegistrationUnavailable) {
		t.Fatalf("unlisted domain error = %v", err)
	}
	if err := registrationPolicyAllowsNewAccount(policy, "member", "example.com"); err != nil {
		t.Fatalf("allowed account error = %v", err)
	}
}
