package rss

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const rawTokenBytes = 32

var validPromotionFilters = map[string]struct{}{
	"free": {}, "double_upload": {}, "double_upload_free": {},
	"half_download": {}, "double_upload_half_download": {}, "thirty_percent_download": {},
}

type Repository interface {
	Settings(context.Context) (Settings, error)
	UpdateSettings(context.Context, SettingsChangeCommand) (Settings, error)
	ListSubscriptions(context.Context, uuid.UUID) ([]Subscription, error)
	CreateSubscription(context.Context, uuid.UUID, SubscriptionInput, uuid.UUID, []byte, time.Time) (Subscription, error)
	UpdateSubscription(context.Context, uuid.UUID, UpdateSubscriptionInput, time.Time) (Subscription, error)
	RotateToken(context.Context, uuid.UUID, SubscriptionVersionInput, []byte, time.Time) (Subscription, error)
	RevokeSubscription(context.Context, uuid.UUID, SubscriptionVersionInput, time.Time) error
	ResolveByToken(context.Context, []byte, time.Time) (ResolvedSubscription, error)
	ConsumeAllowance(context.Context, uuid.UUID, time.Time, int) error
	FeedProjection(context.Context, ResolvedSubscription, time.Time) (FeedProjection, error)
}

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type TorrentDownloader interface {
	DownloadForRSS(context.Context, identity.User, torrents.TorrentID) (torrents.TorrentDownloadResult, error)
}

type ServiceConfig struct {
	PublicOrigin string
	Now          func() time.Time
	NewID        func() uuid.UUID
	ReadRandom   func([]byte) (int, error)
}

type Service struct {
	repository    Repository
	authenticator SessionAuthenticator
	authorizer    authz.Authorizer
	downloader    TorrentDownloader
	publicOrigin  url.URL
	now           func() time.Time
	newID         func() uuid.UUID
	readRandom    func([]byte) (int, error)
}

func NewService(repository Repository, authenticator SessionAuthenticator, authorizer authz.Authorizer, downloader TorrentDownloader, config ServiceConfig) (*Service, error) {
	if repository == nil || authenticator == nil || authorizer == nil || downloader == nil {
		return nil, errors.New("rss service dependencies are required")
	}
	origin, err := url.Parse(strings.TrimSpace(config.PublicOrigin))
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") || (origin.Scheme != "http" && origin.Scheme != "https") {
		return nil, errors.New("rss public origin is invalid")
	}
	origin.Path = ""
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}
	if config.ReadRandom == nil {
		config.ReadRandom = rand.Read
	}
	return &Service{repository: repository, authenticator: authenticator, authorizer: authorizer, downloader: downloader, publicOrigin: *origin, now: config.Now, newID: config.NewID, readRandom: config.ReadRandom}, nil
}

func (service *Service) List(ctx context.Context, cookieToken string) ([]Subscription, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return nil, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRSSSubscriptionReadSelf, service.now().UTC()); err != nil {
		return nil, err
	}
	return service.repository.ListSubscriptions(ctx, session.User.ID)
}

