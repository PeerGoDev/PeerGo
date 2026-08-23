package torrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image/color"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
)

type torrentReadRepositoryFixture struct {
	detail          PublicDetail
	cover           PublicCoverSource
	screenshot      PublicScreenshotSource
	content         PublicContent
	related         []catalog.Torrent
	files           PublicFilePage
	submissions     MySubmissionPage
	err             error
	detailID        TorrentID
	coverID         TorrentID
	screenshotID    TorrentID
	screenshotPos   int
	contentID       TorrentID
	relatedID       TorrentID
	relatedLimit    int
	fileID          TorrentID
	fileLimit       int
	fileOffset      int
	submissionOwner uuid.UUID
	submissionLimit int
}

func (fixture *torrentReadRepositoryFixture) PublishedDetail(_ context.Context, id TorrentID) (PublicDetail, error) {
	fixture.detailID = id
	return fixture.detail, fixture.err
}

func (fixture *torrentReadRepositoryFixture) PublishedCoverSource(_ context.Context, id TorrentID) (PublicCoverSource, error) {
	fixture.coverID = id
	return fixture.cover, fixture.err
}

func (fixture *torrentReadRepositoryFixture) PublishedScreenshotSource(_ context.Context, id TorrentID, position int) (PublicScreenshotSource, error) {
	fixture.screenshotID, fixture.screenshotPos = id, position
	return fixture.screenshot, fixture.err
}

func (fixture *torrentReadRepositoryFixture) PublishedContent(_ context.Context, id TorrentID) (PublicContent, error) {
	fixture.contentID = id
	return fixture.content, fixture.err
}

func (fixture *torrentReadRepositoryFixture) PublishedRelatedVersions(_ context.Context, id TorrentID, limit int) ([]catalog.Torrent, error) {
	fixture.relatedID, fixture.relatedLimit = id, limit
	return fixture.related, fixture.err
}

func (fixture *torrentReadRepositoryFixture) PublishedFiles(_ context.Context, id TorrentID, limit, offset int) (PublicFilePage, error) {
	fixture.fileID = id
	fixture.fileLimit = limit
	fixture.fileOffset = offset
	return fixture.files, fixture.err
}

func (fixture *torrentReadRepositoryFixture) UserSubmissions(_ context.Context, owner uuid.UUID, limit int) (MySubmissionPage, error) {
	fixture.submissionOwner = owner
	fixture.submissionLimit = limit
	return fixture.submissions, fixture.err
}

type rejectingTorrentReadAuthorizer struct{}

func (rejectingTorrentReadAuthorizer) Authorize(context.Context, authz.Request) (authz.Decision, error) {
	return authz.Decision{}, authz.ErrForbidden
}

type torrentDerivativeFixture struct {
	ready    imaging.ReadyDerivative
	err      error
	sourceID uuid.UUID
	variant  imaging.Variant
}

func (fixture *torrentDerivativeFixture) ReadyForTorrentScreenshot(_ context.Context, sourceID uuid.UUID, variant imaging.Variant) (imaging.ReadyDerivative, error) {
	fixture.sourceID, fixture.variant = sourceID, variant
	return fixture.ready, fixture.err
}

