package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type recordingSiteInfoRepository struct {
	Repository
	result SiteInfo
	asOf   time.Time
}

func (repository *recordingSiteInfoRepository) SiteInfo(_ context.Context, asOf time.Time) (SiteInfo, error) {
	repository.asOf = asOf
	return repository.result, nil
}

func TestGetSiteInfoUsesServiceClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	want := SiteInfo{Name: "PeerGo", OnlineUsers: 7, CustomNavigationItems: []CustomNavigationItem{}}
	repository := &recordingSiteInfoRepository{result: want}
	service := NewService(repository, func() time.Time { return now })

	got, err := service.GetSiteInfo(context.Background())
	if err != nil || !reflect.DeepEqual(got, want) || !repository.asOf.Equal(now) {
		t.Fatalf("GetSiteInfo() = %+v, error=%v, as_of=%s", got, err, repository.asOf)
	}
}

func TestGetSiteInfoOnlyPublishesEnabledCustomNavigationItems(t *testing.T) {
	t.Parallel()

	wiki := CustomNavigationItem{Label: "Wiki", URL: "https://wiki.example.com", OpenInNewTab: true, Enabled: true}
	repository := &recordingSiteInfoRepository{result: SiteInfo{
		Name: "PeerGo",
		CustomNavigationItems: []CustomNavigationItem{
			wiki,
			{Label: "Hidden", URL: "/hidden", Enabled: false},
		},
	}}
	service := NewService(repository, time.Now)

	got, err := service.GetSiteInfo(context.Background())
	if err != nil || !reflect.DeepEqual(got.CustomNavigationItems, []CustomNavigationItem{wiki}) {
		t.Fatalf("GetSiteInfo() custom navigation = %+v, error=%v", got.CustomNavigationItems, err)
	}
}

func TestListTorrentsFiltersAndMarksStaleStats(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := NewService(NewDemoRepository(now), func() time.Time { return now })

	page, err := service.ListTorrents(context.Background(), TorrentListRequest{Query: "paper cranes"})
	if err != nil {
		t.Fatalf("ListTorrents() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("ListTorrents() got total=%d items=%d, want 1", page.Total, len(page.Items))
	}
	if !page.Items[0].SwarmStale {
		t.Fatal("ListTorrents() SwarmStale = false, want true")
	}
}

func TestListTorrentsAcceptsMaximumAndRejectsOutOfRangeLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := NewService(NewDemoRepository(now), func() time.Time { return now })
	acceptedLimit := 100
	page, err := service.ListTorrents(context.Background(), TorrentListRequest{Limit: &acceptedLimit})
	if err != nil || page.Limit != acceptedLimit {
		t.Fatalf("ListTorrents(limit=100) = %+v, error=%v", page, err)
	}

	limit := 101

	_, err = service.ListTorrents(context.Background(), TorrentListRequest{Limit: &limit})
	if !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("ListTorrents() error = %v, want ErrInvalidLimit", err)
	}
}

func TestListTorrentsPaginatesAndFiltersCategoryAndPromotion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository(MemoryData{Torrents: []Torrent{
		{ID: 1, Name: "Movie", Category: Category{ID: "movies", Name: "电影"}, Promotion: PromotionFree},
		{ID: 2, Name: "Movie two", Category: Category{ID: "movies", Name: "电影"}, Promotion: PromotionNone},
		{ID: 3, Name: "Series", Category: Category{ID: "tv", Name: "剧集"}, Promotion: PromotionFree},
	}})
	service := NewService(repository, func() time.Time { return now })
	limit, offset, promotion := 1, 0, PromotionFree
	page, err := service.ListTorrents(context.Background(), TorrentListRequest{
		Limit: &limit, Offset: &offset, CategoryID: "movies", Promotion: &promotion,
	})
	if err != nil || page.Total != 1 || page.Limit != 1 || page.Offset != 0 || len(page.Items) != 1 || page.Items[0].ID != 1 {
		t.Fatalf("filtered page = %+v, error = %v", page, err)
	}

	invalidOffset := maxTorrentOffset + 1
	if _, err := service.ListTorrents(context.Background(), TorrentListRequest{Offset: &invalidOffset}); !errors.Is(err, ErrInvalidTorrentPage) {
		t.Fatalf("invalid offset error = %v", err)
	}
	invalidPromotion := Promotion("half")
	if _, err := service.ListTorrents(context.Background(), TorrentListRequest{Promotion: &invalidPromotion}); !errors.Is(err, ErrInvalidTorrentFilter) {
		t.Fatalf("invalid promotion error = %v", err)
	}
}

