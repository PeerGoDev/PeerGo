package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/control"
	"github.com/peergo/peergo/services/tracker/internal/protocol"
	"github.com/peergo/peergo/services/tracker/internal/subjectcontrol"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
)

type torrentAdmissionFixture struct {
	hash               [20]byte
	ready              bool
	completedDownloads int64
}

func (fixture torrentAdmissionFixture) Ready(time.Time, time.Duration) error {
	if fixture.ready {
		return nil
	}
	return errNotReady
}

func (fixture torrentAdmissionFixture) LookupAdmission(hash [20]byte) (control.Admission, bool) {
	if hash != fixture.hash {
		return control.Admission{}, false
	}
	torrent := trackerTorrent(42)
	torrent.CompletedDownloads = fixture.completedDownloads
	return control.Admission{Torrent: torrent, ControlSequence: 7}, true
}

type subjectAdmissionFixture struct {
	passkey string
	ready   bool
	subject trackersubjectcontrolv1.Subject
}

func (fixture subjectAdmissionFixture) Ready(time.Time, time.Duration) error {
	if fixture.ready {
		return nil
	}
	return errNotReady
}

func (fixture subjectAdmissionFixture) LookupAdmission(passkey string) (subjectcontrol.Admission, bool) {
	if passkey != fixture.passkey {
		return subjectcontrol.Admission{}, false
	}
	subject := fixture.subject
	if subject.UserID == "" {
		subject = trackerSubject()
	}
	return subjectcontrol.Admission{Subject: subject, ControlSequence: 3}, true
}

type eventAppenderFixture struct {
	mu     sync.Mutex
	events []announceevent.Event
}

func (fixture *eventAppenderFixture) Append(event announceevent.Event) error {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.events = append(fixture.events, event)
	return nil
}

func (fixture *eventAppenderFixture) Ready() error { return nil }

type requestObserverFixture struct {
	observations []RequestObservation
}

func (fixture *requestObserverFixture) ObserveRequest(observation RequestObservation) {
	fixture.observations = append(fixture.observations, observation)
}

type failOnceAppender struct {
	mu         sync.Mutex
	attempts   []announceevent.Event
	failOnCall int
}

func (appender *failOnceAppender) Append(event announceevent.Event) error {
	appender.mu.Lock()
	defer appender.mu.Unlock()
	appender.attempts = append(appender.attempts, event)
	if len(appender.attempts) == appender.failOnCall {
		return fixtureError("append failed")
	}
	return nil
}

func (appender *failOnceAppender) Ready() error { return nil }

type fixtureError string

func (err fixtureError) Error() string { return string(err) }

const errNotReady fixtureError = "not ready"

func TestHandlerAnnouncesUsingSocketAddressAndCompactResponse(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	hash := [20]byte{}
	copy(hash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	handler, events := testHandler(t, hash, passkey, true)

	first := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 100), nil)
	first.RemoteAddr = "192.0.2.10:50000"
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK || bytes.Contains(firstRecorder.Body.Bytes(), []byte("failure reason")) {
		t.Fatalf("first response code=%d body=%q", firstRecorder.Code, firstRecorder.Body.Bytes())
	}

	second := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccc", 0)+"&ip=203.0.113.99", nil)
	second.RemoteAddr = "192.0.2.11:50001"
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	body := secondRecorder.Body.Bytes()
	if secondRecorder.Code != http.StatusOK || !bytes.Contains(body, []byte{192, 0, 2, 10, 0x1a, 0xe1}) ||
		bytes.Contains(body, []byte{203, 0, 113, 99}) {
		t.Fatalf("second response code=%d body=%q", secondRecorder.Code, body)
	}
	if secondRecorder.Header().Get("Cache-Control") != "private, no-store" || secondRecorder.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("headers = %v", secondRecorder.Header())
	}
	if len(events.events) != 2 || events.events[1].TorrentControlSequence != 7 || events.events[1].SubjectControlSequence != 3 ||
		events.events[1].AddressFamily != 4 {
		t.Fatalf("events = %+v", events.events)
	}
}

