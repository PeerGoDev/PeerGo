package moviepilot

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var (
	legacyCategoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	markdownImagePattern  = regexp.MustCompile(`(?i)!\s*\[[^]]*\]\s*\(|<\s*img\b`)
)

type LegacyCatalogService interface {
	ListCategories(context.Context) ([]catalog.CategorySummary, error)
	ListCategoryFacets(context.Context, string) ([]catalog.CategoryFacet, error)
}

type LegacyTorrentReadService interface {
	Detail(context.Context, torrents.TorrentID) (torrents.PublicDetail, error)
	Content(context.Context, torrents.TorrentID) (torrents.PublicContent, error)
	Files(context.Context, torrents.TorrentID, int, int) (torrents.PublicFilePage, error)
	RelatedVersions(context.Context, torrents.TorrentID) ([]catalog.TorrentSummary, error)
}

type LegacyBookmarkService interface {
	ListForIntegration(context.Context, identity.User, int, int) (catalog.TorrentBookmarkPage, error)
}

type LegacyCommentService interface {
	ListTorrentComments(context.Context, int64, int, int) (social.CommentPage, error)
}

type LegacyUploadService interface {
	SubmitForIntegration(context.Context, identity.User, torrents.TorrentUploadInput) (torrents.TorrentUploadResult, error)
}

type LegacyPurchaseService interface {
	StatusForIntegration(context.Context, identity.User, int64) (torrentpurchase.Status, error)
	PurchaseForIntegration(context.Context, identity.User, uuid.UUID, int64, *int64) (torrentpurchase.Receipt, error)
	HistoryForIntegration(context.Context, identity.User, int, int) (torrentpurchase.HistoryPage, error)
}

// LegacyServices are optional at construction so the narrow MoviePilot unit
// fixtures remain useful. Production supplies the complete canonical set and
// every legacy method fails closed if its required dependency is absent.
type LegacyServices struct {
	Catalog   LegacyCatalogService
	Torrents  LegacyTorrentReadService
	Bookmarks LegacyBookmarkService
	Comments  LegacyCommentService
	Uploads   LegacyUploadService
	Purchases LegacyPurchaseService
}

type LegacyRepository interface {
	PublicProfile(context.Context, string, time.Time) (Profile, error)
	ResolveTorrentID(context.Context, string) (int64, error)
	TorrentMetadata(context.Context, int64) (TorrentMetadata, error)
	TorrentMetadataBatch(context.Context, []int64) (map[int64]TorrentMetadata, error)
	UserNumericIDs(context.Context, []uuid.UUID) (map[uuid.UUID]int64, error)
}

func (service *Service) PublicProfile(ctx context.Context, credential personalapikey.AuthenticatedCredential, username string) (Profile, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeProfileRead); err != nil {
		return Profile{}, err
	}
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 || !service.allow(credential.User.ID, "public-profile", 60) {
		if username == "" || len(username) > 64 {
			return Profile{}, ErrInput
		}
		return Profile{}, ErrRateLimited
	}
	repository, ok := service.repository.(LegacyRepository)
	if !ok {
		return Profile{}, ErrUnavailable
	}
	return repository.PublicProfile(ctx, username, service.now().UTC())
}

func (service *Service) Categories(ctx context.Context, credential personalapikey.AuthenticatedCredential) ([]LegacyCategory, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentRead); err != nil {
		return nil, err
	}
	if !service.allow(credential.User.ID, "categories", 30) {
		return nil, ErrRateLimited
	}
	if service.legacy.Catalog == nil {
		return nil, ErrUnavailable
	}
	categories, err := service.legacy.Catalog.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]LegacyCategory, 0, len(categories))
	for index, category := range categories {
		facets, err := service.legacy.Catalog.ListCategoryFacets(ctx, category.ID)
		if err != nil {
			return nil, err
		}
		attributes := make([]LegacyCategoryAttribute, 0, len(facets))
		for _, facet := range facets {
			options := make([]LegacyCategoryOption, 0, len(facet.Options))
			for _, option := range facet.Options {
				options = append(options, LegacyCategoryOption{Value: option.Key, Label: option.Label})
			}
			attributeType := "select"
			if facet.SelectionMode == catalog.FacetSelectionMulti {
				attributeType = "multi-select"
			}
			attributes = append(attributes, LegacyCategoryAttribute{
				Name: legacyFacetName(facet.ID), Label: facet.Name, Type: attributeType,
				Required: facet.Required && facet.RequirementGroup == "", Options: options,
			})
		}
		result = append(result, LegacyCategory{
			ID: int64(index + 1), Name: moviePilotCategory(category.ID), Label: category.Name,
			Icon: legacyCategoryIcon(category.ID), Attributes: attributes,
		})
	}
	return result, nil
}