func TestTorrentReadKeepsPublicQueriesAnonymousAndBounded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	const torrentID TorrentID = 42
	repository := &torrentReadRepositoryFixture{
		detail: PublicDetail{ID: torrentID},
		content: PublicContent{
			TorrentID: torrentID, Description: "发布说明",
			DescriptionFormat: "markdown", MediaInfo: "General",
		},
		related: []catalog.Torrent{{ID: 43, Name: "4K version"}},
		files:   PublicFilePage{TorrentID: torrentID, Limit: 25, Offset: 50},
	}
	coverBytes := screenshotPNG(t, 4, 3, color.RGBA{R: 32, G: 64, B: 96, A: 255})
	coverDigest := ObjectSHA256(sha256.Sum256(coverBytes))
	coverStore := newMemoryObjectStore("local-primary")
	coverKey := TorrentScreenshotObjectKey(coverDigest, ".png")
	coverStore.objects[coverKey] = coverBytes
	coverLocationID := uuid.New()
	repository.cover = PublicCoverSource{
		TorrentID: torrentID, ObjectID: uuid.New(), ContentType: "image/png",
		Width: 4, Height: 3,
		Descriptor: StoredObjectDescriptor{SHA256: coverDigest, ByteLength: int64(len(coverBytes))},
		Locations: []ReadableObjectLocation{{
			ID: coverLocationID, BackendID: "local-primary", ObjectKey: coverKey,
			Descriptor: StoredObjectDescriptor{SHA256: coverDigest, ByteLength: int64(len(coverBytes))},
			VerifiedAt: now,
		}},
	}
	repository.screenshot = PublicScreenshotSource{
		TorrentID: torrentID, Position: 1, ObjectID: uuid.New(), ContentType: "image/png",
		Width: 4, Height: 3,
		Descriptor: StoredObjectDescriptor{SHA256: coverDigest, ByteLength: int64(len(coverBytes))},
		Locations: []ReadableObjectLocation{{
			ID: uuid.New(), BackendID: "local-primary", ObjectKey: coverKey,
			Descriptor: StoredObjectDescriptor{SHA256: coverDigest, ByteLength: int64(len(coverBytes))},
			VerifiedAt: now,
		}},
	}
	stores, err := NewStoreRegistry(coverStore)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTorrentReadService(
		torrentDownloadAuthenticatorFixture{},
		&recordingTorrentUploadAuthorizer{now: now},
		repository,
		stores,
		nil,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := service.Cover(context.Background(), torrentID)
	if err != nil || !bytes.Equal(cover.Data, coverBytes) || cover.ContentType != "image/png" ||
		cover.ETag != `"sha256-`+coverDigest.Hex()+`"` || repository.coverID != torrentID {
		t.Fatalf("Cover() = %+v, %v", cover, err)
	}
	screenshot, err := service.Screenshot(context.Background(), torrentID, 1)
	if err != nil || !bytes.Equal(screenshot.Data, coverBytes) || screenshot.ContentType != "image/png" ||
		screenshot.ETag != `"sha256-`+coverDigest.Hex()+`"` || repository.screenshotID != torrentID || repository.screenshotPos != 1 {
		t.Fatalf("Screenshot() = %+v, %v", screenshot, err)
	}
	if _, err := service.Screenshot(context.Background(), torrentID, MaxTorrentScreenshots); !errors.Is(err, ErrTorrentReadInput) {
		t.Fatalf("Screenshot() invalid position error = %v", err)
	}

	detail, err := service.Detail(context.Background(), torrentID)
	if err != nil || detail.ID != torrentID || repository.detailID != torrentID {
		t.Fatalf("Detail() = %+v, %v", detail, err)
	}
	content, err := service.Content(context.Background(), torrentID)
	if err != nil || content.TorrentID != torrentID || repository.contentID != torrentID {
		t.Fatalf("Content() = %+v, %v", content, err)
	}
	related, err := service.RelatedVersions(context.Background(), torrentID)
	if err != nil || len(related) != 1 || repository.relatedID != torrentID || repository.relatedLimit != MaxRelatedTorrentVersions {
		t.Fatalf("RelatedVersions() = %+v, %v", related, err)
	}
	files, err := service.Files(context.Background(), torrentID, 25, 50)
	if err != nil || files.TorrentID != torrentID || repository.fileID != torrentID ||
		repository.fileLimit != 25 || repository.fileOffset != 50 {
		t.Fatalf("Files() = %+v, %v", files, err)
	}
	if _, err := service.Files(context.Background(), torrentID, MaxTorrentFileLimit+1, 0); !errors.Is(err, ErrTorrentReadInput) {
		t.Fatalf("Files() invalid limit error = %v", err)
	}
	if repository.fileLimit != 25 {
		t.Fatal("invalid file query reached repository")
	}
}