func TestClientAddressUsesOnlySingleHeaderFromTrustedProxy(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{
		netip.MustParsePrefix("172.30.0.1/32"),
		netip.MustParsePrefix("2001:db8:1::1/128"),
	}
	for name, fixture := range map[string]struct {
		remote  string
		headers []string
		want    string
		ok      bool
	}{
		"trusted_ipv4":         {remote: "172.30.0.1:51000", headers: []string{"198.51.100.20"}, want: "198.51.100.20", ok: true},
		"trusted_ipv6":         {remote: "[2001:db8:1::1]:51000", headers: []string{"2001:db8:2::20"}, want: "2001:db8:2::20", ok: true},
		"untrusted_ignores":    {remote: "192.0.2.44:51000", headers: []string{"198.51.100.20"}, want: "192.0.2.44", ok: true},
		"trusted_missing":      {remote: "172.30.0.1:51000", ok: false},
		"trusted_chain":        {remote: "172.30.0.1:51000", headers: []string{"198.51.100.20, 192.0.2.1"}, ok: false},
		"trusted_multi_header": {remote: "172.30.0.1:51000", headers: []string{"198.51.100.20", "192.0.2.1"}, ok: false},
		"trusted_host_port":    {remote: "172.30.0.1:51000", headers: []string{"198.51.100.20:443"}, ok: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/tracker/passkey/announce", nil)
			request.RemoteAddr = fixture.remote
			for _, value := range fixture.headers {
				request.Header.Add("X-Forwarded-For", value)
			}
			address, ok := clientAddress(request, trusted)
			if ok != fixture.ok || (ok && address.String() != fixture.want) {
				t.Fatalf("clientAddress() = %q, %t; want %q, %t", address, ok, fixture.want, fixture.ok)
			}
		})
	}
}

func TestHandlerUsesForwardedClientAddressForSwarmEndpoint(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	var hash [20]byte
	copy(hash[:], "aaaaaaaaaaaaaaaaaaaa")
	handler, _ := testHandler(t, hash, passkey, true)
	handler.config.TrustedProxyCIDRs = []netip.Prefix{netip.MustParsePrefix("172.30.0.1/32")}

	first := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 100), nil)
	first.RemoteAddr = "172.30.0.1:51000"
	first.Header.Set("X-Forwarded-For", "198.51.100.20")
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if bytes.Contains(firstResponse.Body.Bytes(), []byte("failure reason")) {
		t.Fatalf("first response = %q", firstResponse.Body.Bytes())
	}

	second := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccc", 0), nil)
	second.RemoteAddr = "172.30.0.1:51001"
	second.Header.Set("X-Forwarded-For", "198.51.100.21")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if !bytes.Contains(secondResponse.Body.Bytes(), []byte{198, 51, 100, 20, 0x1a, 0xe1}) ||
		bytes.Contains(secondResponse.Body.Bytes(), []byte{172, 30, 0, 1}) {
		t.Fatalf("second response = %q", secondResponse.Body.Bytes())
	}
}

func TestHandlerObservesOnlyBoundedProtocolDimensions(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	var hash [20]byte
	copy(hash[:], "aaaaaaaaaaaaaaaaaaaa")
	handler, _ := testHandler(t, hash, passkey, true)
	observer := &requestObserverFixture{}
	handler.observer = observer

	accepted := httptest.NewRequest(
		http.MethodGet,
		"/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "-qB1234-aaaaaaaaaaaa", 100)+"&event=started",
		nil,
	)
	accepted.RemoteAddr = "192.0.2.10:50000"
	handler.ServeHTTP(httptest.NewRecorder(), accepted)

	denied := httptest.NewRequest(
		http.MethodGet,
		"/tracker/ffffffffffffffffffffffffffffffff/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "-TR4000-aaaaaaaaaaaa", 100),
		nil,
	)
	denied.RemoteAddr = "[2001:db8::10]:50000"
	handler.ServeHTTP(httptest.NewRecorder(), denied)

	if len(observer.observations) != 2 {
		t.Fatalf("observations = %+v", observer.observations)
	}
	first := observer.observations[0]
	if first.Action != "announce" || first.Result != "ok" || first.AddressFamily != "ipv4" ||
		first.ClientFamily != "qbittorrent" || first.Event != "started" || first.Duration <= 0 {
		t.Fatalf("accepted observation = %+v", first)
	}
	second := observer.observations[1]
	if second.Action != "announce" || second.Result != "access_denied" || second.AddressFamily != "ipv6" ||
		second.ClientFamily != "unknown" || second.Event != "not_applicable" || second.Duration <= 0 {
		t.Fatalf("denied observation = %+v", second)
	}
}

