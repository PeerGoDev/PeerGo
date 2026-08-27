package torrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/workgroups"
)

const (
	defaultUploadCleanupBatchSize     = 20
	defaultUploadCleanupLeaseDuration = 2 * time.Minute
	minimumUploadOrphanRetention      = 24 * time.Hour
	maximumUploadOrphanRetention      = 30 * 24 * time.Hour
	uploadFingerprintDomain           = "peergo:torrent-upload-request:v1\x00"
)

var (
	ErrTorrentUploadCategoryUnavailable = errors.New("torrent upload category is unavailable")
	ErrTorrentUploadDuplicate           = errors.New("torrent upload duplicates an existing swarm")
	ErrTorrentUploadEmailUnverified     = errors.New("torrent upload requires a verified email")
	ErrTorrentUploadExpired             = errors.New("torrent upload reservation is no longer resumable")
	ErrTorrentUploadIdempotencyConflict = errors.New("torrent upload idempotency key conflicts with another request")
	ErrTorrentUploadStateConflict       = errors.New("torrent upload state conflict")
	ErrTorrentUploadStorageUnavailable  = errors.New("torrent upload storage is unavailable")
)

// TorrentUploadState is the durable bridge between PostgreSQL and object
// storage. A response may be retried after any step without creating a second
// swarm or overwriting an immutable object.
type TorrentUploadState string

const (
	TorrentUploadReserved       TorrentUploadState = "reserved"
	TorrentUploadObjectVerified TorrentUploadState = "object_verified"
	TorrentUploadCompleted      TorrentUploadState = "completed"
	TorrentUploadCleaning       TorrentUploadState = "cleaning"
	TorrentUploadAbandoned      TorrentUploadState = "abandoned"
)

type TorrentUploadInput struct {
	ID                  uuid.UUID
	CategoryID          string
	Title               string
	Subtitle            string
	Description         string
	MediaInfo           string
	Anonymous           bool
	PurchasePrice       int64
	ExternalIdentifiers []ExternalIdentifier
	FacetSelections     []FacetSelection
	Screenshots         []TorrentScreenshotInput
	RawMetainfo         []byte
}

// TorrentUploadResult is deliberately a submission projection rather than a
// public listing row. New uploads remain pending review and cannot enter the
// Tracker allowlist until a separate reviewed transition succeeds.
type TorrentUploadResult struct {
	ID             TorrentID
	InfoHashV1     InfoHashV1
	State          State
	ContentName    string
	TotalSizeBytes int64
	FileCount      int
	SubmittedAt    time.Time
}

type ReserveTorrentUploadCommand struct {
	ID                 uuid.UUID
	UploaderID         uuid.UUID
	RequestFingerprint ObjectSHA256
	ObjectID           uuid.UUID
	CategoryID         string
	InfoHashV1         InfoHashV1
	Descriptor         StoredObjectDescriptor
	BackendID          StorageBackendID
	ObjectKey          ObjectKey
	PolicyRevisionID   uuid.UUID
	OccurredAt         time.Time
}

type TorrentUploadReservation struct {
	ID                 uuid.UUID
	UploaderID         uuid.UUID
	RequestFingerprint ObjectSHA256
	ObjectID           uuid.UUID
	CategoryID         string
	InfoHashV1         InfoHashV1
	Descriptor         StoredObjectDescriptor
	BackendID          StorageBackendID
	ObjectKey          ObjectKey
	PolicyRevisionID   uuid.UUID
	State              TorrentUploadState
	ObjectCreated      bool
	StorageVersionID   string
	ObjectVerifiedAt   *time.Time
	CompletedAt        *time.Time
	CreatedAt          time.Time
	Result             *TorrentUploadResult
}

type RecordTorrentUploadObjectCommand struct {
	UploadID           uuid.UUID
	UploaderID         uuid.UUID
	RequestFingerprint ObjectSHA256
	BackendID          StorageBackendID
	ObjectKey          ObjectKey
	StorageVersionID   string
	ObjectCreated      bool
	Descriptor         StoredObjectDescriptor
	VerifiedAt         time.Time
}

type FinalizeTorrentUploadCommand struct {
	UploadID           uuid.UUID
	RequestFingerprint ObjectSHA256
	Torrent            Torrent
	Files              []File
	Screenshots        []storedTorrentScreenshot
	PolicyRevisionID   uuid.UUID
	OccurredAt         time.Time
}

