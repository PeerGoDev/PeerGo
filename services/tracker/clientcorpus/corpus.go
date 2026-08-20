// Package clientcorpus runs a development-only, black-box announce corpus
// through PeerGo's real HTTP handler, signed admission stores, PGW1 WAL and
// ordered JetStream publisher. It is intentionally outside the Tracker
// runtime composition and must never be enabled outside a loopback
// development environment.
package clientcorpus

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/announcepublisher"
	"github.com/peergo/peergo/services/tracker/internal/control"
	"github.com/peergo/peergo/services/tracker/internal/httpserver"
	"github.com/peergo/peergo/services/tracker/internal/jetstreampublisher"
	"github.com/peergo/peergo/services/tracker/internal/protocol"
	"github.com/peergo/peergo/services/tracker/internal/subjectcontrol"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
	"github.com/peergo/peergo/services/tracker/internal/wal"
)

const (
	corpusPasskey              = "c0dec0dec0dec0dec0dec0dec0dec0de"
	corpusLookupKey            = "peergo-development-client-corpus-lookup-key-v1"
	corpusSigningKeyID         = "client-corpus-v1"
	torrentControlSequence     = int64(701)
	subjectControlSequence     = int64(801)
	credentialVersion          = int64(1)
	defaultCorpusTimeout       = 30 * time.Second
	maximumCorpusTimeout       = 2 * time.Minute
	maximumWALBytes            = int64(1 << 20)
	minimumWALCompactionBytes  = int64(1 << 20)
	announceInterval           = 1800
	minimumAnnounceInterval    = 900
	qBittorrentProfile         = "libtorrent-qbittorrent"
	transmissionProfile        = "transmission"
	malformedDuplicateProfile  = "malformed-duplicate-known-field"
	qBittorrentOfficialSource  = "https://github.com/arvidn/libtorrent/blob/RC_2_0/src/http_tracker_connection.cpp"
	transmissionOfficialSource = "https://github.com/transmission/transmission/blob/main/libtransmission/announcer-http.cc"
)

var (
	baseTime = time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	gib      = int64(1024 * 1024 * 1024)
)

type Fixture struct {
	UserID         string
	TorrentID      int64
	InfoHashV1     string
	TotalSizeBytes int64
}

type Config struct {
	Environment string
	Fixture     Fixture
	NATSURL     string
	Stream      string
	Subject     string
	Timeout     time.Duration
}

type HTTPRequestEvidence struct {
	Profile   string `json:"profile"`
	Lifecycle string `json:"lifecycle"`
	Accepted  bool   `json:"accepted"`
	EventID   string `json:"event_id,omitempty"`
}

type PublishEvidence struct {
	EventID        string `json:"event_id"`
	StreamSequence uint64 `json:"stream_sequence"`
	Duplicate      bool   `json:"duplicate"`
}

type WALEvidence struct {
	RecordCount              int  `json:"record_count"`
	CanonicalPayloadsMatched bool `json:"canonical_payloads_matched"`
	CheckpointCaughtUp       bool `json:"checkpoint_caught_up"`
	SensitiveValuesAbsent    bool `json:"sensitive_values_absent"`
}

type EventIDs struct {
	Baseline       string `json:"baseline"`
	FirstInterval  string `json:"first_interval"`
	SecondInterval string `json:"second_interval"`
	BaselineOnly   string `json:"baseline_only"`
}

type Result struct {
	Requests               []HTTPRequestEvidence `json:"requests"`
	Published              []PublishEvidence     `json:"published"`
	WAL                    WALEvidence           `json:"wal"`
	EventIDs               EventIDs              `json:"event_ids"`
	TorrentControlSequence int64                 `json:"torrent_control_sequence"`
	SubjectControlSequence int64                 `json:"subject_control_sequence"`
	ClientProfileSources   map[string]string     `json:"client_profile_sources"`
}

