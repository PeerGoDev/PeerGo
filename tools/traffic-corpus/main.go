// Command traffic-corpus sends fixed development-only, client-shaped HTTP
// announces through Tracker's real WAL publisher and verifies the resulting
// Tracker Ledger and Core traffic projections. It is an orchestration harness,
// never a runtime dependency of either service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/tracker/clientcorpus"
)

const (
	corpusName             = "peergo-client-http-wal-v3-public-explanation"
	corpusUserID           = "0198f20a-6da8-7e51-9c64-111111111111"
	corpusTorrentID        = int64(9_000_000_001)
	defaultWaitTimeout     = 60 * time.Second
	gib                    = int64(1024 * 1024 * 1024)
	corpusRawUploaded      = 3 * gib
	corpusRawDownloaded    = 3 * gib
	corpusCreditedUploaded = 5 * gib
	corpusChargedDownload  = gib / 2
)

var errProjectionPending = errors.New("traffic corpus projection is still pending")

type settings struct {
	CoreDatabaseURL    string
	TrackerDatabaseURL string
	NATSURL            string
	Stream             string
	Subject            string
	WaitTimeout        time.Duration
}

type fixture struct {
	UserID         string
	TorrentID      int64
	TorrentTitle   string
	InfoHashV1     string
	TotalSizeBytes int64
}

type byteTotals struct {
	RawUploaded       int64 `json:"raw_uploaded"`
	RawDownloaded     int64 `json:"raw_downloaded"`
	CreditedUploaded  int64 `json:"credited_uploaded"`
	ChargedDownloaded int64 `json:"charged_downloaded"`
	EntryCount        int64 `json:"entry_count"`
}

type ledgerEvidence struct {
	InboxEvents          int64 `json:"inbox_events"`
	RawIntervals         int64 `json:"raw_intervals"`
	FinalSettlements     int64 `json:"final_settlements"`
	FirstPolicySegments  int64 `json:"first_policy_segments"`
	SecondPolicySegments int64 `json:"second_policy_segments"`
	PublishedOutbox      int64 `json:"published_outbox"`
}

type coreExplanationEvidence struct {
	CompleteEntries         int64 `json:"complete_entries"`
	ProjectedSegments       int64 `json:"projected_segments"`
	FirstProjectedSegments  int64 `json:"first_projected_segments"`
	SecondProjectedSegments int64 `json:"second_projected_segments"`
}

