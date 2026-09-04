package torrents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestTorrentUploadIsRecoverableAndIdempotentAfterDatabaseFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 18, 0, 0, 0, time.UTC)
	userID := uuid.New()
	verifiedAt := now.Add(-time.Hour)
	authenticator := staticTorrentUploadAuthenticator{session: identity.WebSession{
		User: identity.User{ID: userID, EmailVerifiedAt: &verifiedAt},
	}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	repository := &memoryTorrentUploadRepository{finalizeFailures: 1}
	store := newMemoryObjectStore("local-primary")
	registry, err := NewStoreRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTorrentUploadService(authenticator, authorizer, repository, registry, TorrentUploadServiceConfig{
		ActiveBackendID: store.BackendID(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := validSingleFixture("release.bin", 42, 16*1024)
	input := TorrentUploadInput{
		ID: uuid.New(), CategoryID: " movies ", Title: " Release 2026 ",
		Subtitle: " First edition ", RawMetainfo: raw,
	}

	if _, err := service.Submit(context.Background(), "cookie", "csrf", input); !errors.Is(err, errSimulatedUploadCommit) {
		t.Fatalf("first Submit() error = %v", err)
	}
	if repository.reservation.State != TorrentUploadObjectVerified || repository.recordCalls != 1 || repository.finalizeCalls != 1 {
		t.Fatalf("reservation after failed finalization = %+v; record=%d finalize=%d", repository.reservation, repository.recordCalls, repository.finalizeCalls)
	}
	if len(store.objects) != 1 {
		t.Fatalf("stored objects after failed finalization = %d", len(store.objects))
	}

	// A page reload generates a new request ID. The repository returns the
	// original reservation for an exact request and finalization must use that
	// durable ID, not the newly generated browser key.
	input.ID = uuid.New()
	result, err := service.Submit(context.Background(), "cookie", "csrf", input)
	if err != nil {
		t.Fatalf("retry Submit() error = %v", err)
	}
	if result.State != StatePendingReview || result.ID != 42 || result.FileCount != 1 || result.TotalSizeBytes != 42 {
		t.Fatalf("result = %+v", result)
	}
	if repository.recordCalls != 1 || repository.finalizeCalls != 2 || len(store.objects) != 1 {
		t.Fatalf("retry record=%d finalize=%d objects=%d", repository.recordCalls, repository.finalizeCalls, len(store.objects))
	}

	replayed, err := service.Submit(context.Background(), "cookie", "csrf", input)
	if err != nil || replayed != result {
		t.Fatalf("completed replay = %+v, %v; want %+v", replayed, err, result)
	}
	if repository.finalizeCalls != 2 {
		t.Fatalf("completed replay finalized again: %d", repository.finalizeCalls)
	}
	if authorizer.request.Action != authz.ActionTorrentSubmit || authorizer.request.Resource.OwnerID != userID {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestTorrentUploadRejectsUnverifiedAccountBeforeAuthorizationOrStorage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 19, 0, 0, 0, time.UTC)
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	repository := &memoryTorrentUploadRepository{}
	store := newMemoryObjectStore("local-primary")
	registry, _ := NewStoreRegistry(store)
	service, _ := NewTorrentUploadService(
		staticTorrentUploadAuthenticator{session: identity.WebSession{User: identity.User{ID: uuid.New()}}},
		authorizer, repository, registry,
		TorrentUploadServiceConfig{ActiveBackendID: store.BackendID(), Now: func() time.Time { return now }},
	)
	_, err := service.Submit(context.Background(), "cookie", "csrf", TorrentUploadInput{
		ID: uuid.New(), CategoryID: "movies", Title: "Release",
		RawMetainfo: validSingleFixture("release.bin", 1, 16*1024),
	})
	if !errors.Is(err, ErrTorrentUploadEmailUnverified) {
		t.Fatalf("Submit() error = %v", err)
	}
	if authorizer.calls != 0 || repository.reserveCalls != 0 || len(store.objects) != 0 {
		t.Fatalf("unverified path authorization=%d reservations=%d objects=%d", authorizer.calls, repository.reserveCalls, len(store.objects))
	}
}

func TestTorrentUploadRejectsCorruptReadbackWithoutFinalizing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 20, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	store := newMemoryObjectStore("local-primary")
	store.corruptReadback = true
	registry, _ := NewStoreRegistry(store)
	repository := &memoryTorrentUploadRepository{}
	service, _ := NewTorrentUploadService(
		staticTorrentUploadAuthenticator{session: identity.WebSession{User: identity.User{ID: uuid.New(), EmailVerifiedAt: &verifiedAt}}},
		&recordingTorrentUploadAuthorizer{now: now}, repository, registry,
		TorrentUploadServiceConfig{ActiveBackendID: store.BackendID(), Now: func() time.Time { return now }},
	)
	_, err := service.Submit(context.Background(), "cookie", "csrf", TorrentUploadInput{
		ID: uuid.New(), CategoryID: "movies", Title: "Release",
		RawMetainfo: validSingleFixture("release.bin", 1, 16*1024),
	})
	if !errors.Is(err, ErrTorrentUploadStorageUnavailable) {
		t.Fatalf("Submit() error = %v", err)
	}
	if repository.reservation.State != TorrentUploadReserved || repository.recordCalls != 0 || repository.finalizeCalls != 0 {
		t.Fatalf("corrupt path reservation=%+v record=%d finalize=%d", repository.reservation, repository.recordCalls, repository.finalizeCalls)
	}
}

func TestTorrentUploadOrphanCleanupDeletesOnlyDurablyOwnedObject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 21, 0, 0, 0, time.UTC)
	store := newMemoryObjectStore("local-primary")
	ownedKey := ObjectKey("torrents/sha256/aa/owned.torrent")
	unownedKey := ObjectKey("torrents/sha256/bb/unowned.torrent")
	store.objects[ownedKey] = []byte("owned")
	store.objects[unownedKey] = []byte("unowned")
	repository := &memoryTorrentUploadOrphanRepository{tasks: []TorrentUploadCleanupTask{
		{UploadID: uuid.New(), BackendID: store.BackendID(), ObjectKey: ownedKey, DeleteObject: true, LeaseToken: uuid.New(), Attempts: 1},
		{UploadID: uuid.New(), BackendID: store.BackendID(), ObjectKey: unownedKey, DeleteObject: false, LeaseToken: uuid.New(), Attempts: 1},
	}}
	registry, _ := NewStoreRegistry(store)
	service, err := NewTorrentUploadOrphanService(repository, registry, TorrentUploadOrphanServiceConfig{
		Retention: 24 * time.Hour, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := service.RunBatch(context.Background(), store.BackendID())
	if err != nil || processed != 2 {
		t.Fatalf("RunBatch() processed=%d error=%v", processed, err)
	}
	if _, exists := store.objects[ownedKey]; exists {
		t.Fatal("owned orphan object was not deleted")
	}
	if _, exists := store.objects[unownedKey]; !exists {
		t.Fatal("unowned/pre-existing object was deleted")
	}
	if len(repository.abandoned) != 2 || store.deleteCalls != 1 {
		t.Fatalf("abandoned=%d deletes=%d", len(repository.abandoned), store.deleteCalls)
	}
}

func TestTorrentUploadOrphanCleanupReleasesFailedDeleteWithInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 22, 0, 0, 0, time.UTC)
	store := newMemoryObjectStore("local-primary")
	store.deleteErr = errors.New("object store unavailable")
	key := ObjectKey("torrents/sha256/cc/retry.torrent")
	store.objects[key] = []byte("owned")
	task := TorrentUploadCleanupTask{
		UploadID: uuid.New(), BackendID: store.BackendID(), ObjectKey: key,
		DeleteObject: true, LeaseToken: uuid.New(), Attempts: 2,
	}
	repository := &memoryTorrentUploadOrphanRepository{tasks: []TorrentUploadCleanupTask{task}}
	registry, _ := NewStoreRegistry(store)
	service, _ := NewTorrentUploadOrphanService(repository, registry, TorrentUploadOrphanServiceConfig{
		Retention: 24 * time.Hour, Now: func() time.Time { return now },
	})

	processed, err := service.RunBatch(context.Background(), store.BackendID())
	if processed != 1 || err == nil {
		t.Fatalf("RunBatch() processed=%d error=%v", processed, err)
	}
	if len(repository.abandoned) != 0 || len(repository.released) != 1 {
		t.Fatalf("abandoned=%d released=%d", len(repository.abandoned), len(repository.released))
	}
	release := repository.released[0]
	if release.task.UploadID != task.UploadID || !release.releasedAt.Equal(now) || !release.retryAt.Equal(now.Add(2*time.Second)) || release.code != "upload_orphan_delete_failed" {
		t.Fatalf("release = %+v", release)
	}
	if _, exists := store.objects[key]; !exists {
		t.Fatal("failed cleanup removed the object")
	}
}