type evidencePublisher interface {
	PublishWithEvidence(context.Context, string, []byte) (jetstreampublisher.PublishEvidence, error)
}

// Run connects only to a loopback NATS endpoint. PostgreSQL verification stays
// in tools/traffic-corpus, so the Tracker corpus package remains independent of
// Core and Settlement storage schemas.
func Run(ctx context.Context, config Config) (Result, error) {
	if err := validateConfig(config); err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, corpusTimeout(config.Timeout))
	defer cancel()
	connection, jetStream, err := jetstreampublisher.Connect(jetstreampublisher.ConnectionConfig{
		URLs: []string{config.NATSURL}, ConnectTimeout: 2 * time.Second, ReconnectWait: 100 * time.Millisecond,
	}, nil)
	if err != nil {
		return Result{}, fmt.Errorf("connect client corpus publisher: %w", err)
	}
	defer connection.Close()
	sink, err := jetstreampublisher.NewSink(jetStream, config.Stream, config.Subject)
	if err != nil {
		return Result{}, fmt.Errorf("compose client corpus publisher: %w", err)
	}
	return runWithPublisher(runCtx, config, sink)
}

func runWithPublisher(ctx context.Context, config Config, sink evidencePublisher) (Result, error) {
	if err := validateConfig(config); err != nil || sink == nil {
		if err != nil {
			return Result{}, err
		}
		return Result{}, errors.New("client corpus publisher is required")
	}
	torrentStore, subjectStore, infoHash, err := admissionStores(config.Fixture)
	if err != nil {
		return Result{}, err
	}
	parser, err := protocol.NewAnnounceParser(50, 100)
	if err != nil {
		return Result{}, err
	}
	engine, err := swarm.NewEngine(swarm.Config{
		ShardCount: 4, MaxSwarms: 32, MaxPeers: 128, MaxPeersPerSwarm: 32,
		PeerTTL: time.Hour, SweepBudget: 32,
	})
	if err != nil {
		return Result{}, fmt.Errorf("compose client corpus swarm: %w", err)
	}
	directory, err := os.MkdirTemp("", "peergo-tracker-client-corpus-*")
	if err != nil {
		return Result{}, fmt.Errorf("create client corpus WAL directory: %w", err)
	}
	defer os.RemoveAll(directory)
	if err := os.Chmod(directory, 0o700); err != nil {
		return Result{}, fmt.Errorf("protect client corpus WAL directory: %w", err)
	}
	walPath := filepath.Join(directory, "announce.wal")
	eventLog, err := wal.OpenFile(walPath, maximumWALBytes)
	if err != nil {
		return Result{}, fmt.Errorf("open client corpus WAL: %w", err)
	}
	defer eventLog.Close()
	appender := &recordingAppender{delegate: eventLog}
	clock := &manualClock{current: baseTime}
	factory := announceevent.NewFactory(bytes.NewReader(deterministicUUIDEntropy()))
	handler, err := httpserver.NewHandler(
		torrentStore, subjectStore, engine, appender, factory, parser,
		httpserver.Config{
			TorrentSnapshotMaxAge: 2 * time.Hour, SubjectSnapshotMaxAge: 2 * time.Hour,
			Interval: announceInterval, MinInterval: minimumAnnounceInterval,
		},
		clock.Now,
	)
	if err != nil {
		return Result{}, fmt.Errorf("compose client corpus HTTP Tracker: %w", err)
	}
	recordedSink := newRecordingSink(sink)
	publisher, err := announcepublisher.New(eventLog, recordedSink, announcepublisher.Config{
		PublishTimeout: 2 * time.Second, RetryMinimum: 10 * time.Millisecond,
		RetryMaximum: 100 * time.Millisecond, CompactAtBytes: minimumWALCompactionBytes,
	}, nil)
	if err != nil {
		return Result{}, fmt.Errorf("compose client corpus WAL publisher: %w", err)
	}
	publisherCtx, stopPublisher := context.WithCancel(ctx)
	publisherResult := make(chan error, 1)
	go func() { publisherResult <- publisher.Run(publisherCtx) }()
	publisherStopped := false
	defer func() {
		if publisherStopped {
			return
		}
		stopPublisher()
		<-publisherResult
	}()
	server := httptest.NewServer(handler)
	defer server.Close()

	requests := clientRequests(infoHash)
	requestEvidence := make([]HTTPRequestEvidence, 0, len(requests)+1)
	client := &http.Client{Timeout: 3 * time.Second}
	for _, request := range requests {
		clock.Set(request.At)
		before := appender.Count()
		if err := sendAnnounce(ctx, client, server.URL, request, false); err != nil {
			return Result{}, err
		}
		events := appender.Events()
		if len(events) != before+1 {
			return Result{}, fmt.Errorf("client profile %s did not append exactly one WAL event", request.Profile)
		}
		requestEvidence = append(requestEvidence, HTTPRequestEvidence{
			Profile: request.Profile, Lifecycle: request.EventLabel, Accepted: true, EventID: events[len(events)-1].EventID,
		})
	}
	clock.Set(baseTime.Add(30 * time.Minute))
	beforeMalformed := appender.Count()
	if err := sendAnnounce(ctx, client, server.URL, malformedRequest(infoHash), true); err != nil {
		return Result{}, err
	}
	if appender.Count() != beforeMalformed {
		return Result{}, errors.New("malformed client request reached the durable WAL")
	}
	requestEvidence = append(requestEvidence, HTTPRequestEvidence{
		Profile: malformedDuplicateProfile, Lifecycle: "rejected", Accepted: false,
	})

	if err := recordedSink.Wait(ctx, len(requests)); err != nil {
		return Result{}, fmt.Errorf("wait for client corpus WAL publisher: %w", err)
	}
	stopPublisher()
	select {
	case err := <-publisherResult:
		publisherStopped = true
		if err != nil {
			return Result{}, fmt.Errorf("run client corpus WAL publisher: %w", err)
		}
	case <-ctx.Done():
		return Result{}, fmt.Errorf("stop client corpus WAL publisher: %w", ctx.Err())
	}

	events := appender.Events()
	published := recordedSink.Records()
	if err := verifyEventLifecycle(events, config.Fixture, infoHash); err != nil {
		return Result{}, err
	}
	canonicalMatch, err := publishedMatchesEvents(published, events)
	if err != nil {
		return Result{}, err
	}
	pendingBytes, err := eventLog.PendingBytes()
	if err != nil {
		return Result{}, fmt.Errorf("inspect client corpus WAL checkpoint: %w", err)
	}
	walBytes, err := os.ReadFile(walPath)
	if err != nil {
		return Result{}, fmt.Errorf("read client corpus WAL: %w", err)
	}
	if err := assertWALPrivacy(walBytes, server.URL); err != nil {
		return Result{}, err
	}
	return Result{
		Requests:  requestEvidence,
		Published: publishEvidence(published),
		WAL: WALEvidence{
			RecordCount: len(events), CanonicalPayloadsMatched: canonicalMatch,
			CheckpointCaughtUp: pendingBytes == 0, SensitiveValuesAbsent: true,
		},
		EventIDs: EventIDs{
			Baseline: events[0].EventID, FirstInterval: events[1].EventID,
			SecondInterval: events[2].EventID, BaselineOnly: events[3].EventID,
		},
		TorrentControlSequence: torrentControlSequence,
		SubjectControlSequence: subjectControlSequence,
		ClientProfileSources: map[string]string{
			qBittorrentProfile:  qBittorrentOfficialSource,
			transmissionProfile: transmissionOfficialSource,
		},
	}, nil
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Environment) != "development" || config.Fixture.TorrentID < 1 ||
		config.Fixture.TotalSizeBytes < 1 || config.Fixture.UserID == "" ||
		config.Fixture.InfoHashV1 == "" ||
		!jetstreampublisher.ValidStreamName(config.Stream) || !jetstreampublisher.ValidLiteralSubject(config.Subject) {
		return errors.New("client corpus configuration is invalid")
	}
	if config.Timeout < 0 || config.Timeout > maximumCorpusTimeout {
		return errors.New("client corpus timeout is invalid")
	}
	parsed, err := url.Parse(config.NATSURL)
	if err != nil || parsed.Scheme != "nats" || parsed.Hostname() == "" {
		return errors.New("client corpus NATS URL must use an explicit loopback host")
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (address == nil || !address.IsLoopback()) {
		return errors.New("client corpus NATS URL must be loopback")
	}
	return nil
}

