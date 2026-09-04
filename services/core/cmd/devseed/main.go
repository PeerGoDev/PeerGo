// Command devseed creates real, synthetic torrent and announcement discussion
// fixtures through Core's normal domain services. It is intentionally
// unavailable outside development and never creates a second login credential.
package main

import (
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- SHA-1 is required by the BitTorrent v1 protocol.
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/social"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
	"github.com/peergo/peergo/services/core/internal/platform/objectstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

var (
	demoUserID                        = uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	demoCredentialRef                 = uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	fixtureUploaderID                 = uuid.MustParse("0198f20a-6da8-7e51-9c64-666666666666")
	fixtureCredentialRef              = uuid.MustParse("0198f20a-6da8-7e51-9c64-777777777777")
	fixtureUploadID                   = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4502")
	fixtureObjectID                   = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4503")
	fixtureReviewDecisionID           = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4504")
	fixtureReviewAuditID              = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4505")
	fixtureTrackerEventID             = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4506")
	fixtureCoverObjectID              = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4507")
	fixtureCoverLocationID            = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4508")
	fixtureRootRequestID              = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4510")
	fixtureReplyRequestID             = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4511")
	fixtureNestedRequestID            = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4512")
	fixtureAnnouncementRootRequestID  = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4513")
	fixtureAnnouncementReplyRequestID = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4514")
	moderationReporterID              = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4520")
	moderationCredentialRef           = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4521")
	fixtureReportRequestID            = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb4540")
)

const (
	developmentSeedCSRF      = "peergo-development-seed-csrf"
	fixtureAnnouncementID    = "welcome-to-peergo"
	fixtureTitle             = "Coastal Echoes 2026 1080p WEB-DL H.264 AAC"
	fixtureSubtitle          = "海岸回声 · 短片合集 · 普通话 / 简体中文字幕"
	legacyTorrentRootComment = "这个演示种子的画面信息和文件清单已经核对过了，欢迎补充实际体验。"
	torrentRootComment       = "文件信息已经核对，欢迎补充实际观看体验。"
	legacyTorrentReply       = "收到，我下载后会重点确认字幕时间轴；评论区先保持纯文本，迁移内容也不会直接执行旧站标记。"
	torrentReply             = "收到，我下载后会重点确认字幕时间轴和音轨信息。"
	legacyTorrentNestedReply = "好的，这条直接回复关系会保留；界面仍统一压成一级缩进，避免讨论越嵌越窄。"
	torrentNestedReply       = "好的，感谢补充；如果发现问题我会在这里更新。"
	legacyAnnouncementRoot   = "公告已读。首版把公告讨论接入统一评论库，编辑、删除和举报规则与种子评论保持一致。"
	announcementRoot         = "公告已读，期待后续完善更多站点功能。"
	legacyAnnouncementReply  = "收到；公告与种子仍由各自的强外键绑定，后台审核会明确显示内容来源。"
	announcementReply        = "感谢关注，如有建议可以继续留言。"
	legacyFixtureDescription = `## 资源说明

这是 PeerGo 本地开发环境使用的合成种子，用来演示接近正式站点的信息密度、下载流程与多级评论。种子只包含协议元数据，不会在本地生成影片载荷。

- 分类、类型、地区、分辨率和来源均使用目录中的受控选项
- 原始 .torrent 通过正式对象存储与审核链路落库
- 页面不伪造海报、截图、外部评分或实际影视作品信息`
	previousFixtureDescription = `## 资源说明

一组记录清晨海岸、渔港作业与沿海小镇日常的短片，保留自然环境声与普通话旁白。

- 1080p WEB-DL
- 普通话 AAC 2.0
- 简体中文字幕`
	fixtureDescription = `一组记录清晨海岸、渔港作业与沿海小镇日常的短片，保留自然环境声与普通话旁白。

- 1080p WEB-DL
- 普通话 AAC 2.0
- 简体中文字幕`
	fixtureMediaInfo = `General
Complete name                            : PeerGo.Comment.Demo.2026.1080p.mkv
Format                                   : Matroska
File size                                : 1.75 GiB
Duration                                 : 1 h 42 min
Overall bit rate                         : 2 456 kb/s

Video
Format                                   : AVC
Format profile                           : High@L4.1
Width                                    : 1 920 pixels
Height                                   : 1 080 pixels
Display aspect ratio                     : 16:9
Frame rate                               : 23.976 FPS
Bit depth                                : 8 bits

Audio #1
Format                                   : AAC LC
Title                                    : 普通话
Language                                 : Chinese
Channel(s)                               : 2 channels
Bit rate                                 : 192 kb/s

Text #1
Format                                   : UTF-8
Title                                    : 简体中文
Language                                 : Chinese`
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if os.Getenv("PEERGO_ENV") != "development" {
		logger.Error("core dev seed requires PEERGO_ENV=development")
		os.Exit(1)
	}

	settings, err := platformconfig.LoadTorrentUploadStorageTool()
	if err != nil {
		logger.Error("load core dev seed configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		logger.Error("open core database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		logger.Error("ping core database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		logger.Error("core database is not ready", "error", err)
		os.Exit(1)
	}

	store, err := objectstore.NewConfigured(ctx, settings.Store)
	if err != nil {
		logger.Error("compose torrent object store", "error", err)
		os.Exit(1)
	}
	registry, err := torrents.NewStoreRegistry(store)
	if err != nil {
		logger.Error("compose torrent object store registry", "error", err)
		os.Exit(1)
	}
	authorizer, auditConfig, err := newAuthorizationService(pool)
	if err != nil {
		logger.Error("compose development authorization", "error", err)
		os.Exit(1)
	}
	if err := authorizer.ValidateCatalog(ctx); err != nil {
		logger.Error("authorization catalog is not ready", "error", err)
		os.Exit(1)
	}

	now := time.Now().UTC()
	authenticator := newDevelopmentAuthenticator(now)
	rawMetainfo := fixtureMetainfo()
	rawCover, err := fixtureCoverPNG()
	if err != nil {
		logger.Error("build synthetic torrent cover", "error", err)
		os.Exit(1)
	}
	torrentID, err := ensurePublishedTorrent(ctx, pool, store, registry, authenticator, authorizer, auditConfig, rawMetainfo, rawCover, now)
	if err != nil {
		logger.Error("seed published torrent", "error", err)
		os.Exit(1)
	}
	if err := ensureFixtureCover(ctx, pool, store, torrentID, rawCover, now); err != nil {
		logger.Error("seed torrent cover", "error", err)
		os.Exit(1)
	}
	if err := ensureFixturePresentationMetadata(ctx, pool, torrentID, now); err != nil {
		logger.Error("seed torrent presentation metadata", "error", err)
		os.Exit(1)
	}
	commentCount, rootCommentID, err := ensureTorrentComments(ctx, pool, authenticator, authorizer, torrentID, now.Add(2*time.Second))
	if err != nil {
		logger.Error("seed torrent comments", "error", err)
		os.Exit(1)
	}
	announcementCommentCount, err := ensureAnnouncementComments(ctx, pool, authenticator, authorizer, now.Add(2500*time.Millisecond))
	if err != nil {
		logger.Error("seed announcement comments", "error", err)
		os.Exit(1)
	}
	reportReceipt, err := ensureCommentModerationFixture(
		ctx,
		pool,
		authenticator,
		authorizer,
		auditConfig,
		rootCommentID,
		now.Add(3*time.Second),
	)
	if err != nil {
		logger.Error("seed comment moderation case", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"synthetic Core discussions seeded",
		"torrent_id", torrentID,
		"torrent_comments", commentCount,
		"announcement_id", fixtureAnnouncementID,
		"announcement_comments", announcementCommentCount,
		"comment_report_id", reportReceipt.ID,
		"storage_backend", store.BackendID(),
	)
}

func ensureAnnouncementComments(
	ctx context.Context,
	pool *pgxpool.Pool,
	authenticator *developmentAuthenticator,
	authorizer authz.Authorizer,
	now time.Time,
) (int64, error) {
	repository, err := social.NewPostgresCommentRepository(pool)
	if err != nil {
		return 0, err
	}
	service, err := social.NewCommentService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		return 0, err
	}
	root, err := service.CreateAnnouncementComment(ctx, "demo", developmentSeedCSRF, social.CreateAnnouncementCommentInput{
		RequestID: fixtureAnnouncementRootRequestID, AnnouncementID: fixtureAnnouncementID,
		Body: legacyAnnouncementRoot,
	})
	if err != nil {
		return 0, fmt.Errorf("create fixture announcement comment: %w", err)
	}
	root, err = upgradeDevelopmentCommentCopy(ctx, service, "demo", root, legacyAnnouncementRoot, announcementRoot)
	if err != nil {
		return 0, fmt.Errorf("upgrade fixture announcement comment: %w", err)
	}
	reply, err := service.CreateAnnouncementComment(ctx, "fixture-uploader", developmentSeedCSRF, social.CreateAnnouncementCommentInput{
		RequestID: fixtureAnnouncementReplyRequestID, AnnouncementID: fixtureAnnouncementID,
		ParentCommentID: &root.ID,
		Body:            legacyAnnouncementReply,
	})
	if err != nil {
		return 0, fmt.Errorf("create fixture announcement reply: %w", err)
	}
	if _, err := upgradeDevelopmentCommentCopy(ctx, service, "fixture-uploader", reply, legacyAnnouncementReply, announcementReply); err != nil {
		return 0, fmt.Errorf("upgrade fixture announcement reply: %w", err)
	}
	page, err := service.ListAnnouncementComments(ctx, fixtureAnnouncementID, social.MaxCommentLimit, 0)
	if err != nil {
		return 0, fmt.Errorf("read fixture announcement comments: %w", err)
	}
	if page.Total < 2 {
		return 0, errors.New("development fixture announcement thread is incomplete")
	}
	return page.Total, nil
}

func newAuthorizationService(pool *pgxpool.Pool) (*authz.Service, audit.RecorderConfig, error) {
	auditConfig := audit.RecorderConfig{
		PseudonymKey:      []byte(os.Getenv("PEERGO_AUDIT_PSEUDONYM_KEY")),
		PseudonymKeyEpoch: strings.TrimSpace(os.Getenv("PEERGO_AUDIT_PSEUDONYM_KEY_EPOCH")),
	}
	recorder, err := audit.NewDecisionRecorder(audit.NewPostgresRepository(pool), auditConfig)
	if err != nil {
		return nil, audit.RecorderConfig{}, err
	}
	service, err := authz.NewService(authz.NewPostgresRepository(pool), recorder, time.Now)
	if err != nil {
		return nil, audit.RecorderConfig{}, err
	}
	return service, auditConfig, nil
}

func ensurePublishedTorrent(
	ctx context.Context,
	pool *pgxpool.Pool,
	store torrents.ObjectStore,
	registry *torrents.StoreRegistry,
	authenticator *developmentAuthenticator,
	authorizer authz.Authorizer,
	auditConfig audit.RecorderConfig,
	rawMetainfo []byte,
	rawCover []byte,
	now time.Time,
) (torrents.TorrentID, error) {
	torrentID, state, err := fixtureTorrentState(ctx, pool)
	if err != nil {
		return 0, err
	}
	if state != "" && state != string(torrents.StatePendingReview) && state != string(torrents.StatePublished) {
		return 0, fmt.Errorf("development fixture has unsupported state %q", state)
	}
	if state != string(torrents.StatePublished) {
		uploadRepository, err := torrents.NewPostgresTorrentUploadRepository(
			pool,
			torrents.PostgresTorrentUploadRepositoryConfig{
				NewTrackerAppender: func(tx pgx.Tx) trackerevent.Appender {
					return trackercontrol.NewPostgresOutbox(tx)
				},
			},
		)
		if err != nil {
			return 0, err
		}
		uploadService, err := torrents.NewTorrentUploadService(
			authenticator,
			authorizer,
			uploadRepository,
			registry,
			torrents.TorrentUploadServiceConfig{
				ActiveBackendID: store.BackendID(),
				Now:             func() time.Time { return now },
				NewUUID:         orderedUUIDs(fixtureCoverObjectID, fixtureObjectID),
			},
		)
		if err != nil {
			return 0, err
		}
		result, err := uploadService.Submit(
			ctx,
			"fixture-uploader",
			developmentSeedCSRF,
			fixtureTorrentUploadInput(rawMetainfo, rawCover),
		)
		if err != nil {
			return 0, fmt.Errorf("submit fixture torrent: %w", err)
		}
		if result.State != torrents.StatePendingReview || result.ID < 1 {
			return 0, errors.New("fixture upload returned an unexpected aggregate")
		}
		torrentID = result.ID
		if err := approveFixtureTorrent(ctx, pool, authorizer, auditConfig, torrentID, now.Add(time.Second)); err != nil {
			return 0, err
		}
	}
	if err := verifyFixtureObject(ctx, pool, store, torrentID, rawMetainfo); err != nil {
		return 0, err
	}
	return torrentID, nil
}

func fixtureTorrentUploadInput(rawMetainfo, rawCover []byte) torrents.TorrentUploadInput {
	return torrents.TorrentUploadInput{
		ID:          fixtureUploadID,
		CategoryID:  "movies",
		Title:       fixtureTitle,
		Subtitle:    fixtureSubtitle,
		Description: fixtureDescription,
		MediaInfo:   fixtureMediaInfo,
		FacetSelections: []torrents.FacetSelection{
			{FacetID: "genre", OptionKeys: []string{"剧情"}},
			{FacetID: "region", OptionKeys: []string{"mainland-china"}},
			{FacetID: "resolution", OptionKeys: []string{"1080p"}},
			{FacetID: "release-type", OptionKeys: []string{"web-dl"}},
		},
		Screenshots: []torrents.TorrentScreenshotInput{{Raw: append([]byte(nil), rawCover...)}},
		RawMetainfo: rawMetainfo,
	}
}

// ensureFixtureCover is the compatibility half of the development fixture.
// Fresh databases attach the same bytes through TorrentUploadService above;
// older local databases already contain a terminal published upload, so they
// cannot be replayed with a changed request fingerprint. This development-only
// repair writes the immutable object first, verifies it, then attaches exactly
// position zero without mutating any existing media evidence.
func ensureFixtureCover(ctx context.Context, pool *pgxpool.Pool, store torrents.ObjectStore, torrentID torrents.TorrentID, raw []byte, now time.Time) error {
	digest := torrents.ObjectSHA256(sha256.Sum256(raw))
	descriptor := torrents.StoredObjectDescriptor{SHA256: digest, ByteLength: int64(len(raw))}
	key := torrents.TorrentScreenshotObjectKey(digest, ".png")
	writeResult, err := store.PutIfAbsent(ctx, key, bytes.NewReader(raw), descriptor)
	if err != nil {
		return fmt.Errorf("store development cover: %w", err)
	}
	reader, err := store.Open(ctx, key, writeResult.VersionID)
	if err != nil {
		return fmt.Errorf("open development cover: %w", err)
	}
	verifyErr := func() error {
		defer reader.Body.Close()
		_, err := torrents.VerifyStoredObject(reader, descriptor)
		return err
	}()
	if verifyErr != nil {
		return fmt.Errorf("verify development cover: %w", verifyErr)
	}

	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
SELECT id
FROM torrents.torrents
WHERE id = $1 AND state = 'published'
`, torrentID).Scan(&torrentID); err != nil {
			return fmt.Errorf("read development cover torrent: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_screenshot_objects (
    id, content_sha256, byte_length, content_type, width, height, created_at
) VALUES ($1, $2, $3, 'image/png', 320, 480, $4)
ON CONFLICT (content_sha256) DO NOTHING
`, fixtureCoverObjectID, digest[:], descriptor.ByteLength, now); err != nil {
			return fmt.Errorf("insert development cover object: %w", err)
		}

		var objectID uuid.UUID
		var byteLength int64
		var contentType string
		var width, height int
		if err := tx.QueryRow(ctx, `
SELECT id, byte_length, content_type, width, height
FROM torrents.torrent_screenshot_objects
WHERE content_sha256 = $1
`, digest[:]).Scan(&objectID, &byteLength, &contentType, &width, &height); err != nil {
			return fmt.Errorf("resolve development cover object: %w", err)
		}
		if byteLength != descriptor.ByteLength || contentType != "image/png" || width != 320 || height != 480 {
			return errors.New("development cover object conflicts with immutable metadata")
		}

		var versionID any
		if writeResult.VersionID != "" {
			versionID = writeResult.VersionID
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_screenshot_object_locations (
    id, object_id, backend_id, object_key, version_id,
    observed_byte_length, observed_sha256, verified_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (object_id, backend_id) DO NOTHING
`, fixtureCoverLocationID, objectID, string(store.BackendID()), string(key), versionID,
			descriptor.ByteLength, digest[:], now); err != nil {
			return fmt.Errorf("insert development cover location: %w", err)
		}
		var storedKey string
		var storedVersion *string
		var observedLength int64
		var observedDigest []byte
		if err := tx.QueryRow(ctx, `
SELECT object_key, version_id, observed_byte_length, observed_sha256
FROM torrents.torrent_screenshot_object_locations
WHERE object_id = $1 AND backend_id = $2
`, objectID, string(store.BackendID())).Scan(&storedKey, &storedVersion, &observedLength, &observedDigest); err != nil {
			return fmt.Errorf("resolve development cover location: %w", err)
		}
		actualVersion := ""
		if storedVersion != nil {
			actualVersion = *storedVersion
		}
		if storedKey != string(key) || actualVersion != writeResult.VersionID || observedLength != descriptor.ByteLength || !bytes.Equal(observedDigest, digest[:]) {
			return errors.New("development cover location conflicts with verified bytes")
		}

		if _, err := tx.Exec(ctx, `
INSERT INTO torrents.torrent_screenshots (torrent_id, object_id, position, created_at)
VALUES ($1, $2, 0, $3)
ON CONFLICT (torrent_id, position) DO NOTHING
`, torrentID, objectID, now); err != nil {
			return fmt.Errorf("attach development cover: %w", err)
		}
		var attachedObjectID uuid.UUID
		if err := tx.QueryRow(ctx, `
SELECT object_id
FROM torrents.torrent_screenshots
WHERE torrent_id = $1 AND position = 0
`, torrentID).Scan(&attachedObjectID); err != nil {
			return fmt.Errorf("resolve development cover attachment: %w", err)
		}
		if attachedObjectID != objectID {
			return errors.New("development cover position already contains different immutable evidence")
		}
		return nil
	})
}

// fixtureCoverPNG produces a deterministic synthetic poster. It contains no
// copied artwork or user media; the bytes exist only to exercise the same
// decoded-image, content-addressed storage and public-cover path as production.
func fixtureCoverPNG() ([]byte, error) {
	const width, height = 320, 480
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if y < 300 {
				ratio := float64(y) / 300
				canvas.SetRGBA(x, y, color.RGBA{
					R: uint8(24 + 176*ratio),
					G: uint8(45 + 86*ratio),
					B: uint8(82 + 54*ratio),
					A: 255,
				})
				continue
			}
			wave := (x/18 + y/12) % 2
			shade := uint8(26 + wave*18 + (height-y)/12)
			canvas.SetRGBA(x, y, color.RGBA{R: 12, G: 72 + shade/3, B: 112 + shade, A: 255})
		}
	}
	for y := 92; y < 190; y++ {
		for x := 111; x < 209; x++ {
			dx, dy := x-160, y-141
			if dx*dx+dy*dy <= 48*48 {
				canvas.SetRGBA(x, y, color.RGBA{R: 255, G: 210, B: 122, A: 255})
			}
		}
	}
	for y := 300; y < height; y += 28 {
		for x := 0; x < width; x++ {
			if (x+y/2)%54 < 24 {
				canvas.SetRGBA(x, y, color.RGBA{R: 151, G: 215, B: 220, A: 255})
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, fmt.Errorf("encode synthetic cover: %w", err)
	}
	return encoded.Bytes(), nil
}

// ensureFixturePresentationMetadata upgrades only the deterministic synthetic
// development row created by older PeerGo revisions. Product writes and PtYes
// imports never use this compatibility path: fresh fixtures still go through
// the normal upload transaction above, while an existing local fixture keeps
// any non-empty prose the developer may already have customized.
func ensureFixturePresentationMetadata(ctx context.Context, pool *pgxpool.Pool, torrentID torrents.TorrentID, now time.Time) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var upgradeTitle, upgradeSubtitle, enrichDescription, enrichMediaInfo bool
		if err := tx.QueryRow(ctx, `
SELECT
    title = 'PeerGo 评论功能演示 2026 1080p WEB-DL',
    subtitle = '开发环境夹具 · 多级回复 · 真实对象存储',
    description = '' OR description = $2 OR description = $3,
    media_info = ''
FROM torrents.torrents
WHERE id = $1
`, torrentID, legacyFixtureDescription, previousFixtureDescription).Scan(&upgradeTitle, &upgradeSubtitle, &enrichDescription, &enrichMediaInfo); err != nil {
			return fmt.Errorf("read development fixture presentation metadata: %w", err)
		}

		result, err := tx.Exec(ctx, `
WITH selected(facet_id, option_key, position) AS (
    VALUES
        ('genre'::text, '剧情'::text, 0),
        ('region'::text, 'mainland-china'::text, 0),
        ('resolution'::text, '1080p'::text, 0),
        ('release-type'::text, 'web-dl'::text, 0)
)
INSERT INTO torrents.torrent_facet_values (
    torrent_id, category_id, facet_id, option_key,
    selection_mode, position, created_at
)
SELECT
    torrent.id,
    allowed.category_id,
    allowed.facet_id,
    allowed.option_key,
    allowed.selection_mode,
    selected.position,
    $2
FROM torrents.torrents AS torrent
JOIN selected ON true
JOIN catalog.category_facet_options AS allowed
  ON allowed.category_id = torrent.category_id
 AND allowed.facet_id = selected.facet_id
 AND allowed.option_key = selected.option_key
WHERE torrent.id = $1
ON CONFLICT (torrent_id, facet_id, option_key) DO NOTHING
`, torrentID, now)
		if err != nil {
			return fmt.Errorf("seed development fixture facets: %w", err)
		}

		presentationChanged := upgradeTitle || upgradeSubtitle || enrichDescription || enrichMediaInfo || result.RowsAffected() > 0
		if presentationChanged {
			if _, err := tx.Exec(ctx, `
UPDATE torrents.torrents
SET
		title = CASE WHEN $2 THEN $3 ELSE title END,
		subtitle = CASE WHEN $4 THEN $5 ELSE subtitle END,
		description = CASE WHEN $6 THEN $7 ELSE description END,
		media_info = CASE WHEN $8 THEN $9 ELSE media_info END,
		version = version + 1,
		updated_at = GREATEST(updated_at, $10)
WHERE id = $1
`, torrentID, upgradeTitle, fixtureTitle, upgradeSubtitle, fixtureSubtitle, enrichDescription, fixtureDescription, enrichMediaInfo, fixtureMediaInfo, now); err != nil {
				return fmt.Errorf("upgrade development fixture presentation metadata: %w", err)
			}
		}

		// catalog.torrents is the user-facing read projection created when a
		// torrent is approved. Keep the deterministic fixture projection aligned
		// with its source row, but only replace the two exact legacy strings. This
		// prevents the compatibility seed from touching imported or customized
		// catalog data, even if the synthetic public ID is reused accidentally.
		if _, err := tx.Exec(ctx, `
UPDATE catalog.torrents AS projection
SET
    name = CASE
        WHEN projection.name = 'PeerGo 评论功能演示 2026 1080p WEB-DL' THEN $2
        ELSE projection.name
    END,
    subtitle = CASE
        WHEN projection.subtitle = '开发环境夹具 · 多级回复 · 真实对象存储' THEN $3
        ELSE projection.subtitle
    END
FROM torrents.torrents AS source
WHERE projection.id = $1
  AND source.id = $1
  AND source.title = $2
  AND source.subtitle = $3
  AND (
      projection.name = 'PeerGo 评论功能演示 2026 1080p WEB-DL'
      OR projection.subtitle = '开发环境夹具 · 多级回复 · 真实对象存储'
  )
`, torrentID, fixtureTitle, fixtureSubtitle); err != nil {
			return fmt.Errorf("upgrade development fixture catalog projection: %w", err)
		}
		return nil
	})
}

func approveFixtureTorrent(ctx context.Context, pool *pgxpool.Pool, authorizer authz.Authorizer, auditConfig audit.RecorderConfig, torrentID torrents.TorrentID, now time.Time) error {
	auditConfig.NewEventID = func() uuid.UUID { return fixtureReviewAuditID }
	auditBuilder, err := audit.NewTorrentReviewEventBuilder(auditConfig)
	if err != nil {
		return err
	}
	repository, err := review.NewPostgresRepository(
		pool,
		auditBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
		trackercontrol.NewEligibilityEventBuilder(func() uuid.UUID { return fixtureTrackerEventID }),
		func(tx pgx.Tx) trackerevent.Appender { return trackercontrol.NewPostgresOutbox(tx) },
		func(tx pgx.Tx) review.NotificationAppender { return notifications.NewPostgresReviewAppender(tx) },
	)
	if err != nil {
		return err
	}
	service, err := review.NewStaffService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		return err
	}
	result, err := service.Decide(ctx, authz.StaffActor{
		Subject:            authz.Subject{ID: demoUserID, Status: authz.SubjectActive},
		MFAAuthenticatedAt: now,
	}, review.DecideInput{
		DecisionID:      fixtureReviewDecisionID,
		TorrentID:       torrentID,
		ExpectedVersion: 1,
		Decision:        review.DecisionApprove,
		ReasonCode:      review.ReasonMeetsRequirements,
		Reason:          "开发环境演示种子通过自动化审核。",
	})
	if err != nil {
		return fmt.Errorf("approve fixture torrent: %w", err)
	}
	if result.State != torrents.StatePublished || result.Version != 2 {
		return errors.New("fixture review returned an unexpected aggregate")
	}
	return nil
}

func fixtureTorrentState(ctx context.Context, pool *pgxpool.Pool) (torrents.TorrentID, string, error) {
	var torrentID torrents.TorrentID
	var state string
	err := pool.QueryRow(ctx, `
SELECT torrent.id, torrent.state
FROM torrents.torrent_uploads AS upload
JOIN torrents.torrents AS torrent ON torrent.id = upload.torrent_id
WHERE upload.id = $1
`, fixtureUploadID).Scan(&torrentID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("read development fixture state: %w", err)
	}
	return torrentID, state, nil
}

func verifyFixtureObject(ctx context.Context, pool *pgxpool.Pool, store torrents.ObjectStore, torrentID torrents.TorrentID, rawMetainfo []byte) error {
	parsed, err := torrents.ParseV1(rawMetainfo, torrents.ValidationProfileStrictUpload)
	if err != nil {
		return fmt.Errorf("parse development metainfo: %w", err)
	}
	descriptor := torrents.StoredObjectDescriptor{SHA256: parsed.ObjectSHA256, ByteLength: parsed.ObjectByteLength}
	expectedKey := torrents.TorrentObjectKey(descriptor.SHA256)
	var backendID, objectKey, versionID string
	err = pool.QueryRow(ctx, `
SELECT location.backend_id, location.object_key, COALESCE(location.version_id, '')
FROM torrents.torrents AS torrent
JOIN torrents.torrent_object_locations AS location
  ON location.object_id = torrent.object_id
WHERE torrent.id = $1
  AND location.state IN ('verified', 'retiring')
ORDER BY location.is_preferred DESC, location.verified_at DESC, location.id
LIMIT 1
`, torrentID).Scan(&backendID, &objectKey, &versionID)
	if err != nil {
		return fmt.Errorf("read development fixture location: %w", err)
	}
	if backendID != string(store.BackendID()) || objectKey != string(expectedKey) {
		return fmt.Errorf("development fixture location is %s/%s, want %s/%s", backendID, objectKey, store.BackendID(), expectedKey)
	}
	if _, err := store.PutIfAbsent(ctx, expectedKey, bytes.NewReader(rawMetainfo), descriptor); err != nil {
		return fmt.Errorf("restore development fixture object: %w", err)
	}
	reader, err := store.Open(ctx, expectedKey, versionID)
	if err != nil {
		return fmt.Errorf("open development fixture object: %w", err)
	}
	_, verifyErr := torrents.VerifyStoredObject(reader, descriptor)
	closeErr := reader.Body.Close()
	if verifyErr != nil {
		return fmt.Errorf("verify development fixture object: %w", verifyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close development fixture object: %w", closeErr)
	}
	return nil
}

func ensureTorrentComments(
	ctx context.Context,
	pool *pgxpool.Pool,
	authenticator *developmentAuthenticator,
	authorizer authz.Authorizer,
	torrentID torrents.TorrentID,
	now time.Time,
) (int64, uuid.UUID, error) {
	repository, err := social.NewPostgresCommentRepository(pool)
	if err != nil {
		return 0, uuid.Nil, err
	}
	service, err := social.NewCommentService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		return 0, uuid.Nil, err
	}
	root, err := service.CreateTorrentComment(ctx, "fixture-uploader", developmentSeedCSRF, social.CreateTorrentCommentInput{
		RequestID: fixtureRootRequestID,
		TorrentID: int64(torrentID),
		Body:      legacyTorrentRootComment,
	})
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("create fixture root comment: %w", err)
	}
	root, err = upgradeDevelopmentCommentCopy(ctx, service, "fixture-uploader", root, legacyTorrentRootComment, torrentRootComment)
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("upgrade fixture root comment: %w", err)
	}
	reply, err := service.CreateTorrentComment(ctx, "demo", developmentSeedCSRF, social.CreateTorrentCommentInput{
		RequestID:       fixtureReplyRequestID,
		TorrentID:       int64(torrentID),
		ParentCommentID: &root.ID,
		Body:            legacyTorrentReply,
	})
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("create fixture reply: %w", err)
	}
	reply, err = upgradeDevelopmentCommentCopy(ctx, service, "demo", reply, legacyTorrentReply, torrentReply)
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("upgrade fixture reply: %w", err)
	}
	nestedReply, err := service.CreateTorrentComment(ctx, "fixture-uploader", developmentSeedCSRF, social.CreateTorrentCommentInput{
		RequestID:       fixtureNestedRequestID,
		TorrentID:       int64(torrentID),
		ParentCommentID: &reply.ID,
		Body:            legacyTorrentNestedReply,
	})
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("create fixture nested reply: %w", err)
	}
	if _, err := upgradeDevelopmentCommentCopy(ctx, service, "fixture-uploader", nestedReply, legacyTorrentNestedReply, torrentNestedReply); err != nil {
		return 0, uuid.Nil, fmt.Errorf("upgrade fixture nested reply: %w", err)
	}
	page, err := service.ListTorrentComments(ctx, int64(torrentID), social.MaxCommentLimit, 0)
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("read fixture comments: %w", err)
	}
	if page.Total < 3 {
		return 0, uuid.Nil, errors.New("development fixture comment thread is incomplete")
	}
	return page.Total, root.ID, nil
}

// upgradeDevelopmentCommentCopy keeps the historical create request stable so
// rerunning devseed remains idempotent, then uses the same audited self-edit
// path as the web UI to replace only the exact legacy fixture wording.
func upgradeDevelopmentCommentCopy(
	ctx context.Context,
	service *social.CommentService,
	cookieToken string,
	comment social.Comment,
	legacyBody string,
	body string,
) (social.Comment, error) {
	if comment.Body != legacyBody {
		return comment, nil
	}
	return service.UpdateMyComment(ctx, cookieToken, developmentSeedCSRF, social.UpdateCommentInput{
		CommentID:       comment.ID,
		ExpectedVersion: comment.Version,
		Body:            body,
	})
}

func ensureCommentModerationFixture(
	ctx context.Context,
	pool *pgxpool.Pool,
	authenticator *developmentAuthenticator,
	authorizer authz.Authorizer,
	auditConfig audit.RecorderConfig,
	commentID uuid.UUID,
	now time.Time,
) (social.CommentReportReceipt, error) {
	// Reports do not emit an external audit event, but the repository also owns
	// later staff decisions. Compose its fail-closed audit dependencies here so
	// the development fixture exercises the same runtime boundary as Core.
	eventBuilder, err := audit.NewCommentModerationDecisionEventBuilder(auditConfig)
	if err != nil {
		return social.CommentReportReceipt{}, err
	}
	repository, err := social.NewPostgresCommentModerationRepository(
		pool,
		eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		return social.CommentReportReceipt{}, err
	}
	service, err := social.NewCommentModerationService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		return social.CommentReportReceipt{}, err
	}
	receipt, err := service.CreateReport(ctx, "moderation-reporter", developmentSeedCSRF, social.CreateCommentReportInput{
		RequestID:  fixtureReportRequestID,
		CommentID:  commentID,
		ReasonCode: social.CommentReportOffTopic,
		Details:    "开发环境演示举报：用于核对聚合案件、职责隔离与有界处置。",
	})
	if err != nil {
		return social.CommentReportReceipt{}, fmt.Errorf("create fixture comment report: %w", err)
	}
	if receipt.ID == uuid.Nil || receipt.CommentID != commentID || receipt.ReasonCode != social.CommentReportOffTopic {
		return social.CommentReportReceipt{}, errors.New("development fixture comment report is incomplete")
	}
	return receipt, nil
}

type developmentAuthenticator struct {
	users map[string]identity.User
}

func newDevelopmentAuthenticator(now time.Time) *developmentAuthenticator {
	verifiedAt := now.Add(-24 * time.Hour)
	return &developmentAuthenticator{users: map[string]identity.User{
		"demo": {
			ID: demoUserID, CredentialRef: demoCredentialRef,
			Username: "demo", DisplayName: "星河旅人", EmailVerifiedAt: &verifiedAt,
		},
		"fixture-uploader": {
			ID: fixtureUploaderID, CredentialRef: fixtureCredentialRef,
			Username: "demo-target", DisplayName: "远岸", EmailVerifiedAt: &verifiedAt,
		},
		"moderation-reporter": {
			ID: moderationReporterID, CredentialRef: moderationCredentialRef,
			Username: "moderation-reporter", DisplayName: "评论举报演示成员", EmailVerifiedAt: &verifiedAt,
		},
	}}
}

func (authenticator *developmentAuthenticator) AuthenticateWrite(_ context.Context, cookieToken, csrfToken string) (identity.WebSession, error) {
	if csrfToken != developmentSeedCSRF {
		return identity.WebSession{}, identity.ErrInvalidCSRF
	}
	user, exists := authenticator.users[cookieToken]
	if !exists {
		return identity.WebSession{}, identity.ErrSessionNotFound
	}
	return identity.WebSession{User: user, CookieToken: cookieToken, CSRFToken: csrfToken}, nil
}

func orderedUUIDs(values ...uuid.UUID) func() uuid.UUID {
	index := 0
	return func() uuid.UUID {
		if index >= len(values) {
			return uuid.Nil
		}
		value := values[index]
		index++
		return value
	}
}

func fixtureMetainfo() []byte {
	const (
		payloadLength = int64(1_879_048_192)
		pieceLength   = int64(4 << 20)
	)
	pieceCount := int((payloadLength + pieceLength - 1) / pieceLength)
	pieces := make([]byte, 0, pieceCount*sha1.Size)
	for index := 0; index < pieceCount; index++ {
		// Piece hashes are synthetic protocol metadata; no payload is created.
		digest := sha1.Sum([]byte(fmt.Sprintf("peergo-development-piece:%d", index))) // #nosec G401 -- BitTorrent v1 requires SHA-1 piece hashes.
		pieces = append(pieces, digest[:]...)
	}
	info := bencodeDictionary(map[string][]byte{
		"length":       bencodeInteger(payloadLength),
		"name":         bencodeBytes([]byte("PeerGo.Comment.Demo.2026.1080p.mkv")),
		"piece length": bencodeInteger(pieceLength),
		"pieces":       bencodeBytes(pieces),
		"private":      bencodeInteger(1),
		"source":       bencodeBytes([]byte("[PeerGo]")),
	})
	return bencodeDictionary(map[string][]byte{"info": info})
}

// These tiny encoders are local to the deterministic fixture. Runtime parsing
// remains owned by torrents.ParseV1; the seed does not expose a second bencode
// implementation to product code.
func bencodeBytes(value []byte) []byte {
	encoded := make([]byte, 0, len(value)+24)
	encoded = strconv.AppendInt(encoded, int64(len(value)), 10)
	encoded = append(encoded, ':')
	return append(encoded, value...)
}

func bencodeInteger(value int64) []byte {
	encoded := []byte{'i'}
	encoded = strconv.AppendInt(encoded, value, 10)
	return append(encoded, 'e')
}

func bencodeDictionary(values map[string][]byte) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return bytes.Compare([]byte(keys[left]), []byte(keys[right])) < 0
	})
	encoded := []byte{'d'}
	for _, key := range keys {
		encoded = append(encoded, bencodeBytes([]byte(key))...)
		encoded = append(encoded, values[key]...)
	}
	return append(encoded, 'e')
}

var (
	_ torrents.TorrentUploadSessionAuthenticator = (*developmentAuthenticator)(nil)
	_ social.CommentSessionAuthenticator         = (*developmentAuthenticator)(nil)
)