func TestTorrentReadAuthorizesOwnSubmissionHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 30, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &torrentReadRepositoryFixture{submissions: MySubmissionPage{Total: 1, Limit: 10}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentReadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}},
		authorizer,
		repository,
		mustReadStores(t),
		nil,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.MySubmissions(context.Background(), "opaque-cookie", 10)
	if err != nil || page.Total != 1 || repository.submissionOwner != userID || repository.submissionLimit != 10 {
		t.Fatalf("MySubmissions() = %+v, %v", page, err)
	}
	if authorizer.request.Action != authz.ActionTorrentSubmissionReadSelf ||
		authorizer.request.Resource.OwnerID != userID ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestTorrentReadExposesPrivacyMinimizedActivePeersToSignedInMembers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444")
	peerRepository := &torrentAdministrationRepositoryStub{
		peerTarget: ManagedTorrentPeerTarget{
			InfoHashV1: InfoHashV1{1}, TotalSizeBytes: 1_000, UploaderID: userID,
		},
		peerIdentities: []ManagedTorrentPeerIdentity{{
			UserID: userID, NumericID: 9, Username: "member", DisplayName: "站点成员",
		}},
	}
	staffAuthorizer := &torrentAdministrationAuthorizerStub{err: errors.New("member read must not enter staff authorization")}
	administration, err := NewTorrentAdministrationService(
		peerRepository,
		staffAuthorizer,
		func() time.Time { return now },
		trackerPeerReaderStub{page: trackeroperationsv1.ActivePeerPage{
			GeneratedAt: now,
			Items: []trackeroperationsv1.ActivePeer{{
				UserID: userID.String(), ClientFamily: "qbittorrent", Uploaded: 500,
				Downloaded: 100, Left: 0, LastAnnounce: now.Add(-time.Minute),
			}},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTorrentReadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}},
		&recordingTorrentUploadAuthorizer{now: now},
		&torrentReadRepositoryFixture{},
		mustReadStores(t),
		nil,
		func() time.Time { return now },
		administration,
	)
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.ActivePeers(context.Background(), "member-cookie", 42)
	if err != nil || len(page.Items) != 1 || page.Items[0].Username != "member" || page.Items[0].SeedingConnections != 1 {
		t.Fatalf("ActivePeers() = %+v, %v", page, err)
	}
	if len(staffAuthorizer.requests) != 0 {
		t.Fatalf("member peer read entered staff authorization: %+v", staffAuthorizer.requests)
	}
}

func TestTorrentReadRejectsAnonymousActivePeerLookup(t *testing.T) {
	t.Parallel()

	service, err := NewTorrentReadService(
		torrentDownloadAuthenticatorFixture{err: identity.ErrSessionNotFound},
		&recordingTorrentUploadAuthorizer{},
		&torrentReadRepositoryFixture{},
		mustReadStores(t),
		nil,
		time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ActivePeers(context.Background(), "", 42)
	if !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("ActivePeers() error = %v, want ErrSessionNotFound", err)
	}
}

func TestTorrentReadPrefersVerifiedWebPDerivative(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 1, 0, 0, 0, time.UTC)
	sourceObjectID := uuid.New()
	derivativeBytes := []byte("verified-webp-derivative")
	derivativeDigest := objectstorage.SHA256(sha256.Sum256(derivativeBytes))
	derivativeKey, err := objectstorage.ParseKey("image-derivatives/webp-v1/sha256/aa/fixture.webp")
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryObjectStore("local-primary")
	store.objects[derivativeKey] = derivativeBytes
	stores, err := NewStoreRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	repository := &torrentReadRepositoryFixture{cover: PublicCoverSource{
		TorrentID: 42, ObjectID: sourceObjectID, ContentType: "image/png",
	}}
	derivatives := &torrentDerivativeFixture{ready: imaging.ReadyDerivative{
		ObjectID:   uuid.New(),
		Descriptor: objectstorage.Descriptor{SHA256: derivativeDigest, ByteLength: int64(len(derivativeBytes))},
		Width:      320, Height: 200,
		Locations: []imaging.Location{{
			BackendID: "local-primary", ObjectKey: derivativeKey, VerifiedAt: now,
		}},
	}}
	service, err := NewTorrentReadService(
		torrentDownloadAuthenticatorFixture{}, &recordingTorrentUploadAuthorizer{now: now},
		repository, stores, derivatives, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := service.Cover(context.Background(), 42)
	if err != nil || !bytes.Equal(cover.Data, derivativeBytes) || cover.ContentType != "image/webp" ||
		cover.ETag != `"sha256-`+derivativeDigest.Hex()+`"` {
		t.Fatalf("Cover() = %+v, %v", cover, err)
	}
	if derivatives.sourceID != sourceObjectID || derivatives.variant != imaging.VariantThumbnail {
		t.Fatalf("derivative request = %s %s", derivatives.sourceID, derivatives.variant)
	}
}

func TestTorrentReadDenialDoesNotQuerySubmissionHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 13, 0, 0, 0, time.UTC)
	repository := &torrentReadRepositoryFixture{}
	service, err := NewTorrentReadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}},
		rejectingTorrentReadAuthorizer{},
		repository,
		mustReadStores(t),
		nil,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.MySubmissions(context.Background(), "opaque-cookie", DefaultMyTorrentSubmissionLimit)
	if !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("MySubmissions() error = %v", err)
	}
	if repository.submissionOwner != uuid.Nil {
		t.Fatal("denied submission query reached repository")
	}
}

func mustReadStores(t *testing.T) *StoreRegistry {
	t.Helper()
	stores, err := NewStoreRegistry(newMemoryObjectStore("local-primary"))
	if err != nil {
		t.Fatal(err)
	}
	return stores
}