func corpusTimeout(value time.Duration) time.Duration {
	if value == 0 {
		return defaultCorpusTimeout
	}
	return value
}

func admissionStores(source Fixture) (*control.Store, *subjectcontrol.Store, [20]byte, error) {
	infoHash, err := trackercontrolv1.DecodeInfoHash(source.InfoHashV1)
	if err != nil {
		return nil, nil, [20]byte{}, errors.New("client corpus info hash is invalid")
	}
	seed := bytes.Repeat([]byte{0x5c}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trusted := map[string]ed25519.PublicKey{corpusSigningKeyID: publicKey}
	generatedAt := baseTime.Add(-time.Minute)
	torrentArtifact, err := trackercontrolv1.Sign(trackercontrolv1.Snapshot{
		GeneratedAt: generatedAt, ControlSequence: torrentControlSequence,
		Torrents: []trackercontrolv1.Torrent{{
			TorrentID:  source.TorrentID,
			InfoHashV1: source.InfoHashV1, TotalSizeBytes: source.TotalSizeBytes,
			TorrentVersion: 1, ControlSequence: torrentControlSequence,
		}},
	}, corpusSigningKeyID, privateKey)
	if err != nil {
		return nil, nil, [20]byte{}, fmt.Errorf("sign client corpus torrent admission: %w", err)
	}
	lookup, err := trackerpasskeyv1.LookupHMAC([]byte(corpusLookupKey), corpusPasskey)
	if err != nil {
		return nil, nil, [20]byte{}, fmt.Errorf("derive client corpus subject admission: %w", err)
	}
	subjectArtifact, err := trackersubjectcontrolv1.Sign(trackersubjectcontrolv1.Snapshot{
		GeneratedAt: generatedAt, ControlSequence: subjectControlSequence,
		Subjects: []trackersubjectcontrolv1.Subject{{
			UserID: source.UserID, LookupHMAC: hex.EncodeToString(lookup[:]), CredentialVersion: credentialVersion,
		}},
	}, corpusSigningKeyID, privateKey)
	if err != nil {
		return nil, nil, [20]byte{}, fmt.Errorf("sign client corpus subject admission: %w", err)
	}
	torrentStore, err := control.NewStore(trusted, 0)
	if err != nil {
		return nil, nil, [20]byte{}, err
	}
	if _, err := torrentStore.LoadArtifact(torrentArtifact.Bytes, baseTime); err != nil {
		return nil, nil, [20]byte{}, fmt.Errorf("load client corpus torrent admission: %w", err)
	}
	subjectStore, err := subjectcontrol.NewStore(trusted, []byte(corpusLookupKey), 0)
	if err != nil {
		return nil, nil, [20]byte{}, err
	}
	if _, err := subjectStore.LoadArtifact(subjectArtifact.Bytes, baseTime); err != nil {
		return nil, nil, [20]byte{}, fmt.Errorf("load client corpus subject admission: %w", err)
	}
	return torrentStore, subjectStore, infoHash, nil
}

type clientRequest struct {
	Profile    string
	EventLabel string
	At         time.Time
	PeerID     [20]byte
	Port       int
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      string
	Key        string
	UserAgent  string
	Extra      []queryField
	InfoHash   [20]byte
}

type queryField struct {
	Name  string
	Value string
}

func clientRequests(infoHash [20]byte) []clientRequest {
	qBPeer := peerID("-qB5000-+PGCORPUS001")
	transmissionPeer := peerID("-TR4000-XPGCORPUS002")
	qBExtra := []queryField{
		{Name: "corrupt", Value: "0"}, {Name: "numwant", Value: "200"},
		{Name: "compact", Value: "1"}, {Name: "no_peer_id", Value: "1"},
		{Name: "supportcrypto", Value: "1"}, {Name: "redundant", Value: "0"},
		{Name: "ipv4", Value: "198.51.100.20"}, {Name: "ipv6", Value: "2001:db8::20"},
	}
	return []clientRequest{
		{Profile: qBittorrentProfile, EventLabel: "started", At: baseTime, PeerID: qBPeer, Port: 51413, Uploaded: 0, Downloaded: 0, Left: 3 * gib, Event: "started", Key: "A1B2C3D4", UserAgent: "qBittorrent/5.x", Extra: qBExtra, InfoHash: infoHash},
		{Profile: qBittorrentProfile, EventLabel: "periodic", At: baseTime.Add(10 * time.Minute), PeerID: qBPeer, Port: 51413, Uploaded: gib, Downloaded: 2 * gib, Left: gib, Key: "A1B2C3D4", UserAgent: "qBittorrent/5.x", Extra: qBExtra, InfoHash: infoHash},
		{Profile: qBittorrentProfile, EventLabel: "completed", At: baseTime.Add(20 * time.Minute), PeerID: qBPeer, Port: 51413, Uploaded: 3 * gib, Downloaded: 3 * gib, Left: 0, Event: "completed", Key: "A1B2C3D4", UserAgent: "qBittorrent/5.x", Extra: qBExtra, InfoHash: infoHash},
		{Profile: transmissionProfile, EventLabel: "started-baseline", At: baseTime.Add(25 * time.Minute), PeerID: transmissionPeer, Port: 51414, Uploaded: 0, Downloaded: 0, Left: 3 * gib, Event: "started", Key: "0BADF00D", UserAgent: "Transmission/4.x", Extra: []queryField{
			{Name: "numwant", Value: "80"}, {Name: "compact", Value: "1"},
			{Name: "supportcrypto", Value: "1"}, {Name: "ipv4", Value: "203.0.113.40"},
			{Name: "ipv6", Value: "2001:db8::40"},
		}, InfoHash: infoHash},
	}
}

func malformedRequest(infoHash [20]byte) clientRequest {
	request := clientRequests(infoHash)[0]
	request.Profile = malformedDuplicateProfile
	request.EventLabel = "rejected"
	request.Extra = append(request.Extra, queryField{Name: "info_hash", Value: escapeBytes(infoHash[:])})
	return request
}

func sendAnnounce(ctx context.Context, client *http.Client, serverURL string, request clientRequest, expectFailure bool) error {
	target := serverURL + "/tracker/" + corpusPasskey + "/announce?" + request.RawQuery()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build %s announce: %w", request.Profile, err)
	}
	httpRequest.Header.Set("User-Agent", request.UserAgent)
	httpRequest.Header.Set("Accept-Encoding", "gzip")
	response, err := client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("send %s announce: %w", request.Profile, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("read %s announce response: %w", request.Profile, err)
	}
	failure := bytes.Contains(body, []byte("14:failure reason"))
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/plain" ||
		response.Header.Get("Cache-Control") != "private, no-store" ||
		response.Header.Get("X-Content-Type-Options") != "nosniff" || !bytes.HasPrefix(body, []byte("d")) ||
		!bytes.HasSuffix(body, []byte("e")) || failure != expectFailure {
		return fmt.Errorf("%s announce returned invalid Tracker response status=%d body=%q", request.Profile, response.StatusCode, body)
	}
	if !expectFailure && (!bytes.Contains(body, []byte("8:intervali1800e")) || !bytes.Contains(body, []byte("12:min intervali900e"))) {
		return fmt.Errorf("%s announce response omitted interval contract", request.Profile)
	}
	return nil
}

