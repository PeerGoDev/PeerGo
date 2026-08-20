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
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

var (
	ErrTorrentMetadataUpdateInput               = errors.New("torrent metadata update input is invalid")
	ErrTorrentMetadataUpdateNotFound            = errors.New("published torrent was not found for uploader")
	ErrTorrentMetadataUpdateEmailUnverified     = errors.New("torrent metadata update requires a verified email")
	ErrTorrentMetadataUpdateStateConflict       = errors.New("torrent is not published")
	ErrTorrentMetadataUpdateVersionConflict     = errors.New("torrent metadata version changed")
	ErrTorrentMetadataUpdateCategoryUnavailable = errors.New("torrent metadata category is unavailable")
	ErrTorrentMetadataUpdateUnchanged           = errors.New("torrent metadata is unchanged")
	ErrTorrentMetadataUpdateIdempotencyConflict = errors.New("torrent metadata request was reused")
)

type UpdatePublishedMetadataInput struct {
	RequestID       uuid.UUID
	TorrentID       TorrentID
	ExpectedVersion int64
	CategoryID      string
	Title           string
	Subtitle        string
	Reason          string
}

type UpdatePublishedMetadataCommand struct {
	UpdatePublishedMetadataInput
	UploaderID    uuid.UUID
	Metadata      EditableMetadata
	OccurredAt    time.Time
	Authorization authz.Decision
}

type PublishedMetadataRevision struct {
	ID        uuid.UUID
	TorrentID TorrentID
	Version   int64
	Metadata  EditableMetadata
	Reason    string
	UpdatedAt time.Time
}

type TorrentMaintenanceRepository interface {
	UpdatePublishedMetadata(context.Context, UpdatePublishedMetadataCommand) (PublishedMetadataRevision, error)
	SubmitPublishedContentChange(context.Context, SubmitPublishedContentChangeCommand) (PublishedContentChange, error)
	ListPublishedContentChanges(context.Context, PublishedContentChangeQuery) (ManagedPublishedContentChangePage, error)
	DecidePublishedContentChange(context.Context, DecidePublishedContentChangeCommand) (PublishedContentChangeDecisionResult, error)
	SubmitPublishedScreenshotChange(context.Context, SubmitPublishedScreenshotChangeCommand) (PublishedScreenshotChange, error)
	ListPublishedScreenshotChanges(context.Context, PublishedScreenshotChangeQuery) (ManagedPublishedScreenshotChangePage, error)
	DecidePublishedScreenshotChange(context.Context, DecidePublishedScreenshotChangeCommand) (PublishedScreenshotChangeDecisionResult, error)
	PublishedScreenshotChangeSource(context.Context, uuid.UUID, ScreenshotChangeSide, int) (PublicScreenshotSource, error)
	SubmitTorrentWithdrawal(context.Context, SubmitTorrentWithdrawalCommand) (TorrentWithdrawalRequest, error)
	ListTorrentWithdrawals(context.Context, TorrentWithdrawalQuery) (ManagedTorrentWithdrawalPage, error)
	DecideTorrentWithdrawal(context.Context, DecideTorrentWithdrawalCommand) (TorrentWithdrawalDecisionResult, error)
	CreateTorrentReport(context.Context, CreateTorrentReportCommand) (TorrentReportReceipt, error)
	ListTorrentReportCases(context.Context, TorrentReportCaseQuery) (ManagedTorrentReportCasePage, error)
	DecideTorrentReportCase(context.Context, DecideTorrentReportCaseCommand) (TorrentReportDecisionResult, error)
}

type TorrentMaintenanceAuthenticator interface {
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type TorrentMaintenanceService struct {
	authenticator   TorrentMaintenanceAuthenticator
	authorizer      authz.Authorizer
	repository      TorrentMaintenanceRepository
	now             func() time.Time
	stores          *StoreRegistry
	activeBackendID StorageBackendID
	uploadPolicies  interface {
		Effective(context.Context, time.Time) (UploadPolicyRevision, error)
	}
	newUUID func() uuid.UUID
}

type TorrentMaintenanceServiceConfig struct {
	Stores          *StoreRegistry
	ActiveBackendID StorageBackendID
	UploadPolicies  interface {
		Effective(context.Context, time.Time) (UploadPolicyRevision, error)
	}
	NewUUID func() uuid.UUID
}

func NewTorrentMaintenanceService(authenticator TorrentMaintenanceAuthenticator, authorizer authz.Authorizer, repository TorrentMaintenanceRepository, now func() time.Time, configs ...TorrentMaintenanceServiceConfig) (*TorrentMaintenanceService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("torrent maintenance dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if len(configs) > 1 {
		return nil, errors.New("at most one torrent maintenance config is allowed")
	}
	config := TorrentMaintenanceServiceConfig{}
	if len(configs) == 1 {
		config = configs[0]
	}
	if (config.Stores == nil) != (config.ActiveBackendID == "") ||
		(config.Stores != nil && config.UploadPolicies == nil) {
		return nil, errors.New("torrent screenshot maintenance dependencies must be configured together")
	}
	if config.Stores != nil {
		if _, exists := config.Stores.Get(config.ActiveBackendID); !exists {
			return nil, errors.New("torrent screenshot maintenance backend is not configured")
		}
	}
	if config.NewUUID == nil {
		config.NewUUID = uuid.New
	}
	return &TorrentMaintenanceService{
		authenticator: authenticator, authorizer: authorizer, repository: repository, now: now,
		stores: config.Stores, activeBackendID: config.ActiveBackendID,
		uploadPolicies: config.UploadPolicies, newUUID: config.NewUUID,
	}, nil
}

func (service *TorrentMaintenanceService) UpdatePublishedMetadata(ctx context.Context, cookieToken, csrfToken string, input UpdatePublishedMetadataInput) (PublishedMetadataRevision, error) {
	metadata, reason, err := normalizePublishedMetadataInput(input)
	if err != nil {
		return PublishedMetadataRevision{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return PublishedMetadataRevision{}, err
	}
	if session.User.EmailVerifiedAt == nil {
		return PublishedMetadataRevision{}, ErrTorrentMetadataUpdateEmailUnverified
	}
	now := service.now().UTC().Round(0)
	decision, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentMetadataUpdateSelf, now)
	if err != nil {
		return PublishedMetadataRevision{}, err
	}
	input.Reason = reason
	return service.repository.UpdatePublishedMetadata(ctx, UpdatePublishedMetadataCommand{
		UpdatePublishedMetadataInput: input, UploaderID: session.User.ID,
		Metadata: metadata, OccurredAt: now, Authorization: decision,
	})
}

func normalizePublishedMetadataInput(input UpdatePublishedMetadataInput) (EditableMetadata, string, error) {
	if input.RequestID == uuid.Nil || input.TorrentID < 1 || input.ExpectedVersion < 1 {
		return EditableMetadata{}, "", ErrTorrentMetadataUpdateInput
	}
	metadata, err := NewEditableMetadata(input.CategoryID, input.Title, input.Subtitle)
	if err != nil {
		return EditableMetadata{}, "", ErrTorrentMetadataUpdateInput
	}
	reason := strings.TrimSpace(input.Reason)
	if !utf8.ValidString(reason) || utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 1000 {
		return EditableMetadata{}, "", ErrTorrentMetadataUpdateInput
	}
	return metadata, reason, nil
}

func wrapTorrentMetadataUpdate(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