func (service *Service) Upload(ctx context.Context, credential personalapikey.AuthenticatedCredential, input LegacyUploadInput) (LegacyUploadResult, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentUpload); err != nil {
		return LegacyUploadResult{}, err
	}
	if !service.allow(credential.User.ID, "torrent-upload", 10) {
		return LegacyUploadResult{}, ErrRateLimited
	}
	if service.legacy.Catalog == nil || service.legacy.Uploads == nil {
		return LegacyUploadResult{}, ErrUnavailable
	}
	if input.RequestID == uuid.Nil || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Description) == "" ||
		len(input.RawMetainfo) == 0 || input.PurchasePrice < 0 || input.PurchasePrice > 1_000_000 ||
		markdownImagePattern.MatchString(input.Description) {
		return LegacyUploadResult{}, ErrInput
	}
	categoryID, facets, err := service.resolveUploadVocabulary(ctx, input.Category)
	if err != nil {
		return LegacyUploadResult{}, err
	}
	selections, err := legacyFacetSelections(facets, input.Attributes)
	if err != nil {
		return LegacyUploadResult{}, err
	}
	result, err := service.legacy.Uploads.SubmitForIntegration(ctx, credential.User, torrents.TorrentUploadInput{
		ID: input.RequestID, CategoryID: categoryID,
		Title: input.Title, Subtitle: input.Subtitle, Description: input.Description,
		MediaInfo: input.MediaInfo, Anonymous: input.Anonymous, PurchasePrice: input.PurchasePrice,
		ExternalIdentifiers: input.ExternalIdentifiers, FacetSelections: selections,
		Screenshots: input.Screenshots, RawMetainfo: input.RawMetainfo,
	})
	if err != nil {
		return LegacyUploadResult{}, err
	}
	status := "pending"
	if result.State == torrents.StatePublished {
		status = "approved"
	}
	return LegacyUploadResult{
		RouteID: strconv.FormatInt(int64(result.ID), 10), InfoHash: result.InfoHashV1.Hex(),
		Status: status, ID: int64(result.ID),
	}, nil
}

func (service *Service) LegacyTorrent(ctx context.Context, credential personalapikey.AuthenticatedCredential, routeID string) (LegacyTorrentDetail, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentRead); err != nil {
		return LegacyTorrentDetail{}, err
	}
	if !service.allow(credential.User.ID, "legacy-torrent-detail", 60) {
		return LegacyTorrentDetail{}, ErrRateLimited
	}
	repository, ok := service.repository.(LegacyRepository)
	if !ok || service.legacy.Torrents == nil || service.legacy.Purchases == nil {
		return LegacyTorrentDetail{}, ErrUnavailable
	}
	torrentID, err := repository.ResolveTorrentID(ctx, routeID)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	id := torrents.TorrentID(torrentID)
	detail, err := service.legacy.Torrents.Detail(ctx, id)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	content, err := service.legacy.Torrents.Content(ctx, id)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	swarm, err := service.catalog.GetTorrentSwarm(ctx, torrentID)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	metadata, err := repository.TorrentMetadata(ctx, torrentID)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	purchase, err := service.legacy.Purchases.StatusForIntegration(ctx, credential.User, torrentID)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	related, err := service.legacy.Torrents.RelatedVersions(ctx, id)
	if err != nil {
		return LegacyTorrentDetail{}, err
	}
	canReadObject := purchase.State == torrentpurchase.AccessFree || purchase.State == torrentpurchase.AccessUploader || purchase.State == torrentpurchase.AccessPurchased
	files := []torrents.PublicFile{}
	if canReadObject {
		page, err := service.legacy.Torrents.Files(ctx, id, torrents.MaxTorrentFileLimit, 0)
		if err != nil {
			return LegacyTorrentDetail{}, err
		}
		files = page.Items
	}
	downloadURL := ""
	if canReadObject && personalapikey.RequireScope(credential, personalapikey.ScopeTorrentDownload) == nil {
		capability, err := service.issueDownloadCapability(credential.User.ID, torrentID, credential.Credential.Version, service.now().UTC())
		if err != nil {
			return LegacyTorrentDetail{}, err
		}
		resolved := service.publicOrigin
		resolved.Path = "/api/compat/moviepilot/v1/torrents/" + strconv.FormatInt(torrentID, 10) + "/download"
		query := resolved.Query()
		query.Set("capability", capability)
		resolved.RawQuery = query.Encode()
		downloadURL = resolved.String()
	}
	imageURLs := make([]string, 0, detail.ScreenshotCount)
	for position := 0; position < detail.ScreenshotCount; position++ {
		resolved := service.publicOrigin
		resolved.Path = "/api/v1/torrents/" + strconv.FormatInt(torrentID, 10) + "/screenshots/" + strconv.Itoa(position)
		imageURLs = append(imageURLs, resolved.String())
	}
	return LegacyTorrentDetail{
		RouteID: metadata.LegacyRouteID, Detail: detail, Content: content,
		Swarm: swarm, Metadata: metadata, Files: files, Related: related,
		Attributes: legacyPublicAttributes(detail.Facets), ImageURLs: imageURLs,
		DownloadURL: downloadURL, Purchase: purchase, CanReadObject: canReadObject,
		Promotion: promotion(detail.Promotion, detail.PromotionEndsAt),
	}, nil
}