func (request clientRequest) RawQuery() string {
	fields := []queryField{
		{Name: "info_hash", Value: escapeBytes(request.InfoHash[:])},
		{Name: "peer_id", Value: escapeBytes(request.PeerID[:])},
		{Name: "port", Value: strconv.Itoa(request.Port)},
		{Name: "uploaded", Value: strconv.FormatInt(request.Uploaded, 10)},
		{Name: "downloaded", Value: strconv.FormatInt(request.Downloaded, 10)},
		{Name: "left", Value: strconv.FormatInt(request.Left, 10)},
		{Name: "key", Value: request.Key},
	}
	if request.Event != "" {
		fields = append(fields, queryField{Name: "event", Value: request.Event})
	}
	fields = append(fields, request.Extra...)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, field.Name+"="+field.Value)
	}
	return strings.Join(parts, "&")
}

func escapeBytes(value []byte) string {
	const hexadecimal = "0123456789ABCDEF"
	var result strings.Builder
	result.Grow(len(value) * 3)
	for _, current := range value {
		if (current >= 'a' && current <= 'z') || (current >= 'A' && current <= 'Z') ||
			(current >= '0' && current <= '9') || current == '-' || current == '_' || current == '.' || current == '~' {
			result.WriteByte(current)
			continue
		}
		result.WriteByte('%')
		result.WriteByte(hexadecimal[current>>4])
		result.WriteByte(hexadecimal[current&0x0f])
	}
	return result.String()
}

