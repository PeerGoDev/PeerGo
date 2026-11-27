package workgroups

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrRetentionTargetNotFound = errors.New("retention target was not found")
	ErrRetentionTargetClosed   = errors.New("retention target is unavailable")
	ErrPayrollPreviewChanged   = errors.New("workgroup payroll preview changed")
	ErrPayrollNotEligible      = errors.New("workgroup payroll is not eligible")
)

type RetentionTarget struct {
	TorrentID     int64
	Title         string
	SizeBytes     int64
	RequiredHours int
	ClaimedAt     *time.Time
}

type RetentionItem struct {
	RetentionTarget
	ActiveSeconds int64
	Qualified     bool
}

type RetentionProgress struct {
	Month          string
	Items          []RetentionItem
	QualifiedCount int
	QualifiedBytes int64
	ExpectedHours  int
	CompleteHours  int
}

type ReseedPublication struct {
	TorrentID        int64
	Title            string
	Category         string
	SubmittedAt      time.Time
	PublishedAt      *time.Time
	ResolutionSource string
	Status           string
	Reason           string
}

type ReseedPublications struct {
	Items   []ReseedPublication
	Counted int
	Pending int
	Total   int
}

type PayrollInput struct {
	MembershipID uuid.UUID
	Group        GroupKind
	Month        time.Time
	Amount       int64
	MinimumCount int64
	MinimumBytes int64
}

type PayrollPreview struct {
	Input          PayrollInput
	UserID         uuid.UUID
	Username       string
	State          string
	Contribution   int64
	Target         int64
	QualifiedBytes int64
	PendingCount   int
	MissingHours   int
	PaidAt         *time.Time
	TransactionID  *uuid.UUID
	Digest         string
}

type monthlyRepository interface {
	RetentionTargets(context.Context, uuid.UUID, int, int) ([]RetentionTarget, error)
	SaveRetentionTarget(context.Context, int64, int, bool, uuid.UUID, uuid.UUID, time.Time) error
	ClaimRetentionTarget(context.Context, int64, uuid.UUID, uuid.UUID, time.Time) error
	RetentionProgress(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (RetentionProgress, error)
	ReseedPublications(context.Context, uuid.UUID, time.Time, time.Time, int, int) (ReseedPublications, error)
	PreviewPayroll(context.Context, PayrollInput, time.Time) (PayrollPreview, error)
	PayPayroll(context.Context, PayrollInput, []byte, uuid.UUID, uuid.UUID, time.Time) (PayrollPreview, error)
}

func parseWorkgroupMonth(value string, now time.Time) (time.Time, error) {
	if value == "" {
		return calendarMonthStart(now), nil
	}
	month, err := time.Parse("2006-01", value)
	if err != nil || month.Format("2006-01") != value || month.After(calendarMonthStart(now)) {
		return time.Time{}, ErrInput
	}
	return month, nil
}

func (service *Service) MyRetentionTargets(ctx context.Context, cookie string, limit, offset int) ([]RetentionTarget, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookie)
	if err != nil {
		return nil, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupReadSelf, service.now()); err != nil {
		return nil, err
	}
	if !validPage(limit, offset) {
		return nil, ErrInput
	}
	return service.repository.RetentionTargets(ctx, session.User.ID, limit, offset)
}

func (service *Service) MyRetentionProgress(ctx context.Context, cookie, period string) (RetentionProgress, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookie)
	if err != nil {
		return RetentionProgress{}, err
	}
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupReadSelf, now); err != nil {
		return RetentionProgress{}, err
	}
	month, err := parseWorkgroupMonth(period, now)
	if err != nil {
		return RetentionProgress{}, err
	}
	return service.repository.RetentionProgress(ctx, session.User.ID, uuid.Nil, month, now)
}

func (service *Service) ClaimRetentionTarget(ctx context.Context, cookie, csrf string, torrentID int64) error {
	if torrentID < 1 {
		return ErrInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookie, csrf)
	if err != nil {
		return err
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionWorkgroupReadSelf, now)
	if err != nil {
		return err
	}
	return service.repository.ClaimRetentionTarget(ctx, torrentID, session.User.ID, decision.ID, now)
}

func (service *Service) StaffRetentionTargets(ctx context.Context, actor authz.StaffActor, limit, offset int) ([]RetentionTarget, error) {
	if !validPage(limit, offset) {
		return nil, ErrInput
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), service.now(), "workgroup-retention-targets"); err != nil {
		return nil, err
	}
	return service.repository.RetentionTargets(ctx, uuid.Nil, limit, offset)
}

func (service *Service) SaveRetentionTarget(ctx context.Context, actor authz.StaffActor, torrentID int64, hours int, retire bool) error {
	if torrentID < 1 || (!retire && (hours < 1 || hours > 744)) {
		return ErrInput
	}
	now := canonicalTime(service.now())
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupMembershipManage, authz.SiteScope(), now, "workgroup-retention-target")
	if err != nil {
		return err
	}
	return service.repository.SaveRetentionTarget(ctx, torrentID, hours, retire, actor.Subject.ID, decision.ID, now)
}

func (service *Service) StaffRetentionProgress(ctx context.Context, actor authz.StaffActor, membershipID uuid.UUID, period string) (RetentionProgress, error) {
	now := canonicalTime(service.now())
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-retention-progress"); err != nil {
		return RetentionProgress{}, err
	}
	month, err := parseWorkgroupMonth(period, now)
	if err != nil {
		return RetentionProgress{}, err
	}
	return service.repository.RetentionProgress(ctx, uuid.Nil, membershipID, month, now)
}

func (service *Service) StaffReseedPublications(ctx context.Context, actor authz.StaffActor, membershipID uuid.UUID, period string, limit, offset int) (ReseedPublications, error) {
	now := canonicalTime(service.now())
	if !validPage(limit, offset) || membershipID == uuid.Nil {
		return ReseedPublications{}, ErrInput
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-reseed-publications"); err != nil {
		return ReseedPublications{}, err
	}
	month, err := parseWorkgroupMonth(period, now)
	if err != nil {
		return ReseedPublications{}, err
	}
	return service.repository.ReseedPublications(ctx, membershipID, month, now, limit, offset)
}

func (service *Service) MonthlyPayroll(ctx context.Context, actor authz.StaffActor, input PayrollInput, period, digest string) (PayrollPreview, error) {
	now := canonicalTime(service.now())
	month, err := parseWorkgroupMonth(period, now)
	if err != nil || period == "" || input.MembershipID == uuid.Nil || !validGroupKind(input.Group) ||
		input.Amount < 1 || input.Amount > 1_000_000 || input.MinimumCount < 0 || input.MinimumCount > 100_000 ||
		input.MinimumBytes < 0 || (input.Group == GroupRetention && input.MinimumCount == 0) {
		return PayrollPreview{}, ErrInput
	}
	input.Month = month
	if digest == "" {
		if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupManageRead, authz.SiteScope(), now, "workgroup-payroll-preview"); err != nil {
			return PayrollPreview{}, err
		}
		return service.repository.PreviewPayroll(ctx, input, now)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 || !month.Before(calendarMonthStart(now)) {
		return PayrollPreview{}, ErrInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionWorkgroupPayrollIssue, authz.SiteScope(), now, "workgroup-payroll-issue")
	if err != nil {
		return PayrollPreview{}, err
	}
	return service.repository.PayPayroll(ctx, input, decoded, actor.Subject.ID, decision.ID, now)
}