type verificationReport struct {
	Scenario               string                             `json:"scenario"`
	ExistingProjection     bool                               `json:"existing_projection"`
	UserID                 string                             `json:"user_id"`
	TorrentID              int64                              `json:"torrent_id"`
	TorrentTitle           string                             `json:"torrent_title"`
	HTTPRequests           []clientcorpus.HTTPRequestEvidence `json:"http_requests"`
	Published              []clientcorpus.PublishEvidence     `json:"published"`
	WAL                    clientcorpus.WALEvidence           `json:"wal"`
	ClientProfileSources   map[string]string                  `json:"client_profile_sources"`
	TorrentControlSequence int64                              `json:"torrent_control_sequence"`
	SubjectControlSequence int64                              `json:"subject_control_sequence"`
	CorpusTotals           byteTotals                         `json:"corpus_totals"`
	CoreUserTotals         byteTotals                         `json:"core_user_totals"`
	CoreExplanations       coreExplanationEvidence            `json:"core_explanations"`
	Ledger                 ledgerEvidence                     `json:"ledger"`
	VerifiedAt             time.Time                          `json:"verified_at"`
	ProductionBoundaries   []string                           `json:"production_boundaries"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Stdout, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "traffic corpus failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, output io.Writer, getenv func(string) string) error {
	configuration, err := loadSettings(getenv)
	if err != nil {
		return err
	}
	corePool, err := openPool(ctx, configuration.CoreDatabaseURL, "Core")
	if err != nil {
		return err
	}
	defer corePool.Close()
	trackerPool, err := openPool(ctx, configuration.TrackerDatabaseURL, "Tracker Ledger")
	if err != nil {
		return err
	}
	defer trackerPool.Close()

	corpusFixture, err := loadFixture(ctx, corePool)
	if err != nil {
		return err
	}
	clientResult, err := clientcorpus.Run(ctx, clientcorpus.Config{
		Environment: "development",
		Fixture: clientcorpus.Fixture{
			UserID: corpusFixture.UserID, TorrentID: corpusFixture.TorrentID,
			InfoHashV1:     corpusFixture.InfoHashV1,
			TotalSizeBytes: corpusFixture.TotalSizeBytes,
		},
		NATSURL: configuration.NATSURL, Stream: configuration.Stream,
		Subject: configuration.Subject, Timeout: configuration.WaitTimeout,
	})
	if err != nil {
		return err
	}
	existingProjection := allPublishesDuplicate(clientResult.Published)

	waitCtx, cancelWait := context.WithTimeout(ctx, configuration.WaitTimeout)
	defer cancelWait()
	report, err := waitForVerification(waitCtx, corePool, trackerPool, corpusFixture, clientResult, existingProjection)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode traffic corpus report: %w", err)
	}
	return nil
}

func loadSettings(getenv func(string) string) (settings, error) {
	if strings.TrimSpace(getenv("PEERGO_ENV")) != "development" {
		return settings{}, errors.New("PEERGO_ENV must be development")
	}
	result := settings{
		CoreDatabaseURL:    strings.TrimSpace(getenv("PEERGO_TRAFFIC_CORPUS_CORE_DATABASE_URL")),
		TrackerDatabaseURL: strings.TrimSpace(getenv("PEERGO_TRAFFIC_CORPUS_TRACKER_DATABASE_URL")),
		NATSURL:            strings.TrimSpace(getenv("PEERGO_TRAFFIC_CORPUS_NATS_URL")),
		Stream:             strings.TrimSpace(getenv("PEERGO_TRAFFIC_CORPUS_STREAM")),
		Subject:            strings.TrimSpace(getenv("PEERGO_TRAFFIC_CORPUS_SUBJECT")),
		WaitTimeout:        defaultWaitTimeout,
	}
	if raw := strings.TrimSpace(getenv("PEERGO_TRAFFIC_CORPUS_WAIT_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < time.Second || parsed > 2*time.Minute {
			return settings{}, errors.New("PEERGO_TRAFFIC_CORPUS_WAIT_TIMEOUT must be between 1s and 2m")
		}
		result.WaitTimeout = parsed
	}
	if err := validateLoopbackURL(result.CoreDatabaseURL, "postgres", "postgresql"); err != nil {
		return settings{}, fmt.Errorf("Core database URL: %w", err)
	}
	if err := validateLoopbackURL(result.TrackerDatabaseURL, "postgres", "postgresql"); err != nil {
		return settings{}, fmt.Errorf("Tracker Ledger database URL: %w", err)
	}
	if err := validateLoopbackURL(result.NATSURL, "nats"); err != nil {
		return settings{}, fmt.Errorf("NATS URL: %w", err)
	}
	if !trackerannouncev1.ValidStreamName(result.Stream) || !trackerannouncev1.ValidLiteralSubject(result.Subject) {
		return settings{}, errors.New("traffic corpus stream or subject is invalid")
	}
	return result, nil
}

func validateLoopbackURL(raw string, allowedSchemes ...string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return errors.New("a URL with an explicit loopback host is required")
	}
	allowed := false
	for _, scheme := range allowedSchemes {
		if parsed.Scheme == scheme {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("scheme %q is not allowed", parsed.Scheme)
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (address == nil || !address.IsLoopback()) {
		return fmt.Errorf("host %q is not loopback", host)
	}
	return nil
}

func openPool(ctx context.Context, databaseURL, label string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", label, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping %s database: %w", label, err)
	}
	return pool, nil
}

func loadFixture(ctx context.Context, pool *pgxpool.Pool) (fixture, error) {
	var result fixture
	err := pool.QueryRow(ctx, `
SELECT
    subject.id::text,
    torrent.id,
    torrent.title,
    encode(torrent.info_hash_v1, 'hex'),
    torrent.total_size_bytes
FROM identity.users AS subject
JOIN torrents.torrents AS torrent
  ON torrent.id = $2
WHERE subject.id = $1::uuid
  AND subject.username = 'demo'
  AND subject.status = 'active'
  AND subject.email_verified_at IS NOT NULL
  AND torrent.uploader_id = subject.id
  AND torrent.state = 'pending_review'`, corpusUserID, corpusTorrentID).Scan(
		&result.UserID,
		&result.TorrentID,
		&result.TorrentTitle,
		&result.InfoHashV1,
		&result.TotalSizeBytes,
	)
	if err != nil {
		return fixture{}, fmt.Errorf("load development traffic fixture; run make db-seed first: %w", err)
	}
	if result.UserID != corpusUserID || result.TorrentID != corpusTorrentID ||
		result.TorrentID < 1 || len(result.InfoHashV1) != 40 || result.TotalSizeBytes != 3*gib {
		return fixture{}, errors.New("development traffic fixture violates its stable identity")
	}
	return result, nil
}

func allPublishesDuplicate(published []clientcorpus.PublishEvidence) bool {
	if len(published) == 0 {
		return false
	}
	for _, item := range published {
		if !item.Duplicate {
			return false
		}
	}
	return true
}

func waitForVerification(
	ctx context.Context,
	corePool *pgxpool.Pool,
	trackerPool *pgxpool.Pool,
	source fixture,
	clientResult clientcorpus.Result,
	existingProjection bool,
) (verificationReport, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		report, err := verifyProjection(ctx, corePool, trackerPool, source, clientResult, existingProjection)
		if err == nil {
			return report, nil
		}
		if !errors.Is(err, errProjectionPending) {
			return verificationReport{}, err
		}
		select {
		case <-ctx.Done():
			return verificationReport{}, fmt.Errorf("wait for traffic corpus projection: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func verifyProjection(
	ctx context.Context,
	corePool *pgxpool.Pool,
	trackerPool *pgxpool.Pool,
	source fixture,
	clientResult clientcorpus.Result,
	existingProjection bool,
) (verificationReport, error) {
	ledger, err := loadLedgerEvidence(ctx, trackerPool, clientResult.EventIDs)
	if err != nil {
		return verificationReport{}, err
	}
	if ledger.InboxEvents != 4 || ledger.RawIntervals != 2 || ledger.FinalSettlements != 2 ||
		ledger.FirstPolicySegments != 1 || ledger.SecondPolicySegments != 2 || ledger.PublishedOutbox != 2 {
		return verificationReport{}, errProjectionPending
	}
	corpusTotals, coreTotals, explanations, err := loadAndVerifyCoreEvidence(ctx, corePool, source.UserID, clientResult.EventIDs)
	if err != nil {
		return verificationReport{}, err
	}
	return verificationReport{
		Scenario:               corpusName,
		ExistingProjection:     existingProjection,
		UserID:                 source.UserID,
		TorrentID:              source.TorrentID,
		TorrentTitle:           source.TorrentTitle,
		HTTPRequests:           clientResult.Requests,
		Published:              clientResult.Published,
		WAL:                    clientResult.WAL,
		ClientProfileSources:   clientResult.ClientProfileSources,
		TorrentControlSequence: clientResult.TorrentControlSequence,
		SubjectControlSequence: clientResult.SubjectControlSequence,
		CorpusTotals:           corpusTotals,
		CoreUserTotals:         coreTotals,
		CoreExplanations:       explanations,
		Ledger:                 ledger,
		VerifiedAt:             time.Now().UTC().Round(0),
		ProductionBoundaries: []string{
			"client-shaped requests enter through the real loopback HTTP Tracker handler",
			"signed in-memory admissions use only fixed synthetic development identities",
			"PGW1 WAL payloads exactly match the canonical JetStream publications",
			"Tracker Ledger and Core traffic rows are written only by production workers",
			"the harness performs read-only cross-service database verification",
		},
	}, nil
}

func loadLedgerEvidence(ctx context.Context, pool *pgxpool.Pool, eventIDs clientcorpus.EventIDs) (ledgerEvidence, error) {
	var result ledgerEvidence
	err := pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM settlement.event_inbox
      WHERE event_id IN ($1::uuid, $2::uuid, $3::uuid, $4::uuid)),
    (SELECT count(*) FROM ledger.raw_session_intervals
      WHERE event_id IN ($2::uuid, $3::uuid)),
    (SELECT count(*) FROM ledger.traffic_settlements
      WHERE settlement_id IN ($2::uuid, $3::uuid)),
    (SELECT count(*) FROM ledger.traffic_settlement_segments
      WHERE settlement_id = $2::uuid),
    (SELECT count(*) FROM ledger.traffic_settlement_segments
      WHERE settlement_id = $3::uuid),
    (SELECT count(*) FROM settlement.traffic_outbox
      WHERE event_id IN ($2::uuid, $3::uuid) AND published_at IS NOT NULL)`,
		eventIDs.Baseline,
		eventIDs.FirstInterval,
		eventIDs.SecondInterval,
		eventIDs.BaselineOnly,
	).Scan(
		&result.InboxEvents,
		&result.RawIntervals,
		&result.FinalSettlements,
		&result.FirstPolicySegments,
		&result.SecondPolicySegments,
		&result.PublishedOutbox,
	)
	if err != nil {
		return ledgerEvidence{}, fmt.Errorf("read Tracker Ledger traffic corpus evidence: %w", err)
	}
	return result, nil
}