func peerID(value string) [20]byte {
	var result [20]byte
	copy(result[:], value)
	return result
}

func deterministicUUIDEntropy() []byte {
	result := make([]byte, 0, 4*16)
	for _, value := range []byte{0x31, 0x42, 0x53, 0x64} {
		result = append(result, bytes.Repeat([]byte{value}, 16)...)
	}
	return result
}

func verifyEventLifecycle(events []trackerannouncev1.Event, source Fixture, infoHash [20]byte) error {
	if len(events) != 4 {
		return fmt.Errorf("client corpus emitted %d events, want 4", len(events))
	}
	wantEvents := []string{"started", "", "completed", "started"}
	wantUploaded := []int64{0, gib, 3 * gib, 0}
	wantDownloaded := []int64{0, 2 * gib, 3 * gib, 0}
	wantLeft := []int64{3 * gib, gib, 0, 3 * gib}
	for index, event := range events {
		if event.UserID != source.UserID || event.TorrentID != source.TorrentID ||
			event.InfoHashV1 != hex.EncodeToString(infoHash[:]) || event.AddressFamily != 4 ||
			event.Event != wantEvents[index] || event.Uploaded != wantUploaded[index] ||
			event.Downloaded != wantDownloaded[index] || event.Left != wantLeft[index] ||
			event.CredentialVersion != credentialVersion ||
			event.TorrentControlSequence != torrentControlSequence ||
			event.SubjectControlSequence != subjectControlSequence {
			return fmt.Errorf("client corpus event %d violates canonical lifecycle: %+v", index, event)
		}
	}
	if events[0].SessionToken == "" || events[0].SessionToken != events[1].SessionToken ||
		events[1].SessionToken != events[2].SessionToken || events[2].SessionToken == events[3].SessionToken {
		return errors.New("client corpus session tokens do not preserve the client lifecycle boundary")
	}
	return nil
}