func TestListTorrentsSearchScopeAndSort(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository(MemoryData{Torrents: []Torrent{
		{ID: 1, Name: "Orbit", Subtitle: "small", SizeBytes: 1, UploadedAt: now.Add(-time.Hour)},
		{ID: 2, Name: "Other", Subtitle: "Orbit large", SizeBytes: 3, UploadedAt: now.Add(-2 * time.Hour)},
	}})
	service := NewService(repository, func() time.Time { return now })
	scope, order := TorrentSearchSubtitle, TorrentSortSizeDesc
	page, err := service.ListTorrents(context.Background(), TorrentListRequest{
		Query: "orbit", SearchScope: &scope, Sort: &order,
	})
	if err != nil || page.Total != 1 || page.Items[0].ID != 2 {
		t.Fatalf("scoped page = %+v, error = %v", page, err)
	}

	invalidScope := TorrentSearchScope("description")
	if _, err := service.ListTorrents(context.Background(), TorrentListRequest{SearchScope: &invalidScope}); !errors.Is(err, ErrInvalidTorrentFilter) {
		t.Fatalf("invalid search scope error = %v", err)
	}
	invalidSort := TorrentSort("random")
	if _, err := service.ListTorrents(context.Background(), TorrentListRequest{Sort: &invalidSort}); !errors.Is(err, ErrInvalidTorrentFilter) {
		t.Fatalf("invalid sort error = %v", err)
	}
}

func TestGetTorrentSwarmDistinguishesFreshStaleAndUnavailableSnapshots(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository(MemoryData{Torrents: []Torrent{
		{ID: 1, Swarm: SwarmStats{Seeders: 4, Leechers: 2, Completed: 8, ObservedAt: now.Add(-time.Minute)}},
		{ID: 2, Swarm: SwarmStats{Seeders: 3, Leechers: 1, Completed: 7, ObservedAt: now.Add(-10 * time.Minute)}},
		{ID: 3},
	}})
	service := NewService(repository, func() time.Time { return now })

	fresh, err := service.GetTorrentSwarm(context.Background(), 1)
	if err != nil || fresh.Stale || fresh.Confidence != SwarmConfidenceFresh || fresh.Seeders != 4 {
		t.Fatalf("fresh swarm = %+v, error=%v", fresh, err)
	}
	stale, err := service.GetTorrentSwarm(context.Background(), 2)
	if err != nil || !stale.Stale || stale.Confidence != SwarmConfidenceStale || stale.Completed != 7 {
		t.Fatalf("stale swarm = %+v, error=%v", stale, err)
	}
	unavailable, err := service.GetTorrentSwarm(context.Background(), 3)
	if err != nil || !unavailable.Stale || unavailable.Confidence != SwarmConfidenceUnavailable || !unavailable.ObservedAt.IsZero() {
		t.Fatalf("unavailable swarm = %+v, error=%v", unavailable, err)
	}
	if _, err := service.GetTorrentSwarm(context.Background(), 4); !errors.Is(err, ErrTorrentNotFound) {
		t.Fatalf("missing swarm error = %v, want ErrTorrentNotFound", err)
	}
}

func TestGetAnnouncementUsesTheStableRouteKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	announcement := Announcement{
		AnnouncementSummary: AnnouncementSummary{
			ID: "welcome-to-peergo", Title: "欢迎", Summary: "公开摘要", PublishedAt: now.Add(-time.Minute),
		},
		Body:       "公开正文",
		BodyFormat: AnnouncementBodyPlainText, Version: 1,
		UpdatedAt: now,
	}
	repository := NewMemoryRepository(MemoryData{Announcements: []Announcement{announcement}})
	service := NewService(repository, func() time.Time { return now })

	got, err := service.GetAnnouncement(context.Background(), announcement.ID)
	if err != nil || got != announcement {
		t.Fatalf("GetAnnouncement() = %+v, %v", got, err)
	}
	if _, err := service.GetAnnouncement(context.Background(), " "+announcement.ID); !errors.Is(err, ErrAnnouncementNotFound) {
		t.Fatalf("GetAnnouncement(whitespace ID) error = %v", err)
	}

}

func TestListAnnouncementsUsesBoundedNewestFirstPaging(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository(MemoryData{Announcements: []Announcement{
		{AnnouncementSummary: AnnouncementSummary{ID: "older", Title: "较早公告", Summary: "较早摘要", PublishedAt: now.Add(-2 * time.Hour)}},
		{AnnouncementSummary: AnnouncementSummary{ID: "newer", Title: "最新公告", Summary: "最新摘要", PublishedAt: now.Add(-time.Hour)}},
	}})
	service := NewService(repository, func() time.Time { return now })

	first, err := service.ListAnnouncements(context.Background(), 1, 0)
	if err != nil || first.Total != 2 || first.Limit != 1 || len(first.Items) != 1 || first.Items[0].ID != "newer" {
		t.Fatalf("first announcement page = %+v, error = %v", first, err)
	}
	second, err := service.ListAnnouncements(context.Background(), 1, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != "older" {
		t.Fatalf("second announcement page = %+v, error = %v", second, err)
	}
	if _, err := service.ListAnnouncements(context.Background(), 51, 0); !errors.Is(err, ErrInvalidAnnouncementPage) {
		t.Fatalf("oversized announcement page error = %v", err)
	}
	if _, err := service.ListAnnouncements(context.Background(), 20, maxAnnouncementOffset+1); !errors.Is(err, ErrInvalidAnnouncementPage) {
		t.Fatalf("announcement offset error = %v", err)
	}
}

func TestMemoryRepositoryDoesNotExposeMutableFixtureSlices(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	repository := NewDemoRepository(now)

	first, err := repository.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	first[0].Name = "mutated"

	second, err := repository.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("ListCategories() second error = %v", err)
	}
	if second[0].Name == "mutated" {
		t.Fatal("ListCategories() exposed repository-owned storage")
	}
}

func TestListCategoryFacetsReturnsControlledCopy(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository(MemoryData{
		Categories: []Category{{ID: "movies", Name: "电影"}},
		CategoryFacets: map[string][]CategoryFacet{
			"movies": {{
				ID: "resolution", Name: "分辨率", SelectionMode: FacetSelectionSingle, Required: true,
				Options: []CategoryFacetOption{{Key: "1080p", Label: "1080p"}},
			}},
		},
	})
	service := NewService(repository, time.Now)
	first, err := service.ListCategoryFacets(context.Background(), "movies")
	if err != nil || len(first) != 1 || !first[0].Required || first[0].Options[0].Key != "1080p" {
		t.Fatalf("ListCategoryFacets() = %+v, %v", first, err)
	}
	first[0].Options[0].Label = "mutated"
	second, err := service.ListCategoryFacets(context.Background(), "movies")
	if err != nil || second[0].Options[0].Label != "1080p" {
		t.Fatalf("second ListCategoryFacets() = %+v, %v", second, err)
	}
	if _, err := service.ListCategoryFacets(context.Background(), "unknown"); !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("unknown category error = %v", err)
	}
}