func TestHandlerEmitsSeedboxClassificationWithoutSocketAddress(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	var hash [20]byte
	copy(hash[:], "aaaaaaaaaaaaaaaaaaaa")
	handler, events := testHandler(t, hash, passkey, true)
	policy := handler.policy.(*staticRuntimePolicy)
	policy.policy.Seedbox = trackerruntimepolicyv1.SeedboxPolicy{
		Enabled: true, UploadFactorBasisPoints: 5_000,
		SeedboxSpeedLimitBytesPerSecond: 100 << 20,
		Rules:                           []trackerruntimepolicyv1.SeedboxRule{{ID: "trusted-box", CIDR: "192.0.2.0/24"}},
	}

	request := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 100), nil)
	request.RemoteAddr = "192.0.2.10:50000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if bytes.Contains(response.Body.Bytes(), []byte("failure reason")) || len(events.events) != 1 {
		t.Fatalf("response=%q events=%+v", response.Body.Bytes(), events.events)
	}
	evidence := events.events[0].NetworkEvidence
	if evidence == nil || evidence.Class != trackerannouncev1.NetworkClassSeedbox || evidence.RuleID != "trusted-box" ||
		evidence.PolicySequence != 1 || evidence.PolicyRevision != "deployment-fallback" ||
		evidence.UploadFactorBasisPoints != 5_000 || evidence.SpeedLimitBytesPerSecond != 100<<20 {
		t.Fatalf("network evidence = %+v", evidence)
	}
	encoded, err := announceevent.Encode(events.events[0])
	if err != nil || bytes.Contains(encoded, []byte("192.0.2.10")) {
		t.Fatalf("encoded event leaks socket address: %q, %v", encoded, err)
	}
}

func TestHandlerReturnsOnlyBencodedTrackerFailures(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	hash := [20]byte{}
	copy(hash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	handler, _ := testHandler(t, hash, passkey, true)
	for name, target := range map[string]string{
		"denied":       "/tracker/ffffffffffffffffffffffffffffffff/announce?" + announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 1),
		"torrent":      "/tracker/" + passkey + "/announce?" + announceQuery("zzzzzzzzzzzzzzzzzzzz", "bbbbbbbbbbbbbbbbbbbb", 1),
		"malformed":    "/tracker/" + passkey + "/announce?info_hash=short",
		"unknown_path": "/login",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			request.RemoteAddr = "192.0.2.1:50000"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !bytes.HasPrefix(response.Body.Bytes(), []byte("d14:failure reason")) ||
				!bytes.HasSuffix(response.Body.Bytes(), []byte("e")) {
				t.Fatalf("response code=%d body=%q", response.Code, response.Body.Bytes())
			}
		})
	}
}

func TestHandlerDownloadRestrictionBlocksLeechingButKeepsSeedingAvailable(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	hash := [20]byte{}
	copy(hash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	handler, events := testHandler(t, hash, passkey, true)
	restricted := trackerSubject()
	restricted.DownloadRestricted = true
	handler.subjects = subjectAdmissionFixture{passkey: passkey, ready: true, subject: restricted}

	leecher := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 100), nil)
	leecher.RemoteAddr = "192.0.2.10:50000"
	leecherResponse := httptest.NewRecorder()
	handler.ServeHTTP(leecherResponse, leecher)
	if !bytes.Contains(leecherResponse.Body.Bytes(), []byte("download restricted")) || len(events.events) != 0 {
		t.Fatalf("leecher response=%q events=%+v", leecherResponse.Body.Bytes(), events.events)
	}

	seeder := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 0), nil)
	seeder.RemoteAddr = "192.0.2.10:50000"
	seederResponse := httptest.NewRecorder()
	handler.ServeHTTP(seederResponse, seeder)
	if bytes.Contains(seederResponse.Body.Bytes(), []byte("failure reason")) || len(events.events) != 1 {
		t.Fatalf("seeder response=%q events=%+v", seederResponse.Body.Bytes(), events.events)
	}
}

func TestHandlerReusesCompletionIdentityAfterWALAppendFailure(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	hash := [20]byte{}
	copy(hash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	handler, _ := testHandler(t, hash, passkey, true)
	current := time.Date(2026, 8, 8, 23, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return current }
	appender := &failOnceAppender{failOnCall: 2}
	handler.events = appender

	baseline := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 100), nil)
	baseline.RemoteAddr = "192.0.2.10:50000"
	handler.ServeHTTP(httptest.NewRecorder(), baseline)
	completionQuery := strings.Replace(announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 0), "downloaded=0", "downloaded=100", 1) + "&event=completed"

	current = current.Add(time.Second)
	failed := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+completionQuery, nil)
	failed.RemoteAddr = "192.0.2.10:50000"
	failedResponse := httptest.NewRecorder()
	handler.ServeHTTP(failedResponse, failed)
	if !bytes.Contains(failedResponse.Body.Bytes(), []byte("failure reason")) {
		t.Fatalf("failed completion response = %q", failedResponse.Body.Bytes())
	}

	current = current.Add(time.Second)
	retry := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+completionQuery, nil)
	retry.RemoteAddr = "192.0.2.10:50000"
	retryResponse := httptest.NewRecorder()
	handler.ServeHTTP(retryResponse, retry)
	if bytes.Contains(retryResponse.Body.Bytes(), []byte("failure reason")) {
		t.Fatalf("retry response = %q", retryResponse.Body.Bytes())
	}
	if len(appender.attempts) != 3 {
		t.Fatalf("attempts = %+v", appender.attempts)
	}
	first, second := appender.attempts[1], appender.attempts[2]
	if first.EventID == second.EventID || first.CompletionID == "" || first.CompletionID != second.CompletionID {
		t.Fatalf("first completion=%+v retry=%+v", first, second)
	}
}