type TorrentUploadRepository interface {
	Reserve(context.Context, ReserveTorrentUploadCommand) (TorrentUploadReservation, error)
	RecordObjectVerified(context.Context, RecordTorrentUploadObjectCommand) (TorrentUploadReservation, error)
	Finalize(context.Context, FinalizeTorrentUploadCommand) (TorrentUploadResult, error)
}

// TrustedTorrentPublisher is implemented by the review context. Upload owns
// parsing and durable object creation; only review may transition a verified
// pending aggregate into Tracker-visible published state.
type TrustedTorrentPublisher interface {
	PublishTrusted(context.Context, TrustedPublishCommand) (TrustedPublishResult, error)
}

type TorrentUploadEntitlementChecker interface {
	HasEntitlementAt(context.Context, uuid.UUID, workgroups.Entitlement, time.Time) (bool, error)
}

type TrustedPublishCommand struct {
	DecisionID    uuid.UUID
	TorrentID     TorrentID
	UploaderID    uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type TrustedPublishResult struct {
	TorrentID TorrentID
	State     State
	Version   int64
}

type TorrentUploadSessionAuthenticator interface {
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type TorrentUploadServiceConfig struct {
	ActiveBackendID            StorageBackendID
	MaxMetainfoBytes           int
	Now                        func() time.Time
	NewUUID                    func() uuid.UUID
	TrustedPublishEntitlements TorrentUploadEntitlementChecker
	TrustedPublisher           TrustedTorrentPublisher
	UploadPolicies             interface {
		Effective(context.Context, time.Time) (UploadPolicyRevision, error)
		ByID(context.Context, uuid.UUID) (UploadPolicyRevision, error)
	}
}

type TorrentUploadService struct {
	authenticator              TorrentUploadSessionAuthenticator
	authorizer                 authz.Authorizer
	repository                 TorrentUploadRepository
	stores                     *StoreRegistry
	activeBackendID            StorageBackendID
	maxMetainfoBytes           int
	now                        func() time.Time
	newUUID                    func() uuid.UUID
	trustedPublishEntitlements TorrentUploadEntitlementChecker
	trustedPublisher           TrustedTorrentPublisher
	uploadPolicies             interface {
		Effective(context.Context, time.Time) (UploadPolicyRevision, error)
		ByID(context.Context, uuid.UUID) (UploadPolicyRevision, error)
	}
}

func NewTorrentUploadService(
	authenticator TorrentUploadSessionAuthenticator,
	authorizer authz.Authorizer,
	repository TorrentUploadRepository,
	stores *StoreRegistry,
	config TorrentUploadServiceConfig,
) (*TorrentUploadService, error) {
	if authenticator == nil || authorizer == nil || repository == nil || stores == nil || config.ActiveBackendID == "" {
		return nil, errors.New("torrent upload service dependencies are required")
	}
	if (config.TrustedPublishEntitlements == nil) != (config.TrustedPublisher == nil) {
		return nil, errors.New("trusted torrent publish dependencies must be configured together")
	}
	if _, exists := stores.Get(config.ActiveBackendID); !exists {
		return nil, errors.New("active torrent upload backend is not configured")
	}
	if config.MaxMetainfoBytes == 0 {
		config.MaxMetainfoBytes = MaxMetainfoBytes
	}
	if config.MaxMetainfoBytes < 1 || config.MaxMetainfoBytes > MaxMetainfoBytes {
		return nil, errors.New("torrent upload metainfo limit is invalid")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewUUID == nil {
		config.NewUUID = uuid.New
	}
	return &TorrentUploadService{
		authenticator: authenticator, authorizer: authorizer, repository: repository,
		stores: stores, activeBackendID: config.ActiveBackendID,
		maxMetainfoBytes: config.MaxMetainfoBytes, now: config.Now, newUUID: config.NewUUID,
		trustedPublishEntitlements: config.TrustedPublishEntitlements,
		trustedPublisher:           config.TrustedPublisher,
		uploadPolicies:             config.UploadPolicies,
	}, nil
}

// Submit authenticates and authorizes before parsing attacker-controlled
// metainfo, reserves the swarm identity, publishes without overwrite, performs
// a complete read-back, then commits object/location/torrent/file rows in one
// database transaction. Every boundary is safe to retry with the same ID.
func (service *TorrentUploadService) Submit(ctx context.Context, cookieToken, csrfToken string, input TorrentUploadInput) (TorrentUploadResult, error) {
	if input.ID == uuid.Nil {
		return TorrentUploadResult{}, ErrTorrentInputInvalid
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	return service.submitAuthorized(ctx, session.User, input)
}

// SubmitForIntegration shares the canonical upload pipeline with API-key
// clients. Authentication is already complete, but email verification and the
// ordinary torrent.submit authorization are deliberately enforced again here.
func (service *TorrentUploadService) SubmitForIntegration(ctx context.Context, user identity.User, input TorrentUploadInput) (TorrentUploadResult, error) {
	if input.ID == uuid.Nil || user.ID == uuid.Nil {
		return TorrentUploadResult{}, ErrTorrentInputInvalid
	}
	return service.submitAuthorized(ctx, user, input)
}

func (service *TorrentUploadService) submitAuthorized(ctx context.Context, user identity.User, input TorrentUploadInput) (TorrentUploadResult, error) {
	if user.EmailVerifiedAt == nil {
		return TorrentUploadResult{}, ErrTorrentUploadEmailUnverified
	}
	now := service.now().UTC()
	authorization, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, user.ID, authz.ActionTorrentSubmit, now)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	if len(input.RawMetainfo) > service.maxMetainfoBytes {
		return TorrentUploadResult{}, validationFailure(CodeObjectTooLarge, "metainfo", 0, "object exceeds the upload byte policy")
	}
	metainfo, err := ParseV1(input.RawMetainfo, ValidationProfileStrictUpload)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	preparedScreenshots, err := prepareTorrentScreenshots(input.Screenshots, service.newUUID)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	policy := defaultUploadPolicyRevision(service.maxMetainfoBytes)
	if service.uploadPolicies != nil {
		policy, err = service.uploadPolicies.Effective(ctx, now)
		if err != nil {
			return TorrentUploadResult{}, fmt.Errorf("resolve effective torrent upload policy: %w", err)
		}
	}

	// Construct once before reservation so normalized metadata, parser-derived
	// identity and all domain bounds participate in the idempotency fingerprint.
	candidate, err := NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: user.ID,
		CategoryID: input.CategoryID, Title: input.Title, Subtitle: input.Subtitle,
		Description: input.Description, MediaInfo: input.MediaInfo, Anonymous: input.Anonymous, PurchasePrice: input.PurchasePrice,
		ExternalIdentifiers: input.ExternalIdentifiers,
		FacetSelections:     input.FacetSelections,
		Screenshots:         screenshotMetadata(preparedScreenshots),
		ObjectID:            service.newUUID(), Metainfo: metainfo, OccurredAt: now,
	})
	if err != nil {
		return TorrentUploadResult{}, err
	}
	fingerprint := torrentUploadFingerprint(candidate)
	descriptor := StoredObjectDescriptor{SHA256: metainfo.ObjectSHA256, ByteLength: metainfo.ObjectByteLength}
	reservation, err := service.repository.Reserve(ctx, ReserveTorrentUploadCommand{
		ID: input.ID, UploaderID: user.ID, RequestFingerprint: fingerprint,
		ObjectID: candidate.Object.ID, CategoryID: candidate.CategoryID,
		InfoHashV1: candidate.InfoHashV1, Descriptor: descriptor,
		BackendID: service.activeBackendID, ObjectKey: TorrentObjectKey(descriptor.SHA256), OccurredAt: now,
		PolicyRevisionID: policy.ID,
	})
	if err != nil {
		return TorrentUploadResult{}, err
	}
	if reservation.State == TorrentUploadCompleted && reservation.Result != nil {
		if service.trustedPublisher != nil && reservation.CompletedAt == nil {
			return TorrentUploadResult{}, ErrTorrentUploadStateConflict
		}
		completedAt := reservation.CreatedAt
		if reservation.CompletedAt != nil {
			completedAt = *reservation.CompletedAt
		}
		return service.publishTrustedIfEligible(ctx, reservation, *reservation.Result, authorization, completedAt)
	}
	if reservation.State != TorrentUploadReserved && reservation.State != TorrentUploadObjectVerified {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	if service.uploadPolicies != nil && reservation.PolicyRevisionID != policy.ID {
		policy, err = service.uploadPolicies.ByID(ctx, reservation.PolicyRevisionID)
		if err != nil {
			return TorrentUploadResult{}, fmt.Errorf("resolve reserved torrent upload policy: %w", err)
		}
	}
	if err := validateUploadAgainstPolicy(policy, input.RawMetainfo, metainfo, preparedScreenshots); err != nil {
		return TorrentUploadResult{}, err
	}

	// Existing reservations own their original IDs and submission time. This is
	// what makes a response-loss retry return one stable aggregate.
	pending, err := NewPendingTorrent(NewPendingTorrentInput{
		UploaderID: user.ID,
		CategoryID: input.CategoryID, Title: input.Title, Subtitle: input.Subtitle,
		Description: input.Description, MediaInfo: input.MediaInfo, Anonymous: input.Anonymous, PurchasePrice: input.PurchasePrice,
		ExternalIdentifiers: input.ExternalIdentifiers,
		FacetSelections:     input.FacetSelections,
		Screenshots:         screenshotMetadata(preparedScreenshots),
		ObjectID:            reservation.ObjectID, Metainfo: metainfo, OccurredAt: reservation.CreatedAt,
	})
	if err != nil || torrentUploadFingerprint(pending) != fingerprint {
		return TorrentUploadResult{}, ErrTorrentUploadIdempotencyConflict
	}

	if reservation.State == TorrentUploadReserved {
		reservation, err = service.storeAndVerify(ctx, reservation, input.RawMetainfo)
		if err != nil {
			return TorrentUploadResult{}, err
		}
	}
	storedScreenshots, err := service.storeAndVerifyScreenshots(ctx, preparedScreenshots)
	if err != nil {
		return TorrentUploadResult{}, err
	}
	finalizedAt := service.now().UTC()
	result, err := service.repository.Finalize(ctx, FinalizeTorrentUploadCommand{
		UploadID: reservation.ID, RequestFingerprint: fingerprint, Torrent: pending,
		Files: append([]File(nil), metainfo.Files...), Screenshots: storedScreenshots,
		PolicyRevisionID: reservation.PolicyRevisionID,
		OccurredAt:       finalizedAt,
	})
	if err != nil {
		return TorrentUploadResult{}, err
	}
	return service.publishTrustedIfEligible(ctx, reservation, result, authorization, finalizedAt)
}

func (service *TorrentUploadService) publishTrustedIfEligible(
	ctx context.Context,
	reservation TorrentUploadReservation,
	result TorrentUploadResult,
	authorization authz.Decision,
	occurredAt time.Time,
) (TorrentUploadResult, error) {
	if service.trustedPublisher == nil {
		return result, nil
	}
	eligible, err := service.trustedPublishEntitlements.HasEntitlementAt(
		ctx, reservation.UploaderID, workgroups.EntitlementTrustedTorrentPublish, occurredAt,
	)
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("resolve trusted publish entitlement: %w", err)
	}
	if !eligible {
		return result, nil
	}
	decisionID := uuid.NewSHA1(uuid.NameSpaceOID, append([]byte("peergo:trusted-publish:"), reservation.ID[:]...))
	published, err := service.trustedPublisher.PublishTrusted(ctx, TrustedPublishCommand{
		DecisionID: decisionID, TorrentID: result.ID, UploaderID: reservation.UploaderID,
		OccurredAt: occurredAt, Authorization: authorization,
	})
	if err != nil {
		return TorrentUploadResult{}, fmt.Errorf("publish trusted torrent: %w", err)
	}
	result.State = published.State
	return result, nil
}

func (service *TorrentUploadService) storeAndVerifyScreenshots(ctx context.Context, screenshots []preparedTorrentScreenshot) ([]storedTorrentScreenshot, error) {
	return storeAndVerifyTorrentScreenshots(ctx, service.stores, service.activeBackendID, screenshots)
}

func storeAndVerifyTorrentScreenshots(ctx context.Context, stores *StoreRegistry, activeBackendID StorageBackendID, screenshots []preparedTorrentScreenshot) ([]storedTorrentScreenshot, error) {
	store, exists := stores.Get(activeBackendID)
	if !exists {
		return nil, ErrTorrentUploadStorageUnavailable
	}
	result := make([]storedTorrentScreenshot, 0, len(screenshots))
	for _, screenshot := range screenshots {
		descriptor := StoredObjectDescriptor{SHA256: screenshot.ContentSHA256, ByteLength: screenshot.ByteLength}
		key := TorrentScreenshotObjectKey(screenshot.ContentSHA256, screenshot.Extension)
		writeResult, err := store.PutIfAbsent(ctx, key, bytes.NewReader(screenshot.Raw), descriptor)
		if err != nil {
			return nil, fmt.Errorf("%w: publish immutable torrent screenshot: %v", ErrTorrentUploadStorageUnavailable, err)
		}
		object, err := store.Open(ctx, key, writeResult.VersionID)
		if err != nil || object.Body == nil {
			return nil, fmt.Errorf("%w: open torrent screenshot read-back: %v", ErrTorrentUploadStorageUnavailable, err)
		}
		verified, verifyErr := VerifyStoredObject(object, descriptor)
		closeErr := object.Body.Close()
		if verifyErr != nil || closeErr != nil || verified != descriptor {
			return nil, fmt.Errorf("%w: verify torrent screenshot read-back", ErrTorrentUploadStorageUnavailable)
		}
		versionID := object.VersionID
		if versionID == "" {
			versionID = writeResult.VersionID
		}
		result = append(result, storedTorrentScreenshot{
			TorrentScreenshot: screenshot.TorrentScreenshot,
			BackendID:         activeBackendID, ObjectKey: key, StorageVersionID: versionID,
		})
	}
	return result, nil
}

func (service *TorrentUploadService) storeAndVerify(ctx context.Context, reservation TorrentUploadReservation, raw []byte) (TorrentUploadReservation, error) {
	store, exists := service.stores.Get(reservation.BackendID)
	if !exists {
		return TorrentUploadReservation{}, ErrTorrentUploadStorageUnavailable
	}
	writeResult, err := store.PutIfAbsent(ctx, reservation.ObjectKey, bytes.NewReader(raw), reservation.Descriptor)
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("%w: publish immutable torrent object: %v", ErrTorrentUploadStorageUnavailable, err)
	}
	object, err := store.Open(ctx, reservation.ObjectKey, writeResult.VersionID)
	if err != nil || object.Body == nil {
		return TorrentUploadReservation{}, fmt.Errorf("%w: open torrent object read-back: %v", ErrTorrentUploadStorageUnavailable, err)
	}
	verified, verifyErr := VerifyStoredObject(object, reservation.Descriptor)
	closeErr := object.Body.Close()
	if verifyErr != nil {
		return TorrentUploadReservation{}, fmt.Errorf("%w: verify torrent object read-back: %v", ErrTorrentUploadStorageUnavailable, verifyErr)
	}
	if closeErr != nil {
		return TorrentUploadReservation{}, fmt.Errorf("%w: close torrent object read-back: %v", ErrTorrentUploadStorageUnavailable, closeErr)
	}
	versionID := object.VersionID
	if versionID == "" {
		versionID = writeResult.VersionID
	}
	result, err := service.repository.RecordObjectVerified(ctx, RecordTorrentUploadObjectCommand{
		UploadID: reservation.ID, UploaderID: reservation.UploaderID,
		RequestFingerprint: reservation.RequestFingerprint, BackendID: reservation.BackendID,
		ObjectKey: reservation.ObjectKey, StorageVersionID: versionID,
		ObjectCreated: writeResult.Created, Descriptor: verified, VerifiedAt: service.now().UTC(),
	})
	if err != nil {
		return TorrentUploadReservation{}, fmt.Errorf("record verified torrent upload object: %w", err)
	}
	return result, nil
}

