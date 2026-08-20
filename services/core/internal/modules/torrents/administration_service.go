package torrents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultManagedTorrentLimit     = 20
	MaxManagedTorrentLimit         = 50
	MaxManagedTorrentOffset        = 1_000_000
	maxManagedTorrentQueryRunes    = 100
	minTorrentLifecycleReasonRunes = 10
	maxTorrentLifecycleReasonRunes = 1000
)

type TorrentAdministrationRepository interface {
	ListManaged(context.Context, ManagedTorrentQuery) (ManagedTorrentPage, error)
	ChangeAvailability(context.Context, ChangeTorrentAvailabilityCommand) (TorrentAvailabilityResult, error)
}

type TorrentAdministrationService struct {
	repository TorrentAdministrationRepository
	authorizer authz.Authorizer
	now        func() time.Time
}

func NewTorrentAdministrationService(repository TorrentAdministrationRepository, authorizer authz.Authorizer, now func() time.Time) (*TorrentAdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("torrent administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &TorrentAdministrationService{repository: repository, authorizer: authorizer, now: now}, nil
}

func (service *TorrentAdministrationService) ListManaged(ctx context.Context, actor authz.StaffActor, query ManagedTorrentQuery) (ManagedTorrentPage, error) {
	normalized, err := normalizeManagedTorrentQuery(query)
	if err != nil {
		return ManagedTorrentPage{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentManageRead, authz.SiteScope(), now, "torrent-administration"); err != nil {
		return ManagedTorrentPage{}, err
	}
	page, err := service.repository.ListManaged(ctx, normalized)
	if err != nil {
		return ManagedTorrentPage{}, fmt.Errorf("list managed torrents: %w", err)
	}
	return page, nil
}

func (service *TorrentAdministrationService) ChangeAvailability(ctx context.Context, actor authz.StaffActor, input ChangeTorrentAvailabilityInput) (TorrentAvailabilityResult, error) {
	normalized, err := normalizeChangeTorrentAvailabilityInput(input)
	if err != nil {
		return TorrentAvailabilityResult{}, err
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentLifecycleUpdate, authz.SiteScope(), now, "torrent-administration")
	if err != nil {
		return TorrentAvailabilityResult{}, err
	}
	result, err := service.repository.ChangeAvailability(ctx, ChangeTorrentAvailabilityCommand{
		ChangeTorrentAvailabilityInput: normalized,
		ActorID:                        actor.Subject.ID, OccurredAt: now, Authorization: decision,
	})
	if err != nil {
		return TorrentAvailabilityResult{}, fmt.Errorf("change torrent availability: %w", err)
	}
	return result, nil
}

func normalizeManagedTorrentQuery(query ManagedTorrentQuery) (ManagedTorrentQuery, error) {
	query.Query = strings.TrimSpace(query.Query)
	query.CategoryID = strings.TrimSpace(query.CategoryID)
	if query.Limit < 1 || query.Limit > MaxManagedTorrentLimit || query.Offset < 0 || query.Offset > MaxManagedTorrentOffset ||
		!utf8.ValidString(query.Query) || utf8.RuneCountInString(query.Query) > maxManagedTorrentQueryRunes ||
		(query.CategoryID != "" && !facetStableIDPattern.MatchString(query.CategoryID)) ||
		(query.State != "" && !validReadState(query.State)) {
		return ManagedTorrentQuery{}, ErrTorrentAdministrationInput
	}
	return query, nil
}

func normalizeChangeTorrentAvailabilityInput(input ChangeTorrentAvailabilityInput) (ChangeTorrentAvailabilityInput, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	reasonRunes := utf8.RuneCountInString(input.Reason)
	if input.ChangeID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 ||
		!utf8.ValidString(input.Reason) || reasonRunes < minTorrentLifecycleReasonRunes || reasonRunes > maxTorrentLifecycleReasonRunes {
		return ChangeTorrentAvailabilityInput{}, ErrTorrentAdministrationInput
	}
	switch input.Action {
	case TorrentAvailabilityDisable, TorrentAvailabilityRestore:
		return input, nil
	default:
		return ChangeTorrentAvailabilityInput{}, ErrTorrentAdministrationInput
	}
}