func loadAndVerifyCoreEvidence(ctx context.Context, pool *pgxpool.Pool, userID string, eventIDs clientcorpus.EventIDs) (byteTotals, byteTotals, coreExplanationEvidence, error) {
	rows, err := pool.Query(ctx, `
SELECT
    entry.settlement_id::text,
    entry.raw_uploaded,
    entry.raw_downloaded,
    entry.credited_uploaded,
    entry.charged_downloaded,
    explanation.status,
    explanation.segment_count,
    (SELECT count(*) FROM traffic.user_traffic_entry_segments AS segment
      WHERE segment.settlement_id = entry.settlement_id)
FROM traffic.user_traffic_entries AS entry
JOIN traffic.user_traffic_entry_explanations AS explanation
  ON explanation.settlement_id = entry.settlement_id
WHERE entry.settlement_id IN ($1::uuid, $2::uuid)`, eventIDs.FirstInterval, eventIDs.SecondInterval)
	if err != nil {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("read Core corpus entries: %w", err)
	}
	defer rows.Close()
	entries := make(map[string]byteTotals, 2)
	projectedSegments := make(map[string]int64, 2)
	for rows.Next() {
		var id string
		var totals byteTotals
		var status string
		var declaredSegments int32
		var actualSegments int64
		if err := rows.Scan(
			&id,
			&totals.RawUploaded,
			&totals.RawDownloaded,
			&totals.CreditedUploaded,
			&totals.ChargedDownloaded,
			&status,
			&declaredSegments,
			&actualSegments,
		); err != nil {
			return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("scan Core corpus entry: %w", err)
		}
		if status != "complete" || int64(declaredSegments) != actualSegments {
			return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("Core corpus explanation is incomplete: settlement=%s status=%s declared=%d actual=%d", id, status, declaredSegments, actualSegments)
		}
		totals.EntryCount = 1
		entries[id] = totals
		projectedSegments[id] = actualSegments
	}
	if err := rows.Err(); err != nil {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("iterate Core corpus entries: %w", err)
	}
	if len(entries) < 2 {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, errProjectionPending
	}
	wantFirst := byteTotals{RawUploaded: gib, RawDownloaded: 2 * gib, CreditedUploaded: 2 * gib, ChargedDownloaded: 0, EntryCount: 1}
	wantSecond := byteTotals{RawUploaded: 2 * gib, RawDownloaded: gib, CreditedUploaded: 3 * gib, ChargedDownloaded: gib / 2, EntryCount: 1}
	if entries[eventIDs.FirstInterval] != wantFirst || entries[eventIDs.SecondInterval] != wantSecond {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("Core corpus entries do not match immutable policy expectations: first=%+v second=%+v", entries[eventIDs.FirstInterval], entries[eventIDs.SecondInterval])
	}
	if projectedSegments[eventIDs.FirstInterval] != 1 || projectedSegments[eventIDs.SecondInterval] != 2 {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("Core corpus explanation segment counts do not match Ledger: first=%d second=%d", projectedSegments[eventIDs.FirstInterval], projectedSegments[eventIDs.SecondInterval])
	}
	corpusTotals := byteTotals{
		RawUploaded:       corpusRawUploaded,
		RawDownloaded:     corpusRawDownloaded,
		CreditedUploaded:  corpusCreditedUploaded,
		ChargedDownloaded: corpusChargedDownload,
		EntryCount:        2,
	}
	var coreTotals byteTotals
	err = pool.QueryRow(ctx, `
SELECT raw_uploaded, raw_downloaded, credited_uploaded, charged_downloaded, entry_count
FROM traffic.user_totals
WHERE user_id = $1::uuid`, userID).Scan(
		&coreTotals.RawUploaded,
		&coreTotals.RawDownloaded,
		&coreTotals.CreditedUploaded,
		&coreTotals.ChargedDownloaded,
		&coreTotals.EntryCount,
	)
	if err != nil {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("read Core user traffic totals: %w", err)
	}
	var summed byteTotals
	err = pool.QueryRow(ctx, `
SELECT
    COALESCE(sum(raw_uploaded), 0)::bigint,
    COALESCE(sum(raw_downloaded), 0)::bigint,
    COALESCE(sum(credited_uploaded), 0)::bigint,
    COALESCE(sum(charged_downloaded), 0)::bigint,
    count(*)::bigint
FROM traffic.user_traffic_entries
WHERE user_id = $1::uuid`, userID).Scan(
		&summed.RawUploaded,
		&summed.RawDownloaded,
		&summed.CreditedUploaded,
		&summed.ChargedDownloaded,
		&summed.EntryCount,
	)
	if err != nil {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("sum Core user traffic entries: %w", err)
	}
	if coreTotals != summed {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, fmt.Errorf("Core user totals diverge from immutable entries: totals=%+v entries=%+v", coreTotals, summed)
	}
	if coreTotals.RawUploaded < corpusTotals.RawUploaded || coreTotals.RawDownloaded < corpusTotals.RawDownloaded ||
		coreTotals.CreditedUploaded < corpusTotals.CreditedUploaded || coreTotals.ChargedDownloaded < corpusTotals.ChargedDownloaded ||
		coreTotals.EntryCount < corpusTotals.EntryCount {
		return byteTotals{}, byteTotals{}, coreExplanationEvidence{}, errors.New("Core user totals do not include the complete traffic corpus")
	}
	return corpusTotals, coreTotals, coreExplanationEvidence{
		CompleteEntries: 2, ProjectedSegments: 3,
		FirstProjectedSegments:  projectedSegments[eventIDs.FirstInterval],
		SecondProjectedSegments: projectedSegments[eventIDs.SecondInterval],
	}, nil
}