func publishedMatchesEvents(published []recordedPublish, events []trackerannouncev1.Event) (bool, error) {
	if len(published) != len(events) {
		return false, fmt.Errorf("published client corpus count=%d events=%d", len(published), len(events))
	}
	for index, event := range events {
		encoded, err := announceevent.Encode(event)
		if err != nil {
			return false, err
		}
		if published[index].EventID != event.EventID || !bytes.Equal(published[index].Payload, encoded) {
			return false, fmt.Errorf("WAL publisher payload %d diverges from HTTP event", index)
		}
	}
	return true, nil
}

func publishEvidence(records []recordedPublish) []PublishEvidence {
	result := make([]PublishEvidence, 0, len(records))
	for _, record := range records {
		result = append(result, PublishEvidence{
			EventID: record.EventID, StreamSequence: record.Evidence.Sequence, Duplicate: record.Evidence.Duplicate,
		})
	}
	return result
}

func assertWALPrivacy(encoded []byte, serverURL string) error {
	for label, forbidden := range map[string]string{
		"route passkey":           corpusPasskey,
		"qBittorrent peer ID":     "-qB5000-+PGCORPUS001",
		"Transmission peer ID":    "-TR4000-XPGCORPUS002",
		"qBittorrent key":         "A1B2C3D4",
		"Transmission key":        "0BADF00D",
		"claimed IPv4":            "198.51.100.20",
		"claimed IPv6":            "2001:db8::20",
		"qBittorrent user agent":  "qBittorrent/5.x",
		"Transmission user agent": "Transmission/4.x",
		"full Tracker origin":     serverURL,
	} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			return fmt.Errorf("client corpus WAL leaked %s", label)
		}
	}
	return nil
}

