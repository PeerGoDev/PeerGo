package torrents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
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

type TorrentPeerRepository interface {
	ManagedPeerTarget(context.Context, TorrentID) (ManagedTorrentPeerTarget, error)
	ManagedPeerIdentities(context.Context, []uuid.UUID) ([]ManagedTorrentPeerIdentity, error)
}

type TrackerPeerReader interface {
	ActivePeers(context.Context, string, int) (trackeroperationsv1.ActivePeerPage, error)
}

type TorrentAdministrationService struct {
	repository     TorrentAdministrationRepository
	peerRepository TorrentPeerRepository
	peerReader     TrackerPeerReader
	authorizer     authz.Authorizer
	now            func() time.Time
}

func NewTorrentAdministrationService(repository TorrentAdministrationRepository, authorizer authz.Authorizer, now func() time.Time, peerReaders ...TrackerPeerReader) (*TorrentAdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("torrent administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if len(peerReaders) > 1 {
		return nil, errors.New("at most one Tracker peer reader may be provided")
	}
	service := &TorrentAdministrationService{repository: repository, authorizer: authorizer, now: now}
	if len(peerReaders) == 1 {
		peerRepository, ok := repository.(TorrentPeerRepository)
		if peerReaders[0] == nil || !ok {
			return nil, errors.New("torrent peer reader dependencies are required together")
		}
		service.peerReader = peerReaders[0]
		service.peerRepository = peerRepository
	}
	return service, nil
}

func (service *TorrentAdministrationService) ActivePeers(ctx context.Context, actor authz.StaffActor, torrentID TorrentID) (ManagedTorrentPeerList, error) {
	if torrentID < 1 {
		return ManagedTorrentPeerList{}, ErrTorrentAdministrationInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionTorrentManageRead, authz.SiteScope(), now, "torrent-peer-list"); err != nil {
		return ManagedTorrentPeerList{}, err
	}
	if service.peerReader == nil || service.peerRepository == nil {
		return ManagedTorrentPeerList{}, ErrManagedTorrentPeersUnavailable
	}
	target, err := service.peerRepository.ManagedPeerTarget(ctx, torrentID)
	if err != nil {
		return ManagedTorrentPeerList{}, err
	}
	page, err := service.peerReader.ActivePeers(ctx, target.InfoHashV1.Hex(), trackeroperationsv1.MaxActivePeerLimit)
	if err != nil {
		return ManagedTorrentPeerList{}, fmt.Errorf("%w: %v", ErrManagedTorrentPeersUnavailable, err)
	}
	return service.aggregateActivePeers(ctx, target, page)
}

func (service *TorrentAdministrationService) aggregateActivePeers(ctx context.Context, target ManagedTorrentPeerTarget, page trackeroperationsv1.ActivePeerPage) (ManagedTorrentPeerList, error) {
	userIDs := make([]uuid.UUID, 0, len(page.Items))
	seen := make(map[uuid.UUID]struct{}, len(page.Items))
	for _, peer := range page.Items {
		userID, err := uuid.Parse(peer.UserID)
		if err != nil {
			return ManagedTorrentPeerList{}, ErrTorrentReadInvariant
		}
		if _, exists := seen[userID]; !exists {
			seen[userID] = struct{}{}
			userIDs = append(userIDs, userID)
		}
	}
	identities, err := service.peerRepository.ManagedPeerIdentities(ctx, userIDs)
	if err != nil {
		return ManagedTorrentPeerList{}, fmt.Errorf("resolve active peer identities: %w", err)
	}
	identityByID := make(map[uuid.UUID]ManagedTorrentPeerIdentity, len(identities))
	for _, identity := range identities {
		identityByID[identity.UserID] = identity
	}
	grouped := make(map[uuid.UUID]*ManagedTorrentPeer, len(userIDs))
	clients := make(map[uuid.UUID]map[string]struct{}, len(userIDs))
	for _, active := range page.Items {
		userID := uuid.MustParse(active.UserID)
		identity, exists := identityByID[userID]
		if !exists {
			return ManagedTorrentPeerList{}, ErrTorrentReadInvariant
		}
		peer := grouped[userID]
		if peer == nil {
			peer = &ManagedTorrentPeer{
				UserID: userID, NumericID: identity.NumericID, Username: identity.Username,
				DisplayName: identity.DisplayName, ProgressBasisPoints: progressBasisPoints(target.TotalSizeBytes, active.Left),
				Uploader: userID == target.UploaderID,
			}
			grouped[userID] = peer
			clients[userID] = make(map[string]struct{})
		}
		peer.ActiveConnections++
		if active.Left == 0 {
			peer.SeedingConnections++
		} else {
			peer.LeechingConnections++
		}
		peer.ProgressBasisPoints = max(peer.ProgressBasisPoints, progressBasisPoints(target.TotalSizeBytes, active.Left))
		peer.Uploaded = max(peer.Uploaded, active.Uploaded)
		peer.Downloaded = max(peer.Downloaded, active.Downloaded)
		if active.LastAnnounce.After(peer.LastAnnounce) {
			peer.LastAnnounce = active.LastAnnounce
		}
		clients[userID][active.ClientFamily] = struct{}{}
	}
	items := make([]ManagedTorrentPeer, 0, len(grouped))
	for userID, peer := range grouped {
		peer.ClientFamilies = make([]string, 0, len(clients[userID]))
		for client := range clients[userID] {
			peer.ClientFamilies = append(peer.ClientFamilies, client)
		}
		slices.Sort(peer.ClientFamilies)
		items = append(items, *peer)
	}
	slices.SortFunc(items, func(left, right ManagedTorrentPeer) int {
		if left.SeedingConnections != right.SeedingConnections {
			return right.SeedingConnections - left.SeedingConnections
		}
		if compared := strings.Compare(strings.ToLower(left.Username), strings.ToLower(right.Username)); compared != 0 {
			return compared
		}
		return strings.Compare(left.UserID.String(), right.UserID.String())
	})
	return ManagedTorrentPeerList{
		TorrentID: target.TorrentID, Items: items, TotalConnections: len(page.Items),
		Truncated: page.Truncated, GeneratedAt: page.GeneratedAt,
	}, nil
}

func progressBasisPoints(totalSizeBytes, left int64) int {
	if totalSizeBytes < 1 || left >= totalSizeBytes {
		return 0
	}
	if left <= 0 {
		return 10_000
	}
	// This is presentation-only progress, not accounting evidence. Floating
	// point avoids overflowing int64 for very large torrents while the bounded
	// basis-point result remains deterministic enough for one decimal percent.
	return min(10_000, max(0, int((float64(totalSizeBytes-left)/float64(totalSizeBytes))*10_000)))
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
