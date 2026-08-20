// Package httpserver composes the HTTP Tracker data plane from immutable
// admission views, the raw-query codec and a Swarm Engine port. It deliberately
// has no Core, Vault, PostgreSQL, object-storage or shared-cache client.
package httpserver

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/clientpolicy"
	"github.com/peergo/peergo/services/tracker/internal/control"
	"github.com/peergo/peergo/services/tracker/internal/protocol"
	"github.com/peergo/peergo/services/tracker/internal/ratelimit"
	"github.com/peergo/peergo/services/tracker/internal/runtimepolicy"
	"github.com/peergo/peergo/services/tracker/internal/subjectcontrol"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
)

var ErrConfig = errors.New("Tracker HTTP handler configuration is invalid")

var trackerFailureResults = []struct {
	body   []byte
	result string
}{
	{protocol.EncodeFailure("invalid request"), "invalid_request"},
	{protocol.EncodeFailure("tracker temporarily unavailable"), "temporarily_unavailable"},
	{protocol.EncodeFailure("request rate exceeded"), "rate_limited"},
	{protocol.EncodeFailure("access denied"), "access_denied"},
	{protocol.EncodeFailure("client not allowed"), "client_not_allowed"},
	{protocol.EncodeFailure("download restricted"), "download_restricted"},
	{protocol.EncodeFailure("torrent not registered"), "torrent_not_registered"},
	{protocol.EncodeFailure("tracker busy"), "capacity_exhausted"},
	{protocol.EncodeFailure("scrape disabled"), "scrape_disabled"},
}

type TorrentAdmission interface {
	Ready(time.Time, time.Duration) error
	LookupAdmission([20]byte) (control.Admission, bool)
}

type SubjectAdmission interface {
	Ready(time.Time, time.Duration) error
	LookupAdmission(string) (subjectcontrol.Admission, bool)
}

type SwarmEngine interface {
	Announce(swarm.Request) (swarm.Result, error)
}

type SwarmScraper interface {
	Scrape([20]byte, time.Time) swarm.Stats
}

type RuntimePolicy interface {
	Ready(time.Time, time.Duration) error
	CurrentPolicy() (trackerruntimepolicyv1.Policy, bool)
	CurrentStatus() runtimepolicy.Status
}

type EventAppender interface {
	Append(announceevent.Event) error
	Ready() error
}

type Config struct {
	TorrentSnapshotMaxAge time.Duration
	SubjectSnapshotMaxAge time.Duration
	TrustedProxyCIDRs     []netip.Prefix
	Interval              int
	MinInterval           int
	RuntimePolicy         RuntimePolicy
	RuntimePolicyMaxAge   time.Duration
	Operations            *OperationsConfig
	Observer              RequestObserver
}

// RequestObservation contains only bounded, low-cardinality protocol facts.
// It deliberately excludes passkeys, user/torrent identifiers, addresses,
// peer IDs and raw query strings so production telemetry cannot become a
// second Tracker activity log.
type RequestObservation struct {
	Action        string
	Result        string
	AddressFamily string
	ClientFamily  string
	Event         string
	Duration      time.Duration
}

type RequestObserver interface {
	ObserveRequest(RequestObservation)
}

// OperationsConfig enables the service-authenticated, read-only runtime view.
// It is optional for isolated protocol tests, but the real Tracker process
// always supplies it after validating the deployment secret.
type OperationsConfig struct {
	ServiceToken string
	Runtime      trackeroperationsv1.Runtime
}

type Handler struct {
	torrents     TorrentAdmission
	subjects     SubjectAdmission
	swarms       SwarmEngine
	events       EventAppender
	eventFactory *announceevent.Factory
	parser       protocol.AnnounceParser
	policy       RuntimePolicy
	limiter      *ratelimit.Limiter
	observer     RequestObserver
	config       Config
	now          func() time.Time
}

