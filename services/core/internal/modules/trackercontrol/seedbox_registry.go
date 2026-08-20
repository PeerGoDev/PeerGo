package trackercontrol

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultSeedboxReportLimit = 50
	MaximumSeedboxReportLimit = 100
)

var (
	ErrSeedboxReportInput         = errors.New("Seedbox report input is invalid")
	ErrSeedboxReportPending       = errors.New("a Seedbox report is already pending")
	ErrSeedboxReportApproved      = errors.New("the Seedbox address is already approved")
	ErrSeedboxReportNotFound      = errors.New("Seedbox report was not found")
	ErrSeedboxReportConflict      = errors.New("Seedbox report version changed")
	ErrSeedboxDecisionConflict    = errors.New("Seedbox decision request was reused")
	ErrSeedboxRegistryUnavailable = errors.New("Seedbox registry is unavailable")
)

type SeedboxReportStatus string

const (
	SeedboxReportPending  SeedboxReportStatus = "pending"
	SeedboxReportApproved SeedboxReportStatus = "approved"
	SeedboxReportRejected SeedboxReportStatus = "rejected"
)

type SeedboxDecision string

const (
	SeedboxDecisionApprove SeedboxDecision = "approve"
	SeedboxDecisionReject  SeedboxDecision = "reject"
)

type SeedboxReport struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	UserNumericID  int64
	Username       string
	Address        string
	Provider       string
	BandwidthMbps  int64
	Statement      string
	Status         SeedboxReportStatus
	Version        int64
	SubmittedAt    time.Time
	DecidedAt      *time.Time
	DecisionReason string
	PolicySequence *int64
}

type SeedboxReportPage struct {
	Items  []SeedboxReport
	Total  int64
	Limit  int
	Offset int
}

type SubmitSeedboxReportInput struct {
	RequestID     uuid.UUID
	Address       string
	Provider      string
	BandwidthMbps int64
	Statement     string
}

type DecideSeedboxReportInput struct {
	RequestID       uuid.UUID
	ReportID        uuid.UUID
	ExpectedVersion int64
	Decision        SeedboxDecision
	Reason          string
}

type submitSeedboxReportCommand struct {
	SubmitSeedboxReportInput
	ReportID                uuid.UUID
	UserID                  uuid.UUID
	Network                 string
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type decideSeedboxReportCommand struct {
	DecideSeedboxReportInput
	DecisionID              uuid.UUID
	ActorID                 uuid.UUID
	AuthorizationDecisionID uuid.UUID
	OccurredAt              time.Time
}

type SeedboxRegistryRepository interface {
	ListMySeedboxReports(context.Context, uuid.UUID, int, int) (SeedboxReportPage, error)
	SubmitSeedboxReport(context.Context, submitSeedboxReportCommand) (SeedboxReport, error)
	ListSeedboxReports(context.Context, SeedboxReportStatus, int, int) (SeedboxReportPage, error)
	DecideSeedboxReport(context.Context, decideSeedboxReportCommand) (SeedboxReport, error)
}

func (service *RuntimePolicyService) seedboxRegistry() (SeedboxRegistryRepository, error) {
	repository, ok := service.repository.(SeedboxRegistryRepository)
	if !ok {
		return nil, ErrSeedboxRegistryUnavailable
	}
	return repository, nil
}

func (service *RuntimePolicyService) MySeedboxReports(ctx context.Context, userID uuid.UUID, limit, offset int) (SeedboxReportPage, error) {
	if userID == uuid.Nil || !validSeedboxPage(limit, offset) {
		return SeedboxReportPage{}, ErrSeedboxReportInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, userID, authz.ActionTrackerSeedboxReadSelf, now); err != nil {
		return SeedboxReportPage{}, err
	}
	repository, err := service.seedboxRegistry()
	if err != nil {
		return SeedboxReportPage{}, err
	}
	return repository.ListMySeedboxReports(ctx, userID, limit, offset)
}

func (service *RuntimePolicyService) SubmitSeedboxReport(ctx context.Context, userID uuid.UUID, input SubmitSeedboxReportInput) (SeedboxReport, error) {
	input.Address = strings.TrimSpace(input.Address)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Statement = strings.TrimSpace(input.Statement)
	address, err := netip.ParseAddr(input.Address)
	if input.RequestID == uuid.Nil || userID == uuid.Nil || err != nil || input.BandwidthMbps < 1 || input.BandwidthMbps > 10_000_000 ||
		!validSeedboxText(input.Provider, 2, 100) || !validSeedboxText(input.Statement, 10, 1000) {
		return SeedboxReport{}, ErrSeedboxReportInput
	}
	input.Address = address.String()
	bits := 128
	if address.Is4() {
		bits = 32
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, userID, authz.ActionTrackerSeedboxReportCreateSelf, now)
	if err != nil {
		return SeedboxReport{}, err
	}
	repository, err := service.seedboxRegistry()
	if err != nil {
		return SeedboxReport{}, err
	}
	return repository.SubmitSeedboxReport(ctx, submitSeedboxReportCommand{
		SubmitSeedboxReportInput: input, ReportID: uuid.New(), UserID: userID,
		Network: netip.PrefixFrom(address, bits).String(), AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func (service *RuntimePolicyService) SeedboxReports(ctx context.Context, actor authz.StaffActor, status SeedboxReportStatus, limit, offset int) (SeedboxReportPage, error) {
	if (status != "" && !validSeedboxReportStatus(status)) || !validSeedboxPage(limit, offset) {
		return SeedboxReportPage{}, ErrSeedboxReportInput
	}
	now := service.now().UTC().Round(0)
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTrackerSeedboxRegistryRead, authz.SiteScope(), now, "seedbox-registry-read"); err != nil {
		return SeedboxReportPage{}, err
	}
	repository, err := service.seedboxRegistry()
	if err != nil {
		return SeedboxReportPage{}, err
	}
	return repository.ListSeedboxReports(ctx, status, limit, offset)
}

func (service *RuntimePolicyService) DecideSeedboxReport(ctx context.Context, actor authz.StaffActor, input DecideSeedboxReportInput) (SeedboxReport, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequestID == uuid.Nil || input.ReportID == uuid.Nil || input.ExpectedVersion < 1 ||
		(input.Decision != SeedboxDecisionApprove && input.Decision != SeedboxDecisionReject) || !validSeedboxText(input.Reason, 10, 1000) {
		return SeedboxReport{}, ErrSeedboxReportInput
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTrackerSeedboxReportDecide, authz.SiteScope(), now, "seedbox-report-decision")
	if err != nil {
		return SeedboxReport{}, err
	}
	repository, err := service.seedboxRegistry()
	if err != nil {
		return SeedboxReport{}, err
	}
	return repository.DecideSeedboxReport(ctx, decideSeedboxReportCommand{
		DecideSeedboxReportInput: input, DecisionID: uuid.New(), ActorID: actor.Subject.ID,
		AuthorizationDecisionID: decision.ID, OccurredAt: now,
	})
}

func validSeedboxPage(limit, offset int) bool {
	return limit >= 1 && limit <= MaximumSeedboxReportLimit && offset >= 0 && offset <= 1_000_000
}
func validSeedboxReportStatus(value SeedboxReportStatus) bool {
	return value == SeedboxReportPending || value == SeedboxReportApproved || value == SeedboxReportRejected
}
func validSeedboxText(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum
}