func torrentUploadFingerprint(torrent Torrent) ObjectSHA256 {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(uploadFingerprintDomain))
	_, _ = hasher.Write(torrent.UploaderID[:])
	writeFingerprintString(hasher, torrent.CategoryID)
	writeFingerprintString(hasher, torrent.Title)
	writeFingerprintString(hasher, torrent.Subtitle)
	writeFingerprintString(hasher, torrent.DescriptionFormat)
	writeFingerprintString(hasher, torrent.Description)
	writeFingerprintString(hasher, torrent.MediaInfo)
	if torrent.Anonymous {
		_, _ = hasher.Write([]byte{1})
	} else {
		_, _ = hasher.Write([]byte{0})
	}
	var purchasePrice [8]byte
	binary.BigEndian.PutUint64(purchasePrice[:], uint64(torrent.PurchasePrice))
	_, _ = hasher.Write(purchasePrice[:])
	for _, identifier := range torrent.ExternalIdentifiers {
		writeFingerprintString(hasher, identifier.Provider)
		writeFingerprintString(hasher, identifier.ExternalID)
	}
	for _, selection := range torrent.FacetSelections {
		writeFingerprintString(hasher, selection.FacetID)
		for _, optionKey := range selection.OptionKeys {
			writeFingerprintString(hasher, optionKey)
		}
	}
	for _, screenshot := range torrent.Screenshots {
		_, _ = hasher.Write(screenshot.ContentSHA256[:])
		writeFingerprintString(hasher, screenshot.ContentType)
		writeFingerprintString(hasher, screenshot.Extension)
	}
	_, _ = hasher.Write(torrent.InfoHashV1[:])
	_, _ = hasher.Write(torrent.Object.ContentSHA256[:])
	var digest ObjectSHA256
	copy(digest[:], hasher.Sum(nil))
	return digest
}