func NewHandler(torrents TorrentAdmission, subjects SubjectAdmission, swarms SwarmEngine, events EventAppender, eventFactory *announceevent.Factory, parser protocol.AnnounceParser, config Config, now func() time.Time) (*Handler, error) {
	if torrents == nil || subjects == nil || swarms == nil || events == nil || eventFactory == nil || parser.MaxNumWant < 1 ||
		config.TorrentSnapshotMaxAge <= 0 || config.SubjectSnapshotMaxAge <= 0 ||
		config.Interval < 60 || config.Interval > 86400 || config.MinInterval < 30 ||
		config.MinInterval > config.Interval || now == nil {
		return nil, ErrConfig
	}
	if !validTrustedProxyCIDRs(config.TrustedProxyCIDRs) {
		return nil, ErrConfig
	}
	config.TrustedProxyCIDRs = append([]netip.Prefix(nil), config.TrustedProxyCIDRs...)
	policy := config.RuntimePolicy
	if policy == nil {
		policy = newStaticRuntimePolicy(parser, config.Interval, config.MinInterval, now().UTC())
		config.RuntimePolicyMaxAge = 24 * time.Hour
	} else if config.RuntimePolicyMaxAge <= 0 {
		return nil, ErrConfig
	}
	limiter, err := ratelimit.New(64, 262_144)
	if err != nil {
		return nil, ErrConfig
	}
	if config.Operations != nil {
		candidate := config.Operations.Runtime
		candidate.GeneratedAt = now().UTC()
		if current, ok := policy.CurrentPolicy(); ok {
			applyRuntimePolicy(&candidate, policy.CurrentStatus(), current)
		}
		if len(config.Operations.ServiceToken) < 32 || !candidate.Valid() {
			return nil, ErrConfig
		}
	}
	return &Handler{
		torrents: torrents, subjects: subjects, swarms: swarms, parser: parser, policy: policy, limiter: limiter,
		events: events, eventFactory: eventFactory, observer: config.Observer, config: config, now: now,
	}, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/health/live":
		handler.writeHealth(response, http.StatusOK, "live\n")
		return
	case "/health/ready":
		now := handler.now().UTC()
		if handler.torrents.Ready(now, handler.config.TorrentSnapshotMaxAge) != nil ||
			handler.subjects.Ready(now, handler.config.SubjectSnapshotMaxAge) != nil ||
			handler.policy.Ready(now, handler.config.RuntimePolicyMaxAge) != nil || handler.events.Ready() != nil {
			handler.writeHealth(response, http.StatusServiceUnavailable, "not ready\n")
			return
		}
		handler.writeHealth(response, http.StatusOK, "ready\n")
		return
	case "/internal/v1/operations/runtime":
		handler.serveOperationsRuntime(response, request)
		return
	}
	startedAt := time.Now()
	observation := RequestObservation{
		Action: "unknown", Result: "failure_other", AddressFamily: "unknown",
		ClientFamily: "unknown", Event: "not_applicable",
	}
	capture := &trackerResponseCapture{ResponseWriter: response}
	handler.serveTracker(capture, request, &observation)
	observation.Duration = time.Since(startedAt)
	observation.Result = trackerResult(capture.body)
	if handler.observer != nil {
		handler.observer.ObserveRequest(observation)
	}
}

func (handler *Handler) serveOperationsRuntime(response http.ResponseWriter, request *http.Request) {
	operations := handler.config.Operations
	if operations == nil || request.Method != http.MethodGet || request.ContentLength > 0 || len(request.TransferEncoding) != 0 {
		http.NotFound(response, request)
		return
	}
	expected := []byte("Bearer " + operations.ServiceToken)
	actual := []byte(request.Header.Get("Authorization"))
	if len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
		handler.writeJSON(response, http.StatusForbidden, map[string]string{"code": "service_auth_failed"})
		return
	}
	runtime := operations.Runtime
	runtime.GeneratedAt = handler.now().UTC().Round(0)
	if policy, ok := handler.policy.CurrentPolicy(); ok {
		applyRuntimePolicy(&runtime, handler.policy.CurrentStatus(), policy)
	}
	handler.writeJSON(response, http.StatusOK, runtime)
}