type staticTorrentUploadAuthenticator struct {
	session identity.WebSession
	err     error
}

func (authenticator staticTorrentUploadAuthenticator) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return authenticator.session, authenticator.err
}

type recordingTorrentUploadAuthorizer struct {
	now     time.Time
	request authz.Request
	calls   int
}

func (authorizer *recordingTorrentUploadAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	authorizer.calls++
	authorizer.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: authorizer.now.Add(time.Hour),
	}, nil
}

var errSimulatedUploadCommit = errors.New("simulated upload commit failure")

type memoryTorrentUploadRepository struct {
	reservation      TorrentUploadReservation
	reserveCalls     int
	recordCalls      int
	finalizeCalls    int
	finalizeFailures int
}

func (repository *memoryTorrentUploadRepository) Reserve(_ context.Context, command ReserveTorrentUploadCommand) (TorrentUploadReservation, error) {
	repository.reserveCalls++
	if repository.reservation.ID == uuid.Nil {
		repository.reservation = TorrentUploadReservation{
			ID: command.ID, UploaderID: command.UploaderID, RequestFingerprint: command.RequestFingerprint,
			ObjectID: command.ObjectID, CategoryID: command.CategoryID,
			InfoHashV1: command.InfoHashV1, Descriptor: command.Descriptor,
			BackendID: command.BackendID, ObjectKey: command.ObjectKey,
			State: TorrentUploadReserved, CreatedAt: command.OccurredAt,
		}
	} else if repository.reservation.UploaderID != command.UploaderID ||
		repository.reservation.RequestFingerprint != command.RequestFingerprint ||
		repository.reservation.CategoryID != command.CategoryID ||
		repository.reservation.InfoHashV1 != command.InfoHashV1 ||
		repository.reservation.Descriptor != command.Descriptor {
		return TorrentUploadReservation{}, ErrTorrentUploadIdempotencyConflict
	}
	return repository.reservation, nil
}

