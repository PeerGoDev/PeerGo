package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestDecisionEventConstantsAndReasonsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/decision-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != DecisionRecordedSchemaVersion || schema.Properties["event_type"].Const != DecisionRecordedEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantReasons := []string{
		string(authz.ReasonAllowed),
		string(authz.ReasonActionUnknown),
		string(authz.ReasonSubjectInvalid),
		string(authz.ReasonSubjectInactive),
		string(authz.ReasonCredentialAudienceMismatch),
		string(authz.ReasonRelationshipMismatch),
		string(authz.ReasonAuthorityBindingMismatch),
		string(authz.ReasonGrantMissing),
		string(authz.ReasonGrantRevoked),
		string(authz.ReasonGrantNotStarted),
		string(authz.ReasonGrantExpired),
		string(authz.ReasonGrantInvariant),
		string(authz.ReasonMandateInactive),
		string(authz.ReasonMandateNotStarted),
		string(authz.ReasonMandateExpired),
		string(authz.ReasonScopeMismatch),
		string(authz.ReasonContextMissing),
	}
	gotReasons := append([]string(nil), schema.Properties["reason"].Enum...)
	slices.Sort(wantReasons)
	slices.Sort(gotReasons)
	if !slices.Equal(gotReasons, wantReasons) {
		t.Fatalf("schema reasons = %v, want %v", gotReasons, wantReasons)
	}
}

func TestRegistrationCompletedConstantsAndModesMatchJSONSchema(t *testing.T) {
	t.Parallel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/registration-completed.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != RegistrationCompletedSchemaVersion || schema.Properties["event_type"].Const != RegistrationCompletedEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantModes := []string{string(identity.RegistrationModeOpen), string(identity.RegistrationModeInvite)}
	gotModes := append([]string(nil), schema.Properties["admission_mode"].Enum...)
	slices.Sort(wantModes)
	slices.Sort(gotModes)
	if !slices.Equal(gotModes, wantModes) {
		t.Fatalf("schema modes = %v, want %v", gotModes, wantModes)
	}
}

func TestEmailVerifiedConstantsMatchJSONSchema(t *testing.T) {
	t.Parallel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/email-verified.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != EmailVerifiedSchemaVersion || schema.Properties["event_type"].Const != EmailVerifiedEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
}

func TestPasswordRecoveredConstantsMatchJSONSchema(t *testing.T) {
	t.Parallel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/password-recovered.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != PasswordRecoveredSchemaVersion || schema.Properties["event_type"].Const != PasswordRecoveredEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
}

func TestSessionRevocationConstantsAndScopesMatchJSONSchema(t *testing.T) {
	t.Parallel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/session-revocation-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != SessionRevocationSchemaVersion || schema.Properties["event_type"].Const != SessionRevocationEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	want := []string{string(identity.SessionRevocationSingle), string(identity.SessionRevocationOthers)}
	got := append([]string(nil), schema.Properties["scope"].Enum...)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("schema scopes = %v, want %v", got, want)
	}
}

func TestTwoFactorChangeConstantsAndKindsMatchJSONSchema(t *testing.T) {
	t.Parallel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/two-factor-change-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != TwoFactorChangeSchemaVersion || schema.Properties["event_type"].Const != TwoFactorChangeEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	want := []string{
		string(identity.TwoFactorEnabled),
		string(identity.TwoFactorRecoveryCodesRotated),
		string(identity.TwoFactorDisabled),
	}
	got := append([]string(nil), schema.Properties["kind"].Enum...)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("schema kinds = %v, want %v", got, want)
	}
}

func TestGrantRevocationEventConstantsAndTransitionsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/grant-revocation-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != GrantRevocationSchemaVersion || schema.Properties["event_type"].Const != GrantRevocationEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantTransitions := []string{
		string(authz.GrantTransitionProposed),
		string(authz.GrantTransitionGovernanceApproved),
		string(authz.GrantTransitionSecurityApproved),
		string(authz.GrantTransitionRejected),
		string(authz.GrantTransitionApplied),
		string(authz.GrantTransitionConflicted),
		string(authz.GrantTransitionExpired),
	}
	gotTransitions := append([]string(nil), schema.Properties["transition"].Enum...)
	slices.Sort(wantTransitions)
	slices.Sort(gotTransitions)
	if !slices.Equal(gotTransitions, wantTransitions) {
		t.Fatalf("schema transitions = %v, want %v", gotTransitions, wantTransitions)
	}
}

func TestStaffBootstrapEventConstantsAndTransitionsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/staff-credential-bootstrap-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != StaffBootstrapSchemaVersion || schema.Properties["event_type"].Const != StaffBootstrapEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantTransitions := []string{
		string(identity.StaffBootstrapTicketIssued),
		string(identity.StaffBootstrapCredentialEnrolled),
	}
	gotTransitions := append([]string(nil), schema.Properties["transition"].Enum...)
	slices.Sort(wantTransitions)
	slices.Sort(gotTransitions)
	if !slices.Equal(gotTransitions, wantTransitions) {
		t.Fatalf("schema transitions = %v, want %v", gotTransitions, wantTransitions)
	}
}