func (handler *Handler) serveTracker(response http.ResponseWriter, request *http.Request, observation *RequestObservation) {
	passkey, action, ok := trackerRoute(request)
	if !ok || request.Method != http.MethodGet || request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		handler.writeTracker(response, protocol.EncodeFailure("invalid request"))
		return
	}
	observation.Action = action
	now := handler.now().UTC()
	if handler.torrents.Ready(now, handler.config.TorrentSnapshotMaxAge) != nil ||
		handler.subjects.Ready(now, handler.config.SubjectSnapshotMaxAge) != nil ||
		handler.policy.Ready(now, handler.config.RuntimePolicyMaxAge) != nil || handler.events.Ready() != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	policy, ok := handler.policy.CurrentPolicy()
	if !ok {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	address, ok := clientAddress(request, handler.config.TrustedProxyCIDRs)
	if !ok {
		handler.writeTracker(response, protocol.EncodeFailure("invalid request"))
		return
	}
	if address.Is4() {
		observation.AddressFamily = "ipv4"
	} else {
		observation.AddressFamily = "ipv6"
	}
	if !handler.limiter.AllowAddress(address.String(), policy.AddressRequestsPerMinute, policy.AddressBurst, now) {
		handler.writeTracker(response, protocol.EncodeFailure("request rate exceeded"))
		return
	}
	subjectAdmission, found := handler.subjects.LookupAdmission(passkey)
	if !found {
		handler.writeTracker(response, protocol.EncodeFailure("access denied"))
		return
	}
	if !handler.limiter.AllowUser(subjectAdmission.Subject.UserID, policy.UserRequestsPerMinute, policy.UserBurst, now) {
		handler.writeTracker(response, protocol.EncodeFailure("request rate exceeded"))
		return
	}
	if action == "scrape" {
		handler.serveScrape(response, request, policy, now)
		return
	}
	parser, err := protocol.NewAnnounceParser(policy.DefaultNumWant, policy.MaxNumWant)
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	announce, err := parser.Parse(request.URL.RawQuery)
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("invalid request"))
		return
	}
	observation.Event = announceEventLabel(announce.Event)
	if identity, identified := clientpolicy.Identify(announce.PeerID); identified {
		observation.ClientFamily = string(identity.Family)
	}
	if !clientpolicy.Allowed(policy, announce.PeerID) {
		handler.writeTracker(response, protocol.EncodeFailure("client not allowed"))
		return
	}
	// A ratio-restricted user must be able to keep seeding and recover. Block
	// only announces that still have bytes left; stopped is accepted so stale
	// swarm state can be removed even if the client reports an old left value.
	if subjectAdmission.Subject.DownloadRestricted && announce.Left > 0 && announce.Event != protocol.EventStopped {
		handler.writeTracker(response, protocol.EncodeFailure("download restricted"))
		return
	}
	torrentAdmission, found := handler.torrents.LookupAdmission(announce.InfoHash)
	if !found {
		handler.writeTracker(response, protocol.EncodeFailure("torrent not registered"))
		return
	}
	if torrentAdmission.Torrent.TorrentID < 1 || torrentAdmission.ControlSequence < 1 ||
		subjectAdmission.Subject.CredentialVersion < 1 || subjectAdmission.ControlSequence < 1 {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	result, err := handler.swarms.Announce(swarm.Request{
		InfoHash: announce.InfoHash, UserID: subjectAdmission.Subject.UserID, PeerID: announce.PeerID,
		Key: announce.Key, Endpoint: netip.AddrPortFrom(address, announce.Port),
		Left: announce.Left, Downloaded: announce.Downloaded, Event: announce.Event, NumWant: announce.NumWant, Now: now,
	})
	if errors.Is(err, swarm.ErrCapacity) {
		handler.writeTracker(response, protocol.EncodeFailure("tracker busy"))
		return
	}
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	encoded, err := protocol.EncodeAnnounce(protocol.AnnounceResponse{
		Interval: policy.AnnounceIntervalSeconds, MinInterval: policy.MinAnnounceIntervalSeconds,
		Complete: result.Complete, Incomplete: result.Incomplete, Peers: result.Peers,
	}, announce.Compact)
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	addressFamily := 6
	if address.Is4() {
		addressFamily = 4
	}
	classification, err := policy.ClassifyAddressForUser(address, subjectAdmission.Subject.NumericUserID)
	status := handler.policy.CurrentStatus()
	if err != nil || status.ControlSequence < 1 || status.Revision != policy.Revision {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	networkClass := trackerannouncev1.NetworkClassStandard
	if classification.Seedbox {
		networkClass = trackerannouncev1.NetworkClassSeedbox
	}
	downloadFactor := int64(classification.DownloadFactorBasisPoints)
	networkEvidence := &trackerannouncev1.NetworkEvidence{
		PolicySequence: status.ControlSequence, PolicyRevision: status.Revision,
		Class: networkClass, RuleID: classification.RuleID,
		UploadFactorBasisPoints:   int64(classification.UploadFactorBasisPoints),
		DownloadFactorBasisPoints: &downloadFactor,
		SpeedLimitBytesPerSecond:  classification.SpeedLimitBytesPerSecond,
	}
	event, err := handler.eventFactory.New(announceevent.Input{
		ReceivedAt: now, UserID: subjectAdmission.Subject.UserID,
		TorrentID: torrentAdmission.Torrent.TorrentID, InfoHash: announce.InfoHash,
		PeerID: announce.PeerID, Key: announce.Key, AddressFamily: addressFamily,
		Event: announce.Event, Uploaded: announce.Uploaded, Downloaded: announce.Downloaded, Left: announce.Left,
		CredentialVersion:      subjectAdmission.Subject.CredentialVersion,
		TorrentControlSequence: torrentAdmission.ControlSequence,
		SubjectControlSequence: subjectAdmission.ControlSequence,
		CompletionToken:        result.CompletionToken,
		NetworkEvidence:        networkEvidence,
	})
	if err != nil || handler.events.Append(event) != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	handler.writeTracker(response, encoded)
}

type trackerResponseCapture struct {
	http.ResponseWriter
	body []byte
}

func (capture *trackerResponseCapture) Write(body []byte) (int, error) {
	const maximumCapturedResponseBytes = 256
	remaining := maximumCapturedResponseBytes - len(capture.body)
	if remaining > 0 {
		capture.body = append(capture.body, body[:min(len(body), remaining)]...)
	}
	return capture.ResponseWriter.Write(body)
}

func trackerResult(body []byte) string {
	for _, failure := range trackerFailureResults {
		if bytes.Equal(body, failure.body) {
			return failure.result
		}
	}
	if bytes.Contains(body, []byte("14:failure reason")) {
		return "failure_other"
	}
	return "ok"
}

func announceEventLabel(event protocol.Event) string {
	switch event {
	case protocol.EventNone:
		return "regular"
	case protocol.EventStarted:
		return "started"
	case protocol.EventStopped:
		return "stopped"
	case protocol.EventCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

func (handler *Handler) serveScrape(response http.ResponseWriter, request *http.Request, policy trackerruntimepolicyv1.Policy, now time.Time) {
	if !policy.ScrapeEnabled {
		handler.writeTracker(response, protocol.EncodeFailure("scrape disabled"))
		return
	}
	parser, err := protocol.NewScrapeParser(policy.MaxScrapeHashes)
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	hashes, err := parser.Parse(request.URL.RawQuery)
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("invalid request"))
		return
	}
	stats := make([]protocol.ScrapeStat, 0, len(hashes))
	for _, hash := range hashes {
		admission, found := handler.torrents.LookupAdmission(hash)
		if !found {
			handler.writeTracker(response, protocol.EncodeFailure("torrent not registered"))
			return
		}
		scraper, ok := handler.swarms.(SwarmScraper)
		if !ok {
			handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
			return
		}
		active := scraper.Scrape(hash, now)
		stats = append(stats, protocol.ScrapeStat{
			InfoHash: hash, Complete: active.Complete, Incomplete: active.Incomplete,
			Downloaded: admission.Torrent.CompletedDownloads,
		})
	}
	encoded, err := protocol.EncodeScrape(stats)
	if err != nil {
		handler.writeTracker(response, protocol.EncodeFailure("tracker temporarily unavailable"))
		return
	}
	handler.writeTracker(response, encoded)
}

func trackerRoute(request *http.Request) (string, string, bool) {
	if request.URL.RawPath != "" || request.URL.EscapedPath() != request.URL.Path {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "tracker" || (parts[2] != "announce" && parts[2] != "scrape") || parts[1] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

type staticRuntimePolicy struct {
	policy trackerruntimepolicyv1.Policy
	status runtimepolicy.Status
}

func newStaticRuntimePolicy(parser protocol.AnnounceParser, interval, minInterval int, generatedAt time.Time) *staticRuntimePolicy {
	return &staticRuntimePolicy{
		policy: trackerruntimepolicyv1.Policy{
			Revision: "deployment-fallback", AnnounceIntervalSeconds: interval, MinAnnounceIntervalSeconds: minInterval,
			DefaultNumWant: parser.DefaultNumWant, MaxNumWant: parser.MaxNumWant,
			ScrapeEnabled: false, MaxScrapeHashes: 50, ClientMode: trackerruntimepolicyv1.ClientModeAllowAll,
			AllowedClients: []trackerruntimepolicyv1.ClientRule{}, UserRequestsPerMinute: 600, UserBurst: 1200,
			AddressRequestsPerMinute: 5000, AddressBurst: 10000,
			Seedbox: trackerruntimepolicyv1.SeedboxPolicy{
				UploadFactorBasisPoints: 5_000, DownloadFactorBasisPoints: 10_000,
				Rules: []trackerruntimepolicyv1.SeedboxRule{},
			},
		},
		status: runtimepolicy.Status{Loaded: true, ControlSequence: 1, Revision: "deployment-fallback", GeneratedAt: generatedAt},
	}
}

func (policy *staticRuntimePolicy) Ready(time.Time, time.Duration) error { return nil }
func (policy *staticRuntimePolicy) CurrentPolicy() (trackerruntimepolicyv1.Policy, bool) {
	return policy.policy, true
}
func (policy *staticRuntimePolicy) CurrentStatus() runtimepolicy.Status { return policy.status }

func applyRuntimePolicy(runtime *trackeroperationsv1.Runtime, status runtimepolicy.Status, policy trackerruntimepolicyv1.Policy) {
	runtime.PolicyGeneratedAt = status.GeneratedAt
	runtime.PolicyControlSequence = status.ControlSequence
	runtime.PolicyRevision = policy.Revision
	runtime.AnnounceIntervalSeconds = int64(policy.AnnounceIntervalSeconds)
	runtime.MinAnnounceIntervalSeconds = int64(policy.MinAnnounceIntervalSeconds)
	runtime.DefaultNumWant = int64(policy.DefaultNumWant)
	runtime.MaxNumWant = int64(policy.MaxNumWant)
	runtime.ScrapeEnabled = policy.ScrapeEnabled
	runtime.MaxScrapeHashes = int64(policy.MaxScrapeHashes)
	runtime.ClientMode = string(policy.ClientMode)
	runtime.AllowedClients = append([]trackerruntimepolicyv1.ClientRule(nil), policy.AllowedClients...)
	runtime.UserRequestsPerMinute = int64(policy.UserRequestsPerMinute)
	runtime.UserBurst = int64(policy.UserBurst)
	runtime.AddressRequestsPerMinute = int64(policy.AddressRequestsPerMinute)
	runtime.AddressBurst = int64(policy.AddressBurst)
	runtime.Seedbox = policy.Seedbox
}

func remoteAddress(remote string) (netip.Addr, bool) {
	endpoint, err := netip.ParseAddrPort(remote)
	if err != nil {
		return netip.Addr{}, false
	}
	address := endpoint.Addr().Unmap()
	if address.Zone() != "" || (!address.Is4() && !address.Is6()) {
		return netip.Addr{}, false
	}
	return address, true
}

// clientAddress trusts X-Forwarded-For only when the immediate TCP peer is an
// explicitly configured reverse proxy. The proxy must overwrite the header
// with one address; accepting caller-supplied chains would let public clients
// choose their rate-limit key, swarm endpoint and seedbox classification.
func clientAddress(request *http.Request, trustedProxyCIDRs []netip.Prefix) (netip.Addr, bool) {
	peer, ok := remoteAddress(request.RemoteAddr)
	if !ok {
		return netip.Addr{}, false
	}
	trusted := false
	for _, prefix := range trustedProxyCIDRs {
		if prefix.Contains(peer) {
			trusted = true
			break
		}
	}
	if !trusted {
		return peer, true
	}
	values := request.Header.Values("X-Forwarded-For")
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	raw := strings.TrimSpace(values[0])
	if raw == "" || strings.Contains(raw, ",") {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	if address.Zone() != "" || (!address.Is4() && !address.Is6()) {
		return netip.Addr{}, false
	}
	return address, true
}

func validTrustedProxyCIDRs(prefixes []netip.Prefix) bool {
	if len(prefixes) > 16 {
		return false
	}
	for index, prefix := range prefixes {
		if !prefix.IsValid() || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
			return false
		}
		for _, existing := range prefixes[:index] {
			if existing.Contains(prefix.Addr()) || prefix.Contains(existing.Addr()) {
				return false
			}
		}
	}
	return true
}

func (handler *Handler) writeTracker(response http.ResponseWriter, body []byte) {
	response.Header().Set("Content-Type", "text/plain")
	response.Header().Set("Cache-Control", "private, no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Length", strconv.Itoa(len(body)))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(body)
}

func (handler *Handler) writeHealth(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Length", strconv.Itoa(len(body)))
	response.WriteHeader(status)
	_, _ = response.Write([]byte(body))
}

func (handler *Handler) writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