func (service *Service) Comments(ctx context.Context, credential personalapikey.AuthenticatedCredential, routeID string, page, pageSize int) (LegacyCommentPage, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentRead); err != nil {
		return LegacyCommentPage{}, err
	}
	if page < 1 || pageSize < 1 || pageSize > 100 || page > 1_000 || (page-1) > social.MaxCommentOffset/pageSize {
		return LegacyCommentPage{}, ErrInput
	}
	if !service.allow(credential.User.ID, "torrent-comments", 120) {
		return LegacyCommentPage{}, ErrRateLimited
	}
	repository, ok := service.repository.(LegacyRepository)
	if !ok || service.legacy.Comments == nil {
		return LegacyCommentPage{}, ErrUnavailable
	}
	torrentID, err := repository.ResolveTorrentID(ctx, routeID)
	if err != nil {
		return LegacyCommentPage{}, err
	}
	offset := (page - 1) * pageSize
	remaining := pageSize
	comments := make([]social.Comment, 0, pageSize)
	var total int64
	for remaining > 0 {
		limit := remaining
		if limit > social.MaxCommentLimit {
			limit = social.MaxCommentLimit
		}
		part, err := service.legacy.Comments.ListTorrentComments(ctx, torrentID, limit, offset+len(comments))
		if err != nil {
			return LegacyCommentPage{}, err
		}
		total = part.Total
		comments = append(comments, part.Items...)
		remaining -= len(part.Items)
		if len(part.Items) < limit {
			break
		}
	}
	authorIDs := make([]uuid.UUID, 0, len(comments))
	for _, comment := range comments {
		authorIDs = append(authorIDs, comment.Author.ID)
	}
	numericIDs, err := repository.UserNumericIDs(ctx, authorIDs)
	if err != nil {
		return LegacyCommentPage{}, err
	}
	items := make([]LegacyComment, 0, len(comments))
	for _, comment := range comments {
		items = append(items, LegacyComment{
			ID: comment.ID, Body: comment.Body, UserID: numericIDs[comment.Author.ID],
			Username: comment.Author.Username, CreatedAt: comment.CreatedAt.UTC(),
		})
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return LegacyCommentPage{Items: items, Total: total, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

func (service *Service) Bookmarks(ctx context.Context, credential personalapikey.AuthenticatedCredential, page, pageSize int) (LegacyBookmarkPage, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopeTorrentRead); err != nil {
		return LegacyBookmarkPage{}, err
	}
	if page < 1 || pageSize < 1 || pageSize > 100 || page > 1_000_001 || (page-1) > 1_000_000/pageSize {
		return LegacyBookmarkPage{}, ErrInput
	}
	if !service.allow(credential.User.ID, "bookmarks", 60) {
		return LegacyBookmarkPage{}, ErrRateLimited
	}
	repository, ok := service.repository.(LegacyRepository)
	if !ok || service.legacy.Bookmarks == nil {
		return LegacyBookmarkPage{}, ErrUnavailable
	}
	offset := (page - 1) * pageSize
	bookmarks, err := service.legacy.Bookmarks.ListForIntegration(ctx, credential.User, pageSize, offset)
	if err != nil {
		return LegacyBookmarkPage{}, err
	}
	ids := make([]int64, 0, len(bookmarks.Items))
	for _, item := range bookmarks.Items {
		ids = append(ids, item.Torrent.ID)
	}
	metadata, err := repository.TorrentMetadataBatch(ctx, ids)
	if err != nil {
		return LegacyBookmarkPage{}, err
	}
	items := make([]TorrentSummary, 0, len(bookmarks.Items))
	for _, item := range bookmarks.Items {
		items = append(items, legacyTorrentSummary(item.Torrent, metadata[item.Torrent.ID]))
	}
	totalPages := 0
	if bookmarks.Total > 0 {
		totalPages = (bookmarks.Total + pageSize - 1) / pageSize
	}
	return LegacyBookmarkPage{Items: items, Total: bookmarks.Total, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

func (service *Service) PurchaseStatus(ctx context.Context, credential personalapikey.AuthenticatedCredential, routeID string) (torrentpurchase.Status, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopePurchaseRead); err != nil {
		return torrentpurchase.Status{}, err
	}
	if !service.allow(credential.User.ID, "purchase-status", 60) {
		return torrentpurchase.Status{}, ErrRateLimited
	}
	repository, ok := service.repository.(LegacyRepository)
	if !ok || service.legacy.Purchases == nil {
		return torrentpurchase.Status{}, ErrUnavailable
	}
	torrentID, err := repository.ResolveTorrentID(ctx, routeID)
	if err != nil {
		return torrentpurchase.Status{}, err
	}
	return service.legacy.Purchases.StatusForIntegration(ctx, credential.User, torrentID)
}

func (service *Service) Purchase(ctx context.Context, credential personalapikey.AuthenticatedCredential, routeID string, requestID uuid.UUID, expectedPrice *int64) (torrentpurchase.Receipt, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopePurchaseWrite); err != nil {
		return torrentpurchase.Receipt{}, err
	}
	if requestID == uuid.Nil || !service.allow(credential.User.ID, "purchase", 10) {
		if requestID == uuid.Nil {
			return torrentpurchase.Receipt{}, ErrInput
		}
		return torrentpurchase.Receipt{}, ErrRateLimited
	}
	repository, ok := service.repository.(LegacyRepository)
	if !ok || service.legacy.Purchases == nil {
		return torrentpurchase.Receipt{}, ErrUnavailable
	}
	torrentID, err := repository.ResolveTorrentID(ctx, routeID)
	if err != nil {
		return torrentpurchase.Receipt{}, err
	}
	return service.legacy.Purchases.PurchaseForIntegration(ctx, credential.User, requestID, torrentID, expectedPrice)
}

func (service *Service) Purchases(ctx context.Context, credential personalapikey.AuthenticatedCredential, page, pageSize int) (torrentpurchase.HistoryPage, error) {
	if err := personalapikey.RequireScope(credential, personalapikey.ScopePurchaseRead); err != nil {
		return torrentpurchase.HistoryPage{}, err
	}
	if page < 1 || pageSize < 1 || pageSize > torrentpurchase.MaxHistoryLimit || page > 1_000_001 || (page-1) > torrentpurchase.MaxHistoryOffset/pageSize {
		return torrentpurchase.HistoryPage{}, ErrInput
	}
	if !service.allow(credential.User.ID, "purchase-history", 30) {
		return torrentpurchase.HistoryPage{}, ErrRateLimited
	}
	if service.legacy.Purchases == nil {
		return torrentpurchase.HistoryPage{}, ErrUnavailable
	}
	return service.legacy.Purchases.HistoryForIntegration(ctx, credential.User, pageSize, (page-1)*pageSize)
}

func (service *Service) resolveUploadVocabulary(ctx context.Context, rawCategory string) (string, []catalog.CategoryFacet, error) {
	categories, err := service.legacy.Catalog.ListCategories(ctx)
	if err != nil {
		return "", nil, err
	}
	rawCategory = strings.ToLower(strings.TrimSpace(rawCategory))
	categoryID := ""
	for _, category := range categories {
		if rawCategory == strings.ToLower(category.ID) || rawCategory == moviePilotCategory(category.ID) || (rawCategory == "" && category.ID == "other") {
			categoryID = category.ID
			break
		}
	}
	if categoryID == "" {
		return "", nil, ErrInput
	}
	facets, err := service.legacy.Catalog.ListCategoryFacets(ctx, categoryID)
	return categoryID, facets, err
}

func legacyFacetSelections(facets []catalog.CategoryFacet, attributes map[string][]string) ([]torrents.FacetSelection, error) {
	if len(attributes) > 20 {
		return nil, ErrInput
	}
	byName := make(map[string]catalog.CategoryFacet, len(facets)*3)
	for _, facet := range facets {
		byName[strings.ToLower(facet.ID)] = facet
		byName[strings.ToLower(legacyFacetName(facet.ID))] = facet
		byName[strings.ToLower(strings.TrimSpace(facet.Name))] = facet
	}
	selections := make([]torrents.FacetSelection, 0, len(attributes))
	seenFacets := make(map[string]struct{}, len(attributes))
	for rawName, rawValues := range attributes {
		facet, exists := byName[strings.ToLower(strings.TrimSpace(rawName))]
		if !exists || len(rawValues) == 0 || (facet.SelectionMode == catalog.FacetSelectionSingle && len(rawValues) != 1) {
			return nil, ErrInput
		}
		if _, duplicate := seenFacets[facet.ID]; duplicate {
			return nil, ErrInput
		}
		seenFacets[facet.ID] = struct{}{}
		optionByValue := make(map[string]string, len(facet.Options)*2)
		for _, option := range facet.Options {
			optionByValue[strings.ToLower(strings.TrimSpace(option.Key))] = option.Key
			optionByValue[strings.ToLower(strings.TrimSpace(option.Label))] = option.Key
		}
		keys := make([]string, 0, len(rawValues))
		seenOptions := make(map[string]struct{}, len(rawValues))
		for _, rawValue := range rawValues {
			key, exists := optionByValue[strings.ToLower(strings.TrimSpace(rawValue))]
			if !exists {
				return nil, ErrInput
			}
			if _, duplicate := seenOptions[key]; !duplicate {
				seenOptions[key] = struct{}{}
				keys = append(keys, key)
			}
		}
		if len(keys) == 0 {
			return nil, ErrInput
		}
		selections = append(selections, torrents.FacetSelection{FacetID: facet.ID, OptionKeys: keys})
	}
	sort.Slice(selections, func(left, right int) bool { return selections[left].FacetID < selections[right].FacetID })
	return selections, nil
}

func legacyPublicAttributes(facets []torrents.PublicFacet) map[string]any {
	grouped := make(map[string][]string)
	for _, facet := range facets {
		name := legacyFacetName(facet.FacetID)
		grouped[name] = append(grouped[name], facet.OptionKey)
	}
	result := make(map[string]any, len(grouped))
	for name, values := range grouped {
		if len(values) == 1 {
			result[name] = values[0]
		} else {
			result[name] = values
		}
	}
	return result
}

func legacyTorrentSummary(item catalog.TorrentSummary, metadata TorrentMetadata) TorrentSummary {
	routeID := metadata.LegacyRouteID
	if routeID == "" {
		routeID = strconv.FormatInt(item.ID, 10)
	}
	return TorrentSummary{
		ID: item.ID, LegacyRouteID: routeID, Title: item.Name, Subtitle: item.Subtitle,
		Category: moviePilotCategory(item.Category.ID), CategoryName: item.Category.Name,
		Size: item.SizeBytes, Seeders: item.Swarm.Seeders, Leechers: item.Swarm.Leechers,
		Downloads: item.Swarm.Completed, Uploader: metadata.Uploader,
		UploaderID: metadata.UploaderID, Anonymous: metadata.Anonymous,
		CreatedAt: item.UploadedAt.UTC(), Promotion: promotion(item.Promotion, nil),
	}
}

func legacyFacetName(value string) string {
	if value == "source-medium" {
		return "source"
	}
	return value
}

func legacyCategoryIcon(value string) string {
	switch value {
	case "movies":
		return "film"
	case "tv", "documentary", "variety":
		return "tv"
	case "anime":
		return "clapperboard"
	case "music":
		return "music"
	case "games":
		return "gamepad-2"
	case "software":
		return "package"
	case "ebooks":
		return "book-open"
	default:
		return "folder"
	}
}