func (service *Service) Create(ctx context.Context, cookieToken, csrfToken string, input SubscriptionInput) (IssuedSubscription, error) {
	input, err := normalizeSubscriptionInput(input)
	if err != nil {
		return IssuedSubscription{}, err
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return IssuedSubscription{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRSSSubscriptionManageSelf, now); err != nil {
		return IssuedSubscription{}, err
	}
	rawToken, digest, err := service.newToken()
	if err != nil {
		return IssuedSubscription{}, err
	}
	subscription, err := service.repository.CreateSubscription(ctx, session.User.ID, input, service.newID(), digest, now)
	if err != nil {
		return IssuedSubscription{}, err
	}
	return service.issuedSubscription(subscription, rawToken), nil
}

func (service *Service) Update(ctx context.Context, cookieToken, csrfToken string, input UpdateSubscriptionInput) (Subscription, error) {
	normalized, err := normalizeSubscriptionInput(input.SubscriptionInput)
	if err != nil || input.ID == uuid.Nil || input.ExpectedVersion < 1 {
		return Subscription{}, ErrInvalidInput
	}
	input.SubscriptionInput = normalized
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return Subscription{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRSSSubscriptionManageSelf, now); err != nil {
		return Subscription{}, err
	}
	return service.repository.UpdateSubscription(ctx, session.User.ID, input, now)
}

func (service *Service) Rotate(ctx context.Context, cookieToken, csrfToken string, input SubscriptionVersionInput) (IssuedSubscription, error) {
	if input.ID == uuid.Nil || input.ExpectedVersion < 1 {
		return IssuedSubscription{}, ErrInvalidInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return IssuedSubscription{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRSSSubscriptionManageSelf, now); err != nil {
		return IssuedSubscription{}, err
	}
	rawToken, digest, err := service.newToken()
	if err != nil {
		return IssuedSubscription{}, err
	}
	subscription, err := service.repository.RotateToken(ctx, session.User.ID, input, digest, now)
	if err != nil {
		return IssuedSubscription{}, err
	}
	return service.issuedSubscription(subscription, rawToken), nil
}

func (service *Service) Revoke(ctx context.Context, cookieToken, csrfToken string, input SubscriptionVersionInput) error {
	if input.ID == uuid.Nil || input.ExpectedVersion < 1 {
		return ErrInvalidInput
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionRSSSubscriptionManageSelf, now); err != nil {
		return err
	}
	return service.repository.RevokeSubscription(ctx, session.User.ID, input, now)
}

func (service *Service) Settings(ctx context.Context, actor authz.StaffActor) (Settings, error) {
	now := service.now().UTC()
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRSSSettingsManageRead, authz.SiteScope(), now, "rss-settings"); err != nil {
		return Settings{}, err
	}
	return service.repository.Settings(ctx)
}

func (service *Service) UpdateSettings(ctx context.Context, actor authz.StaffActor, input UpdateSettingsInput) (Settings, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.CacheTTLSeconds < 60 || input.CacheTTLSeconds > 900 || input.MaxItemsPerFeed < 1 || input.MaxItemsPerFeed > 50 || input.MaxSubscriptionsPerUser < 1 || input.MaxSubscriptionsPerUser > 20 || input.RequestsPerMinute < 1 || input.RequestsPerMinute > 120 || input.ExpectedVersion < 1 || utf8.RuneCountInString(input.Reason) < 10 || utf8.RuneCountInString(input.Reason) > 500 {
		return Settings{}, ErrInvalidInput
	}
	now := service.now().UTC()
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionRSSSettingsUpdate, authz.SiteScope(), now, "rss-settings")
	if err != nil {
		return Settings{}, err
	}
	return service.repository.UpdateSettings(ctx, SettingsChangeCommand{UpdateSettingsInput: input, ID: service.newID(), ActorID: actor.Subject.ID, OccurredAt: now, Authorization: decision})
}

// Feed authenticates and rate-limits before consulting ETag state.  A client
// repeatedly sending If-None-Match therefore cannot bypass the request budget
// merely because the safe item projection remains unchanged.
func (service *Service) Feed(ctx context.Context, rawToken string) (FeedDocument, error) {
	resolved, projection, err := service.resolveProjection(ctx, rawToken)
	if err != nil {
		return FeedDocument{}, err
	}
	data, err := service.renderFeed(resolved, projection, rawToken)
	if err != nil {
		return FeedDocument{}, err
	}
	digest := sha256.Sum256(data)
	return FeedDocument{Data: data, ETag: `"` + hex.EncodeToString(digest[:]) + `"`, LastModified: projection.ObservedAt, ExpiresAt: projection.ExpiresAt, User: resolved.User}, nil
}