type fingerprintWriter interface {
	Write([]byte) (int, error)
}

func writeFingerprintString(writer fingerprintWriter, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

type TorrentUploadCleanupTask struct {
	UploadID         uuid.UUID
	BackendID        StorageBackendID
	ObjectKey        ObjectKey
	StorageVersionID string
	DeleteObject     bool
	LeaseToken       uuid.UUID
	Attempts         int32
}

type TorrentUploadOrphanRepository interface {
	ClaimUploadCleanupTasks(context.Context, StorageBackendID, time.Time, time.Time, int32, time.Duration) ([]TorrentUploadCleanupTask, error)
	MarkUploadAbandoned(context.Context, TorrentUploadCleanupTask, time.Time) error
	ReleaseUploadCleanupTask(context.Context, TorrentUploadCleanupTask, time.Time, time.Time, string) error
}

type TorrentUploadOrphanServiceConfig struct {
	Retention     time.Duration
	BatchSize     int32
	LeaseDuration time.Duration
	Now           func() time.Time
}

// TorrentUploadOrphanService only deletes objects whose successful creation
// was durably attributed to the abandoned attempt. Unknown/pre-existing keys
// are released from the reservation without deletion, preferring a leak over
// destroying bytes that may belong to an external recovery workflow.
type TorrentUploadOrphanService struct {
	repository    TorrentUploadOrphanRepository
	stores        *StoreRegistry
	retention     time.Duration
	batchSize     int32
	leaseDuration time.Duration
	now           func() time.Time
}

func NewTorrentUploadOrphanService(repository TorrentUploadOrphanRepository, stores *StoreRegistry, config TorrentUploadOrphanServiceConfig) (*TorrentUploadOrphanService, error) {
	if repository == nil || stores == nil {
		return nil, errors.New("torrent upload orphan service dependencies are required")
	}
	if config.Retention == 0 {
		config.Retention = minimumUploadOrphanRetention
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultUploadCleanupBatchSize
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultUploadCleanupLeaseDuration
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Retention < minimumUploadOrphanRetention || config.Retention > maximumUploadOrphanRetention ||
		config.BatchSize < 1 || config.BatchSize > 100 || config.LeaseDuration <= 0 || config.LeaseDuration > 10*time.Minute {
		return nil, errors.New("torrent upload orphan service configuration is invalid")
	}
	return &TorrentUploadOrphanService{
		repository: repository, stores: stores, retention: config.Retention,
		batchSize: config.BatchSize, leaseDuration: config.LeaseDuration, now: config.Now,
	}, nil
}

func (service *TorrentUploadOrphanService) RunBatch(ctx context.Context, backendID StorageBackendID) (int, error) {
	store, exists := service.stores.Get(backendID)
	if backendID == "" || !exists {
		return 0, ErrStorageInputInvalid
	}
	now := service.now().UTC()
	tasks, err := service.repository.ClaimUploadCleanupTasks(
		ctx, backendID, now.Add(-service.retention), now, service.batchSize, service.leaseDuration,
	)
	if err != nil {
		return 0, fmt.Errorf("claim abandoned torrent uploads: %w", err)
	}
	var failures []error
	for _, task := range tasks {
		if task.DeleteObject {
			if err := store.Delete(ctx, task.ObjectKey, task.StorageVersionID); err != nil {
				failures = append(failures, service.releaseFailure(ctx, task, "upload_orphan_delete_failed", err))
				continue
			}
		}
		if err := service.repository.MarkUploadAbandoned(ctx, task, service.now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("record abandoned torrent upload %s: %w", task.UploadID, err))
		}
	}
	return len(tasks), errors.Join(failures...)
}

func (service *TorrentUploadOrphanService) releaseFailure(ctx context.Context, task TorrentUploadCleanupTask, code string, cause error) error {
	releasedAt := service.now().UTC()
	retryAt := releasedAt.Add(storageRetryDelay(task.Attempts))
	if err := service.repository.ReleaseUploadCleanupTask(ctx, task, releasedAt, retryAt, code); err != nil {
		return fmt.Errorf("clean abandoned torrent upload %s: %w; release task: %v", task.UploadID, cause, err)
	}
	return fmt.Errorf("clean abandoned torrent upload %s: %w", task.UploadID, cause)
}