type manualClock struct {
	mu      sync.RWMutex
	current time.Time
}

func (clock *manualClock) Set(value time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.current = value
}

func (clock *manualClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.current
}

type recordingAppender struct {
	mu       sync.Mutex
	delegate *wal.File
	events   []trackerannouncev1.Event
}

func (appender *recordingAppender) Append(event announceevent.Event) error {
	if err := appender.delegate.Append(event); err != nil {
		return err
	}
	appender.mu.Lock()
	defer appender.mu.Unlock()
	appender.events = append(appender.events, event)
	return nil
}

func (appender *recordingAppender) Ready() error { return appender.delegate.Ready() }

func (appender *recordingAppender) Count() int {
	appender.mu.Lock()
	defer appender.mu.Unlock()
	return len(appender.events)
}

func (appender *recordingAppender) Events() []trackerannouncev1.Event {
	appender.mu.Lock()
	defer appender.mu.Unlock()
	return append([]trackerannouncev1.Event(nil), appender.events...)
}

type recordedPublish struct {
	EventID  string
	Payload  []byte
	Evidence jetstreampublisher.PublishEvidence
}

type recordingSink struct {
	mu       sync.Mutex
	delegate evidencePublisher
	records  []recordedPublish
	wake     chan struct{}
}

func newRecordingSink(delegate evidencePublisher) *recordingSink {
	return &recordingSink{delegate: delegate, wake: make(chan struct{}, 1)}
}

func (sink *recordingSink) Publish(ctx context.Context, eventID string, payload []byte) error {
	evidence, err := sink.delegate.PublishWithEvidence(ctx, eventID, payload)
	if err != nil {
		return err
	}
	sink.mu.Lock()
	sink.records = append(sink.records, recordedPublish{
		EventID: eventID, Payload: append([]byte(nil), payload...), Evidence: evidence,
	})
	sink.mu.Unlock()
	select {
	case sink.wake <- struct{}{}:
	default:
	}
	return nil
}

func (sink *recordingSink) Wait(ctx context.Context, count int) error {
	for {
		sink.mu.Lock()
		ready := len(sink.records) >= count
		sink.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sink.wake:
		}
	}
}

func (sink *recordingSink) Records() []recordedPublish {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	result := make([]recordedPublish, len(sink.records))
	for index, record := range sink.records {
		result[index] = record
		result[index].Payload = append([]byte(nil), record.Payload...)
	}
	return result
}