// AuthorizeDownload applies the same token/account/rate controls as Feed and
// requires the requested torrent to be present in this subscription's frozen
// projection.  A leaked token cannot be upgraded into an arbitrary numeric-ID
// download endpoint.
func (service *Service) AuthorizeDownload(ctx context.Context, rawToken string, torrentID int64) (identity.User, error) {
	if torrentID < 1 {
		return identity.User{}, ErrTokenInvalid
	}
	resolved, projection, err := service.resolveProjection(ctx, rawToken)
	if err != nil {
		return identity.User{}, err
	}
	for _, item := range projection.Items {
		if item.TorrentID == torrentID {
			return resolved.User, nil
		}
	}
	return identity.User{}, ErrSubscriptionNotFound
}

func (service *Service) Download(ctx context.Context, rawToken string, torrentID int64) (torrents.TorrentDownloadResult, error) {
	user, err := service.AuthorizeDownload(ctx, rawToken, torrentID)
	if err != nil {
		return torrents.TorrentDownloadResult{}, err
	}
	return service.downloader.DownloadForRSS(ctx, user, torrents.TorrentID(torrentID))
}

func (service *Service) resolveProjection(ctx context.Context, rawToken string) (ResolvedSubscription, FeedProjection, error) {
	digest, err := tokenDigest(rawToken)
	if err != nil {
		return ResolvedSubscription{}, FeedProjection{}, err
	}
	now := service.now().UTC()
	resolved, err := service.repository.ResolveByToken(ctx, digest, now)
	if err != nil {
		return ResolvedSubscription{}, FeedProjection{}, err
	}
	if err := service.repository.ConsumeAllowance(ctx, resolved.User.ID, now, resolved.RequestsPerMinute); err != nil {
		return ResolvedSubscription{}, FeedProjection{}, err
	}
	projection, err := service.repository.FeedProjection(ctx, resolved, now)
	if err != nil {
		return ResolvedSubscription{}, FeedProjection{}, err
	}
	return resolved, projection, nil
}

func (service *Service) newToken() (string, []byte, error) {
	secret := make([]byte, rawTokenBytes)
	read, err := service.readRandom(secret)
	if err != nil || read != len(secret) {
		return "", nil, errors.New("generate rss token: secure random source failed")
	}
	raw := base64.RawURLEncoding.EncodeToString(secret)
	digest := sha256.Sum256([]byte(raw))
	return raw, digest[:], nil
}

func tokenDigest(raw string) ([]byte, error) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(rawTokenBytes) {
		return nil, ErrTokenInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != rawTokenBytes {
		return nil, ErrTokenInvalid
	}
	digest := sha256.Sum256([]byte(raw))
	return digest[:], nil
}

func (service *Service) issuedSubscription(subscription Subscription, rawToken string) IssuedSubscription {
	feedURL := service.publicOrigin
	feedURL.Path = "/rss/" + rawToken
	return IssuedSubscription{Subscription: subscription, Token: rawToken, FeedURL: feedURL.String()}
}

func normalizeSubscriptionInput(input SubscriptionInput) (SubscriptionInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 80 || input.ItemLimit < 1 || input.ItemLimit > 50 || (input.PriceFilter != PriceFilterAll && input.PriceFilter != PriceFilterFree && input.PriceFilter != PriceFilterPaid) {
		return SubscriptionInput{}, ErrInvalidInput
	}
	categories, ok := normalizedUnique(input.CategoryIDs, 20, nil)
	if !ok {
		return SubscriptionInput{}, ErrInvalidInput
	}
	promotions, ok := normalizedUnique(input.PromotionFilters, 6, validPromotionFilters)
	if !ok {
		return SubscriptionInput{}, ErrInvalidInput
	}
	input.CategoryIDs = categories
	input.PromotionFilters = promotions
	return input, nil
}

func normalizedUnique(values []string, maximum int, allowed map[string]struct{}) ([]string, bool) {
	if len(values) > maximum {
		return nil, false
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 64 {
			return nil, false
		}
		if allowed != nil {
			if _, exists := allowed[value]; !exists {
				return nil, false
			}
		}
		if _, exists := seen[value]; exists {
			return nil, false
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, true
}

type xmlRSS struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Channel xmlChannel `xml:"channel"`
}

type xmlChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	LastBuild   string    `xml:"lastBuildDate"`
	AtomLink    xmlAtom   `xml:"atom:link"`
	Items       []xmlItem `xml:"item"`
}

