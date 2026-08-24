package torrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestRewriteTrackerAnnouncePreservesExactInfoAndRemovesLegacyTrackers(t *testing.T) {
	t.Parallel()

	info := validSingleInfo("payload.bin", 5, 16*1024)
	raw := testDictionary(map[string][]byte{
		"announce":      testBytes([]byte("https://old.example/tracker/old-passkey/announce")),
		"announce-list": testList(testList(testBytes([]byte("https://old-region.example/announce")))),
		"comment":       testBytes([]byte("kept exactly")),
		"info":          info,
	})
	original := append([]byte(nil), raw...)
	parsedOriginal := mustParseV1(t, raw, ValidationProfileStrictUpload)

	rewritten, err := RewriteTrackerAnnounce(
		raw,
		parsedOriginal.InfoOffset,
		parsedOriginal.InfoLength,
		[][]string{{"https://tracker.example/tracker/0123456789abcdef0123456789abcdef/announce"}},
	)
	if err != nil {
		t.Fatalf("RewriteTrackerAnnounce() error = %v", err)
	}
	if !bytes.Equal(raw, original) {
		t.Fatal("RewriteTrackerAnnounce() mutated the immutable source")
	}
	parsedRewritten := mustParseV1(t, rewritten, ValidationProfileStrictUpload)
	if parsedRewritten.InfoHashV1 != parsedOriginal.InfoHashV1 {
		t.Fatalf("info hash changed: %s != %s", parsedRewritten.InfoHashV1.Hex(), parsedOriginal.InfoHashV1.Hex())
	}
	if !bytes.Equal(
		raw[parsedOriginal.InfoOffset:parsedOriginal.InfoOffset+parsedOriginal.InfoLength],
		rewritten[parsedRewritten.InfoOffset:parsedRewritten.InfoOffset+parsedRewritten.InfoLength],
	) {
		t.Fatal("rewritten copy changed the exact info bytes")
	}
	root, _, err := decodeBencode(rewritten, ValidationProfileStrictUpload)
	if err != nil {
		t.Fatal(err)
	}
	announce, exists := root.get("announce")
	if !exists || string(announce.bytes) != "https://tracker.example/tracker/0123456789abcdef0123456789abcdef/announce" {
		t.Fatalf("announce = %q", announce.bytes)
	}
	if _, exists := root.get("announce-list"); exists {
		t.Fatal("single canonical Tracker copy retained announce-list")
	}
	comment, exists := root.get("comment")
	if !exists || string(comment.bytes) != "kept exactly" {
		t.Fatal("unrelated outer field was not preserved")
	}
}