func (repository *memoryTorrentUploadRepository) RecordObjectVerified(_ context.Context, command RecordTorrentUploadObjectCommand) (TorrentUploadReservation, error) {
	repository.recordCalls++
	repository.reservation.State = TorrentUploadObjectVerified
	repository.reservation.ObjectCreated = command.ObjectCreated
	repository.reservation.StorageVersionID = command.StorageVersionID
	verifiedAt := command.VerifiedAt
	repository.reservation.ObjectVerifiedAt = &verifiedAt
	return repository.reservation, nil
}

func (repository *memoryTorrentUploadRepository) Finalize(_ context.Context, command FinalizeTorrentUploadCommand) (TorrentUploadResult, error) {
	repository.finalizeCalls++
	if command.UploadID != repository.reservation.ID {
		return TorrentUploadResult{}, ErrTorrentUploadStateConflict
	}
	if repository.finalizeFailures > 0 {
		repository.finalizeFailures--
		return TorrentUploadResult{}, errSimulatedUploadCommit
	}
	result := TorrentUploadResult{
		ID: 42, InfoHashV1: command.Torrent.InfoHashV1,
		State: StatePendingReview, ContentName: command.Torrent.ContentName,
		TotalSizeBytes: command.Torrent.TotalSizeBytes, FileCount: command.Torrent.FileCount,
		SubmittedAt: command.Torrent.SubmittedAt,
	}
	repository.reservation.State = TorrentUploadCompleted
	repository.reservation.Result = &result
	return result, nil
}

type memoryTorrentUploadOrphanRepository struct {
	tasks     []TorrentUploadCleanupTask
	abandoned []TorrentUploadCleanupTask
	released  []torrentUploadCleanupRelease
}

type torrentUploadCleanupRelease struct {
	task       TorrentUploadCleanupTask
	releasedAt time.Time
	retryAt    time.Time
	code       string
}

func (repository *memoryTorrentUploadOrphanRepository) ClaimUploadCleanupTasks(_ context.Context, _ StorageBackendID, _, _ time.Time, _ int32, _ time.Duration) ([]TorrentUploadCleanupTask, error) {
	return append([]TorrentUploadCleanupTask(nil), repository.tasks...), nil
}

func (repository *memoryTorrentUploadOrphanRepository) MarkUploadAbandoned(_ context.Context, task TorrentUploadCleanupTask, _ time.Time) error {
	repository.abandoned = append(repository.abandoned, task)
	return nil
}

func (repository *memoryTorrentUploadOrphanRepository) ReleaseUploadCleanupTask(_ context.Context, task TorrentUploadCleanupTask, releasedAt, retryAt time.Time, code string) error {
	repository.released = append(repository.released, torrentUploadCleanupRelease{
		task: task, releasedAt: releasedAt, retryAt: retryAt, code: code,
	})
	return nil
}

func TestTorrentUploadFingerprintIncludesPurchasePrice(t *testing.T) {
	t.Parallel()
	base := Torrent{UploaderID: uuid.New(), CategoryID: "movies", Title: "Release"}
	priced := base
	priced.PurchasePrice = 100
	if torrentUploadFingerprint(base) == torrentUploadFingerprint(priced) {
		t.Fatal("purchase price did not participate in upload idempotency fingerprint")
	}
}