func TestCategoryChangeEventConstantsAndTransitionsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/category-change-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != CategoryChangeSchemaVersion || schema.Properties["event_type"].Const != CategoryChangeEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantTransitions := []string{
		string(catalog.CategoryTransitionCreated),
		string(catalog.CategoryTransitionUpdated),
	}
	gotTransitions := append([]string(nil), schema.Properties["transition"].Enum...)
	slices.Sort(wantTransitions)
	slices.Sort(gotTransitions)
	if !slices.Equal(gotTransitions, wantTransitions) {
		t.Fatalf("schema transitions = %v, want %v", gotTransitions, wantTransitions)
	}
}

func TestSiteDisplaySettingsChangeEventConstantsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/site-display-settings-change-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != SiteDisplaySettingsChangeSchemaVersion || schema.Properties["event_type"].Const != SiteDisplaySettingsChangeEventType || schema.Properties["settings_section"].Const != catalog.SiteDisplaySettingsSection {
		t.Fatalf("schema constants = version %q type %q section %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const, schema.Properties["settings_section"].Const)
	}
}

func TestRegistrationPolicyChangeEventConstantsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/registration-policy-change-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != RegistrationPolicyChangeSchemaVersion ||
		schema.Properties["event_type"].Const != RegistrationPolicyChangeEventType ||
		schema.Properties["settings_section"].Const != identity.RegistrationPolicySettingsSection {
		t.Fatalf("schema constants = version %q type %q section %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const, schema.Properties["settings_section"].Const)
	}
}

func TestAccountRestrictionEventConstantsAndTransitionsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/account-restriction-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != AccountRestrictionSchemaVersion || schema.Properties["event_type"].Const != AccountRestrictionEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantTransitions := []string{
		string(identity.AccountRestrictionTransitionCreated),
		string(identity.AccountRestrictionTransitionRevoked),
	}
	gotTransitions := append([]string(nil), schema.Properties["transition"].Enum...)
	slices.Sort(wantTransitions)
	slices.Sort(gotTransitions)
	if !slices.Equal(gotTransitions, wantTransitions) {
		t.Fatalf("schema transitions = %v, want %v", gotTransitions, wantTransitions)
	}
}

func TestTorrentLifecycleEventConstantsAndActionsMatchJSONSchema(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v1/torrent-lifecycle-change-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != TorrentLifecycleSchemaVersion ||
		schema.Properties["event_type"].Const != TorrentLifecycleEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantActions := []string{
		string(torrents.TorrentAvailabilityDisable),
		string(torrents.TorrentAvailabilityRestore),
	}
	gotActions := append([]string(nil), schema.Properties["action"].Enum...)
	slices.Sort(wantActions)
	slices.Sort(gotActions)
	if !slices.Equal(gotActions, wantActions) {
		t.Fatalf("schema actions = %v, want %v", gotActions, wantActions)
	}
}

func TestCommentModerationEventConstantsAndEnumsMatchJSONSchema(t *testing.T) {
	t.Parallel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../contracts/events/audit/v4/comment-moderation-decision-recorded.schema.json"))
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var schema struct {
		Properties map[string]struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("Unmarshal(schema) error = %v", err)
	}
	if schema.Properties["schema_version"].Const != CommentModerationDecisionSchemaVersion ||
		schema.Properties["event_type"].Const != CommentModerationDecisionEventType {
		t.Fatalf("schema constants = version %q type %q", schema.Properties["schema_version"].Const, schema.Properties["event_type"].Const)
	}
	wantTargets := []string{string(social.CommentTargetTorrent), string(social.CommentTargetAnnouncement), string(social.CommentTargetPost)}
	gotTargets := append([]string(nil), schema.Properties["target_kind"].Enum...)
	slices.Sort(wantTargets)
	slices.Sort(gotTargets)
	if !slices.Equal(gotTargets, wantTargets) {
		t.Fatalf("schema targets = %v, want %v", gotTargets, wantTargets)
	}
	wantDecisions := []string{string(social.CommentModerationDismiss), string(social.CommentModerationHideComment)}
	gotDecisions := append([]string(nil), schema.Properties["decision"].Enum...)
	slices.Sort(wantDecisions)
	slices.Sort(gotDecisions)
	if !slices.Equal(gotDecisions, wantDecisions) {
		t.Fatalf("schema decisions = %v, want %v", gotDecisions, wantDecisions)
	}
	wantReasons := []string{
		string(social.CommentModerationNoViolation), string(social.CommentModerationSpam),
		string(social.CommentModerationHarassment), string(social.CommentModerationPersonalInformation),
		string(social.CommentModerationOffTopic), string(social.CommentModerationOther),
	}
	gotReasons := append([]string(nil), schema.Properties["reason_code"].Enum...)
	slices.Sort(wantReasons)
	slices.Sort(gotReasons)
	if !slices.Equal(gotReasons, wantReasons) {
		t.Fatalf("schema reasons = %v, want %v", gotReasons, wantReasons)
	}
}