func TestRewriteTrackerAnnounceBuildsOrderedRegionFallbackTiers(t *testing.T) {
	t.Parallel()

	raw := validSingleFixture("payload.bin", 5, 16*1024)
	parsed := mustParseV1(t, raw, ValidationProfileStrictUpload)
	rewritten, err := RewriteTrackerAnnounce(raw, parsed.InfoOffset, parsed.InfoLength, [][]string{
		{"https://tracker-hk.example/tracker/0123456789abcdef0123456789abcdef/announce"},
		{"https://tracker.example/tracker/0123456789abcdef0123456789abcdef/announce"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := decodeBencode(rewritten, ValidationProfileStrictUpload)
	announceList, exists := root.get("announce-list")
	if !exists || len(announceList.list) != 2 || len(announceList.list[0].list) != 1 || len(announceList.list[1].list) != 1 {
		t.Fatalf("announce-list = %+v", announceList)
	}
	if string(announceList.list[0].list[0].bytes) != "https://tracker-hk.example/tracker/0123456789abcdef0123456789abcdef/announce" ||
		string(announceList.list[1].list[0].bytes) != "https://tracker.example/tracker/0123456789abcdef0123456789abcdef/announce" {
		t.Fatal("region and canonical fallback tiers were not preserved in order")
	}
}

type torrentDownloadAuthenticatorFixture struct {
	session identity.WebSession
	err     error
}

func (fixture torrentDownloadAuthenticatorFixture) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return fixture.session, fixture.err
}

type torrentDownloadRepositoryFixture struct {
	source      TorrentDownloadSource
	err         error
	id          TorrentID
	restricted  bool
	restrictErr error
}

func (fixture *torrentDownloadRepositoryFixture) DownloadRestricted(context.Context, uuid.UUID) (bool, error) {
	return fixture.restricted, fixture.restrictErr
}

func (fixture *torrentDownloadRepositoryFixture) PublishedDownloadSource(_ context.Context, id TorrentID) (TorrentDownloadSource, error) {
	fixture.id = id
	return fixture.source, fixture.err
}

type trackerCredentialProviderFixture struct {
	user       identity.User
	credential identity.TrackerCredential
	err        error
}

type torrentPurchaseAccessFixture struct{ err error }

func (fixture torrentPurchaseAccessFixture) RequireDownloadAccess(context.Context, uuid.UUID, int64) error {
	return fixture.err
}

func (fixture torrentPurchaseAccessFixture) MyStatus(context.Context, string, int64) (torrentpurchase.Status, error) {
	return torrentpurchase.Status{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) MyHistory(context.Context, string, int, int) (torrentpurchase.HistoryPage, error) {
	return torrentpurchase.HistoryPage{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) Purchase(context.Context, string, string, uuid.UUID, int64) (torrentpurchase.Receipt, error) {
	return torrentpurchase.Receipt{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) PurchasePolicy(context.Context, authz.StaffActor) (torrentpurchase.PolicySettings, error) {
	return torrentpurchase.PolicySettings{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) UpdatePurchasePolicy(context.Context, authz.StaffActor, torrentpurchase.UpdatePolicyCommand) (torrentpurchase.PolicySettings, error) {
	return torrentpurchase.PolicySettings{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) UpdateTorrentPrice(context.Context, authz.StaffActor, torrentpurchase.UpdatePriceCommand) (torrentpurchase.PriceChange, error) {
	return torrentpurchase.PriceChange{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) AdminHistory(context.Context, authz.StaffActor, torrentpurchase.AdminPurchaseQuery) (torrentpurchase.AdminPurchasePage, error) {
	return torrentpurchase.AdminPurchasePage{}, fixture.err
}

func (fixture torrentPurchaseAccessFixture) RefundPurchase(context.Context, authz.StaffActor, torrentpurchase.RefundCommand) (torrentpurchase.RefundReceipt, error) {
	return torrentpurchase.RefundReceipt{}, fixture.err
}

func (fixture *trackerCredentialProviderFixture) ForUser(_ context.Context, user identity.User) (identity.TrackerCredential, error) {
	fixture.user = user
	return fixture.credential, fixture.err
}

func TestTorrentDownloadUsesVerifiedFallbackAndServerSideCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 21, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	user := identity.User{ID: uuid.New(), CredentialRef: uuid.New(), EmailVerifiedAt: &verifiedAt}
	raw := validSingleFixture("payload.bin", 5, 16*1024)
	parsed := mustParseV1(t, raw, ValidationProfileStrictUpload)
	descriptor := StoredObjectDescriptor{SHA256: ObjectSHA256(sha256.Sum256(raw)), ByteLength: int64(len(raw))}
	key := TorrentObjectKey(descriptor.SHA256)
	store := newMemoryObjectStore("s3-primary")
	store.objects[key] = append([]byte(nil), raw...)
	registry, _ := NewStoreRegistry(store)
	const torrentID TorrentID = 42
	objectID := uuid.New()
	repository := &torrentDownloadRepositoryFixture{source: TorrentDownloadSource{
		TorrentID: torrentID, Title: "Release / 2026", FilenamePrefix: "[ROUSI]", ObjectID: objectID,
		Descriptor: descriptor, InfoOffset: parsed.InfoOffset, InfoLength: parsed.InfoLength,
		Locations: []TorrentDownloadLocation{
			{ID: uuid.New(), BackendID: "local-primary", ObjectKey: key, State: StorageLocationVerified, Preferred: true, Descriptor: descriptor, VerifiedAt: verifiedAt},
			{ID: uuid.New(), BackendID: store.BackendID(), ObjectKey: key, State: StorageLocationRetiring, Descriptor: descriptor, VerifiedAt: verifiedAt},
		},
	}}
	credentials := &trackerCredentialProviderFixture{credential: identity.TrackerCredential{
		Passkey: "0123456789abcdef0123456789abcdef", Version: 1, CreatedAt: verifiedAt,
	}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentDownloadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: user}},
		authorizer,
		repository,
		torrentPurchaseAccessFixture{},
		credentials,
		registry,
		TorrentDownloadServiceConfig{CanonicalTrackerOrigin: "https://tracker.example", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Download(context.Background(), "opaque-cookie", torrentID)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if repository.id != torrentID || credentials.user.ID != user.ID || result.Filename != "[ROUSI].Release _ 2026.torrent" {
		t.Fatalf("download result filename=%q", result.Filename)
	}
	if authorizer.request.Action != authz.ActionTorrentDownload || authorizer.request.Resource.OwnerID != user.ID {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
	root, _, err := decodeBencode(result.Data, ValidationProfileStrictUpload)
	if err != nil {
		t.Fatal(err)
	}
	announce, _ := root.get("announce")
	if string(announce.bytes) != "https://tracker.example/tracker/0123456789abcdef0123456789abcdef/announce" {
		t.Fatalf("announce = %q", announce.bytes)
	}
}

func TestTorrentDownloadFilenameSupportsOperatorPrefixAndSafeFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		title  string
		want   string
	}{
		{name: "Rousi legacy prefix", prefix: "[ROUSI]", title: "电影 2026", want: "[ROUSI].电影 2026.torrent"},
		{name: "prefix disabled", title: "电影 2026", want: "电影 2026.torrent"},
		{name: "unsafe characters", prefix: `[ROU/SI]`, title: `Release: 2026`, want: "[ROU_SI].Release_ 2026.torrent"},
		{name: "empty title", prefix: "[ROUSI]", title: "...", want: "[ROUSI].PeerGo-42.torrent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := torrentDownloadFilename(test.prefix, test.title, 42); got != test.want {
				t.Fatalf("torrentDownloadFilename() = %q, want %q", got, test.want)
			}
		})
	}

	longTitle := strings.Repeat("影", maxDownloadFilenameRunes)
	got := torrentDownloadFilename("[ROUSI]", longTitle, 42)
	if utf8.RuneCountInString(strings.TrimSuffix(got, ".torrent")) != maxDownloadFilenameRunes || !strings.HasPrefix(got, "[ROUSI].") {
		t.Fatalf("bounded prefixed filename = %q", got)
	}
}

func TestTorrentDownloadRequiresPurchaseBeforeReadingStorageOrTrackerCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	user := identity.User{ID: uuid.New(), CredentialRef: uuid.New(), EmailVerifiedAt: &verifiedAt}
	repository := &torrentDownloadRepositoryFixture{}
	credentials := &trackerCredentialProviderFixture{}
	store := newMemoryObjectStore("local-primary")
	registry, err := NewStoreRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTorrentDownloadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: user}},
		&recordingTorrentUploadAuthorizer{now: now},
		repository,
		torrentPurchaseAccessFixture{err: torrentpurchase.ErrPurchaseRequired},
		credentials,
		registry,
		TorrentDownloadServiceConfig{CanonicalTrackerOrigin: "https://tracker.example", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Download(context.Background(), "opaque-cookie", 42)
	if !errors.Is(err, torrentpurchase.ErrPurchaseRequired) {
		t.Fatalf("Download() error = %v", err)
	}
	if repository.id != 0 {
		t.Fatalf("repository was read before purchase authorization: torrent_id=%d", repository.id)
	}
	if credentials.user.ID != uuid.Nil {
		t.Fatalf("Tracker credential was requested before purchase authorization: user_id=%s", credentials.user.ID)
	}
}

func TestTorrentDownloadRestrictionStopsBeforePurchaseStorageAndCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	user := identity.User{ID: uuid.New(), CredentialRef: uuid.New(), EmailVerifiedAt: &verifiedAt}
	repository := &torrentDownloadRepositoryFixture{restricted: true}
	credentials := &trackerCredentialProviderFixture{}
	registry, err := NewStoreRegistry(newMemoryObjectStore("local-primary"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTorrentDownloadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: user}},
		&recordingTorrentUploadAuthorizer{now: now},
		repository,
		torrentPurchaseAccessFixture{},
		credentials,
		registry,
		TorrentDownloadServiceConfig{CanonicalTrackerOrigin: "https://tracker.example", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Download(context.Background(), "opaque-cookie", 42)
	if !errors.Is(err, ErrTorrentDownloadRestricted) {
		t.Fatalf("Download() error = %v", err)
	}
	if repository.id != 0 || credentials.user.ID != uuid.Nil {
		t.Fatalf("restricted request crossed a later boundary: torrent_id=%d credential_user=%s", repository.id, credentials.user.ID)
	}
}

func TestTorrentDownloadFailsClosedOnPreferredContentConflict(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	verifiedAt := now.Add(-time.Hour)
	raw := validSingleFixture("payload.bin", 5, 16*1024)
	parsed := mustParseV1(t, raw, ValidationProfileStrictUpload)
	descriptor := StoredObjectDescriptor{SHA256: ObjectSHA256(sha256.Sum256(raw)), ByteLength: int64(len(raw))}
	key := TorrentObjectKey(descriptor.SHA256)
	corrupt := newMemoryObjectStore("s3-corrupt")
	corrupt.objects[key] = append([]byte(nil), raw...)
	corrupt.corruptReadback = true
	healthy := newMemoryObjectStore("local-healthy")
	healthy.objects[key] = append([]byte(nil), raw...)
	registry, _ := NewStoreRegistry(corrupt, healthy)
	user := identity.User{ID: uuid.New(), CredentialRef: uuid.New(), EmailVerifiedAt: &verifiedAt}
	repository := &torrentDownloadRepositoryFixture{source: TorrentDownloadSource{
		TorrentID: 42, Title: "Release", ObjectID: uuid.New(), Descriptor: descriptor,
		InfoOffset: parsed.InfoOffset, InfoLength: parsed.InfoLength,
		Locations: []TorrentDownloadLocation{
			{ID: uuid.New(), BackendID: corrupt.BackendID(), ObjectKey: key, State: StorageLocationVerified, Preferred: true, Descriptor: descriptor, VerifiedAt: verifiedAt},
			{ID: uuid.New(), BackendID: healthy.BackendID(), ObjectKey: key, State: StorageLocationVerified, Descriptor: descriptor, VerifiedAt: verifiedAt},
		},
	}}
	service, _ := NewTorrentDownloadService(
		torrentDownloadAuthenticatorFixture{session: identity.WebSession{User: user}},
		&recordingTorrentUploadAuthorizer{now: now}, repository,
		torrentPurchaseAccessFixture{},
		&trackerCredentialProviderFixture{}, registry,
		TorrentDownloadServiceConfig{CanonicalTrackerOrigin: "https://tracker.example", Now: func() time.Time { return now }},
	)
	_, err := service.Download(context.Background(), "cookie", repository.source.TorrentID)
	if !errors.Is(err, ErrTorrentDownloadObjectConflict) {
		t.Fatalf("Download() error = %v", err)
	}
}