func TestHandlerScrapeReturnsAuthenticatedRegisteredSwarmStatistics(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	var hash [20]byte
	copy(hash[:], "aaaaaaaaaaaaaaaaaaaa")
	handler, _ := testHandler(t, hash, passkey, true)
	handler.torrents = torrentAdmissionFixture{hash: hash, ready: true, completedDownloads: 12}
	policy := handler.policy.(*staticRuntimePolicy)
	policy.policy.ScrapeEnabled = true

	for _, peer := range []struct {
		id   string
		left int64
		port string
	}{
		{id: "bbbbbbbbbbbbbbbbbbbb", left: 100, port: "50000"},
		{id: "cccccccccccccccccccc", left: 0, port: "50001"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", peer.id, peer.left), nil)
		request.RemoteAddr = "192.0.2.10:" + peer.port
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if bytes.Contains(response.Body.Bytes(), []byte("failure reason")) {
			t.Fatalf("announce failed: %q", response.Body.Bytes())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/scrape?info_hash=aaaaaaaaaaaaaaaaaaaa", nil)
	request.RemoteAddr = "192.0.2.10:50002"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	want := []byte("d8:completei1e10:downloadedi12e10:incompletei1ee")
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), want) {
		t.Fatalf("scrape response code=%d body=%q", response.Code, response.Body.Bytes())
	}
}

func TestHandlerAppliesRuntimeUserRateLimit(t *testing.T) {
	t.Parallel()
	passkey := "00112233445566778899aabbccddeeff"
	var hash [20]byte
	copy(hash[:], "aaaaaaaaaaaaaaaaaaaa")
	handler, _ := testHandler(t, hash, passkey, true)
	policy := handler.policy.(*staticRuntimePolicy)
	policy.policy.UserRequestsPerMinute = 1
	policy.policy.UserBurst = 1

	for index := 0; index < 2; index++ {
		request := httptest.NewRequest(http.MethodGet, "/tracker/"+passkey+"/announce?"+announceQuery("aaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbb", 100), nil)
		request.RemoteAddr = "192.0.2.10:50000"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if index == 0 && bytes.Contains(response.Body.Bytes(), []byte("failure reason")) {
			t.Fatalf("first request failed: %q", response.Body.Bytes())
		}
		if index == 1 && !bytes.Contains(response.Body.Bytes(), []byte("request rate exceeded")) {
			t.Fatalf("second request was not rate limited: %q", response.Body.Bytes())
		}
	}
}

func testHandler(t *testing.T, hash [20]byte, passkey string, ready bool) (*Handler, *eventAppenderFixture) {
	t.Helper()
	parser, err := protocol.NewAnnounceParser(50, 100)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := swarm.NewEngine(swarm.Config{
		ShardCount: 4, MaxSwarms: 100, MaxPeers: 1000, MaxPeersPerSwarm: 100,
		PeerTTL: 30 * time.Minute, SweepBudget: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := &eventAppenderFixture{}
	handler, err := NewHandler(
		torrentAdmissionFixture{hash: hash, ready: ready},
		subjectAdmissionFixture{passkey: passkey, ready: ready},
		engine, events, announceevent.NewFactory(bytes.NewReader(bytes.Repeat([]byte{0xa1}, 128))), parser,
		Config{TorrentSnapshotMaxAge: time.Hour, SubjectSnapshotMaxAge: time.Minute, Interval: 1800, MinInterval: 900},
		func() time.Time { return time.Date(2026, 8, 8, 23, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	return handler, events
}

func trackerTorrent(id int64) trackercontrolv1.Torrent {
	return trackercontrolv1.Torrent{TorrentID: id}
}

func trackerSubject() trackersubjectcontrolv1.Subject {
	return trackersubjectcontrolv1.Subject{UserID: "0198f20a-6da8-7e51-9c64-111111111111", CredentialVersion: 1}
}

func announceQuery(infoHash, peerID string, left int64) string {
	return "info_hash=" + infoHash + "&peer_id=" + peerID + "&port=6881&uploaded=0&downloaded=0&left=" + strconv.FormatInt(left, 10) + "&compact=1"
}