type xmlAtom struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type xmlItem struct {
	Title       string       `xml:"title"`
	Link        string       `xml:"link"`
	Description string       `xml:"description"`
	Category    string       `xml:"category,omitempty"`
	GUID        xmlGUID      `xml:"guid"`
	Published   string       `xml:"pubDate"`
	Enclosure   xmlEnclosure `xml:"enclosure"`
}

type xmlGUID struct {
	PermaLink bool   `xml:"isPermaLink,attr"`
	Value     string `xml:",chardata"`
}

type xmlEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

func (service *Service) renderFeed(subscription ResolvedSubscription, projection FeedProjection, rawToken string) ([]byte, error) {
	feedURL := service.publicOrigin
	feedURL.Path = "/rss/" + rawToken
	homeURL := service.publicOrigin
	homeURL.Path = "/torrents"
	items := make([]xmlItem, 0, len(projection.Items))
	for _, item := range projection.Items {
		detailURL := service.publicOrigin
		detailURL.Path = "/torrents/" + strconv.FormatInt(item.TorrentID, 10)
		downloadURL := service.publicOrigin
		downloadURL.Path = "/rss/" + rawToken + "/torrents/" + strconv.FormatInt(item.TorrentID, 10) + "/download"
		descriptionParts := make([]string, 0, 5)
		if subscription.IncludeSubtitle && item.Subtitle != "" {
			descriptionParts = append(descriptionParts, item.Subtitle)
		}
		if subscription.IncludeSize {
			descriptionParts = append(descriptionParts, "大小："+humanBytes(item.SizeBytes))
		}
		if subscription.IncludePromotion && item.Promotion != "none" {
			promotion := "优惠：" + promotionLabel(item.Promotion)
			if item.PromotionEndsAt != nil {
				promotion += "（至 " + item.PromotionEndsAt.Local().Format("2006-01-02 15:04") + "）"
			}
			descriptionParts = append(descriptionParts, promotion)
		}
		descriptionParts = append(descriptionParts, fmt.Sprintf("做种 %d · 下载 %d · 完成 %d", item.Seeders, item.Leechers, item.Completed))
		category := ""
		if subscription.IncludeCategory {
			category = item.CategoryName
		}
		items = append(items, xmlItem{Title: item.Title, Link: detailURL.String(), Description: strings.Join(descriptionParts, "\n"), Category: category, GUID: xmlGUID{PermaLink: false, Value: "peergo:torrent:" + strconv.FormatInt(item.TorrentID, 10)}, Published: item.PublishedAt.Format(time.RFC1123Z), Enclosure: xmlEnclosure{URL: downloadURL.String(), Length: strconv.FormatInt(item.SizeBytes, 10), Type: "application/x-bittorrent"}})
	}
	document := xmlRSS{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Channel: xmlChannel{Title: "PeerGo · " + subscription.Name, Link: homeURL.String(), Description: "PeerGo 已发布种子订阅", Language: "zh-CN", LastBuild: projection.ObservedAt.Format(time.RFC1123Z), AtomLink: xmlAtom{Href: feedURL.String(), Rel: "self", Type: "application/rss+xml"}, Items: items}}
	encoded, err := xml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("render rss xml: %w", err)
	}
	return append([]byte(xml.Header), encoded...), nil
}

func promotionLabel(value string) string {
	labels := map[string]string{"free": "免费", "double_upload": "2× 上传", "double_upload_free": "2× 上传 / 免费", "half_download": "50% 下载", "double_upload_half_download": "2× 上传 / 50% 下载", "thirty_percent_download": "30% 下载"}
	if label := labels[value]; label != "" {
		return label
	}
	return value
}

func humanBytes(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return strconv.FormatInt(value, 10) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	amount := float64(value)
	index := -1
	for amount >= 1024 && index < len(units)-1 {
		amount /= 1024
		index++
	}
	return strconv.FormatFloat(amount, 'f', 2, 64) + " " + units[index]
}
