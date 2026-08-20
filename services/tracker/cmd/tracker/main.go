package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/services/tracker/internal/announceevent"
	"github.com/peergo/peergo/services/tracker/internal/announcepublisher"
	"github.com/peergo/peergo/services/tracker/internal/config"
	"github.com/peergo/peergo/services/tracker/internal/control"
	"github.com/peergo/peergo/services/tracker/internal/httpserver"
	"github.com/peergo/peergo/services/tracker/internal/jetstreampublisher"
	"github.com/peergo/peergo/services/tracker/internal/protocol"
	"github.com/peergo/peergo/services/tracker/internal/runtimepolicy"
	"github.com/peergo/peergo/services/tracker/internal/subjectcontrol"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
	"github.com/peergo/peergo/services/tracker/internal/swarmsnapshot"
	"github.com/peergo/peergo/services/tracker/internal/telemetry"
	"github.com/peergo/peergo/services/tracker/internal/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Tracker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadServer()
	if err != nil {
		return fmt.Errorf("load Tracker server configuration: %w", err)
	}
	torrentStore, err := control.NewStore(settings.Control.TrustedKeys, settings.Control.MaxFutureSkew)
	if err != nil {
		return fmt.Errorf("compose Tracker torrent control store: %w", err)
	}
	torrentLoader, err := control.NewFileLoader(settings.Control.SnapshotPath, torrentStore)
	if err != nil {
		return fmt.Errorf("compose Tracker torrent snapshot loader: %w", err)
	}
	subjectStore, err := subjectcontrol.NewStore(
		settings.Control.TrustedKeys, settings.Control.PasskeyLookupKey, settings.Control.MaxFutureSkew,
	)
	if err != nil {
		return fmt.Errorf("compose Tracker subject control store: %w", err)
	}
	subjectLoader, err := subjectcontrol.NewFileLoader(settings.Control.SubjectSnapshotPath, subjectStore)
	if err != nil {
		return fmt.Errorf("compose Tracker subject snapshot loader: %w", err)
	}
	runtimePolicyStore, err := runtimepolicy.NewStore(settings.Control.TrustedKeys, settings.Control.MaxFutureSkew)
	if err != nil {
		return fmt.Errorf("compose Tracker runtime policy store: %w", err)
	}
	runtimePolicyLoader, err := runtimepolicy.NewFileLoader(settings.Control.RuntimePolicyPath, runtimePolicyStore)
	if err != nil {
		return fmt.Errorf("compose Tracker runtime policy loader: %w", err)
	}
	now := time.Now().UTC()
	if _, err := torrentLoader.LoadOnce(now); err != nil {
		return fmt.Errorf("load initial Tracker torrent snapshot: %w", err)
	}
	if _, err := subjectLoader.LoadOnce(now); err != nil {
		return fmt.Errorf("load initial Tracker subject snapshot: %w", err)
	}
	if _, err := runtimePolicyLoader.LoadOnce(now); err != nil {
		return fmt.Errorf("load initial Tracker runtime policy snapshot: %w", err)
	}
	if err := torrentStore.Ready(now, settings.Control.MaxAge); err != nil {
		return fmt.Errorf("initial Tracker torrent snapshot is not ready: %w", err)
	}
	if err := subjectStore.Ready(now, settings.Control.SubjectMaxAge); err != nil {
		return fmt.Errorf("initial Tracker subject snapshot is not ready: %w", err)
	}
	if err := runtimePolicyStore.Ready(now, settings.Control.RuntimePolicyMaxAge); err != nil {
		return fmt.Errorf("initial Tracker runtime policy snapshot is not ready: %w", err)
	}
	parser, err := protocol.NewAnnounceParser(settings.DefaultNumWant, settings.MaxNumWant)
	if err != nil {
		return fmt.Errorf("compose Tracker announce parser: %w", err)
	}
	engine, err := swarm.NewEngine(settings.Swarm)
	if err != nil {
		return fmt.Errorf("compose Swarm Engine: %w", err)
	}
	eventWAL, err := wal.OpenFile(settings.WALPath, settings.MaxWALBytes)
	if err != nil {
		return fmt.Errorf("open Tracker announce WAL: %w", err)
	}
	defer eventWAL.Close()
	natsConnection, jetStream, err := jetstreampublisher.Connect(jetstreampublisher.ConnectionConfig{
		URLs: settings.NATSURLs, CredentialsFile: settings.NATSCredentialsFile,
		RootCAFile: settings.NATSRootCAFile, ConnectTimeout: settings.NATSConnectTimeout,
		ReconnectWait: settings.NATSReconnectWait,
	}, logger)
	if err != nil {
		return fmt.Errorf("compose Tracker JetStream connection: %w", err)
	}
	defer natsConnection.Close()
	eventSink, err := jetstreampublisher.NewSink(jetStream, settings.AnnounceStream, settings.AnnounceSubject)
	if err != nil {
		return fmt.Errorf("compose Tracker JetStream sink: %w", err)
	}
	eventPublisher, err := announcepublisher.New(eventWAL, eventSink, announcepublisher.Config{
		PublishTimeout: settings.PublishTimeout, RetryMinimum: settings.PublishRetryMinimum,
		RetryMaximum: settings.PublishRetryMaximum, CompactAtBytes: settings.WALCompactAtBytes,
	}, logger)
	if err != nil {
		return fmt.Errorf("compose Tracker announce publisher: %w", err)
	}
	swarmSink, err := swarmsnapshot.NewJetStreamSink(jetStream, settings.SwarmSnapshotStream, settings.SwarmSnapshotSubject)
	if err != nil {
		return fmt.Errorf("compose Tracker swarm snapshot sink: %w", err)
	}
	swarmSequence, err := swarmsnapshot.OpenFileSequenceStore(settings.SwarmSequencePath, settings.SwarmSnapshotSourceID, settings.SwarmRoutingEpoch)
	if err != nil {
		return fmt.Errorf("open Tracker swarm snapshot sequence: %w", err)
	}
	swarmFactory, err := swarmsnapshot.NewFactory(settings.SwarmSnapshotSourceID, settings.SwarmRoutingEpoch, settings.SwarmMaxChunkEntries, nil)
	if err != nil {
		return fmt.Errorf("compose Tracker swarm snapshot factory: %w", err)
	}
	swarmPublisher, err := swarmsnapshot.NewPublisher(engine, swarmFactory, swarmSequence, swarmSink, swarmsnapshot.PublisherConfig{
		Interval: settings.SwarmSnapshotInterval, PublishTimeout: settings.PublishTimeout,
		RetryMinimum: settings.PublishRetryMinimum, RetryMaximum: settings.PublishRetryMaximum,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Tracker swarm snapshot publisher: %w", err)
	}
	registry := prometheus.NewRegistry()
	for _, collector := range []prometheus.Collector{
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	} {
		if err := registry.Register(collector); err != nil {
			return fmt.Errorf("register Tracker runtime telemetry: %w", err)
		}
	}
	trackerMetrics, err := telemetry.New(registry, engine, eventWAL)
	if err != nil {
		return fmt.Errorf("compose Tracker telemetry: %w", err)
	}
	handler, err := httpserver.NewHandler(torrentStore, subjectStore, engine, eventWAL, announceevent.NewFactory(nil), parser, httpserver.Config{
		TorrentSnapshotMaxAge: settings.Control.MaxAge,
		SubjectSnapshotMaxAge: settings.Control.SubjectMaxAge,
		Interval:              settings.AnnounceInterval, MinInterval: settings.MinAnnounceInterval,
		RuntimePolicy: runtimePolicyStore, RuntimePolicyMaxAge: settings.Control.RuntimePolicyMaxAge,
		Observer: trackerMetrics,
		Operations: &httpserver.OperationsConfig{
			ServiceToken: settings.ServiceToken,
			Runtime: trackeroperationsv1.Runtime{
				AnnounceIntervalSeconds:    int64(settings.AnnounceInterval),
				MinAnnounceIntervalSeconds: int64(settings.MinAnnounceInterval),
				DefaultNumWant:             int64(settings.DefaultNumWant), MaxNumWant: int64(settings.MaxNumWant),
				PeerTTLSeconds: int64(settings.Swarm.PeerTTL / time.Second),
				MaxSwarms:      settings.Swarm.MaxSwarms, MaxPeers: settings.Swarm.MaxPeers,
				MaxPeersPerSwarm: int64(settings.Swarm.MaxPeersPerSwarm),
			},
		},
	}, time.Now)
	if err != nil {
		return fmt.Errorf("compose Tracker HTTP handler: %w", err)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go reloadSnapshots(rootCtx, logger, settings.SnapshotReloadInterval, torrentLoader, subjectLoader, runtimePolicyLoader)
	go sweepSwarms(rootCtx, logger, settings.SwarmSweepInterval, settings.SwarmSweepBudget, engine)
	go func() {
		if err := swarmPublisher.Run(rootCtx); err != nil {
			logger.Error("Tracker swarm snapshot publisher stopped", "error", err)
		}
	}()
	publisherErrors := make(chan error, 1)
	go func() { publisherErrors <- eventPublisher.Run(rootCtx) }()

	trackerServer := &http.Server{
		Addr: settings.ListenAddress, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 8 << 10,
	}
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true, MaxRequestsInFlight: 2, Timeout: 5 * time.Second,
	}))
	metricsServer := &http.Server{
		Addr: settings.MetricsListenAddress, Handler: metricsMux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 4 << 10,
	}
	trackerServerErrors := make(chan error, 1)
	metricsServerErrors := make(chan error, 1)
	go func() { trackerServerErrors <- trackerServer.ListenAndServe() }()
	go func() { metricsServerErrors <- metricsServer.ListenAndServe() }()
	logger.Info("Tracker HTTP server started", "listen_address", settings.ListenAddress)
	logger.Info("Tracker metrics server started", "listen_address", settings.MetricsListenAddress)
	var runtimeErr error
	trackerServerCompleted := false
	metricsServerCompleted := false
	publisherCompleted := false
	select {
	case <-rootCtx.Done():
	case err := <-trackerServerErrors:
		trackerServerCompleted = true
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtimeErr = fmt.Errorf("Tracker HTTP server failed: %w", err)
		}
	case err := <-metricsServerErrors:
		metricsServerCompleted = true
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtimeErr = fmt.Errorf("Tracker metrics server failed: %w", err)
		}
	case err := <-publisherErrors:
		publisherCompleted = true
		if err != nil {
			runtimeErr = fmt.Errorf("Tracker announce publisher failed: %w", err)
		}
	}
	stop()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	if !trackerServerCompleted {
		if err := trackerServer.Shutdown(shutdownCtx); err != nil {
			runtimeErr = errors.Join(runtimeErr, fmt.Errorf("shutdown Tracker HTTP server: %w", err))
		}
		if err := <-trackerServerErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtimeErr = errors.Join(runtimeErr, fmt.Errorf("Tracker HTTP server stopped unexpectedly: %w", err))
		}
	}
	if !metricsServerCompleted {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			runtimeErr = errors.Join(runtimeErr, fmt.Errorf("shutdown Tracker metrics server: %w", err))
		}
		if err := <-metricsServerErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtimeErr = errors.Join(runtimeErr, fmt.Errorf("Tracker metrics server stopped unexpectedly: %w", err))
		}
	}
	cancelShutdown()
	if !publisherCompleted {
		if err := <-publisherErrors; err != nil {
			runtimeErr = errors.Join(runtimeErr, fmt.Errorf("stop Tracker announce publisher: %w", err))
		}
	}
	return runtimeErr
}

func sweepSwarms(ctx context.Context, logger *slog.Logger, interval time.Duration, budget int, engine *swarm.Engine) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := engine.Sweep(now, budget); err != nil {
				logger.Error("Swarm Engine expiry sweep failed", "error", err)
			}
		}
	}
}

func reloadSnapshots(ctx context.Context, logger *slog.Logger, interval time.Duration, torrentLoader *control.FileLoader, subjectLoader *subjectcontrol.FileLoader, runtimePolicyLoader *runtimepolicy.FileLoader) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if result, err := torrentLoader.LoadOnce(now); err != nil {
				logger.Warn("Tracker torrent snapshot reload failed", "error", err)
			} else if result.Activated {
				logger.Info("Tracker torrent snapshot activated", "control_sequence", result.Status.ControlSequence, "torrent_count", result.Status.TorrentCount)
			}
			if result, err := subjectLoader.LoadOnce(now); err != nil {
				logger.Warn("Tracker subject snapshot reload failed", "error", err)
			} else if result.Activated {
				logger.Info("Tracker subject snapshot activated", "control_sequence", result.Status.ControlSequence, "subject_count", result.Status.SubjectCount)
			}
			if result, err := runtimePolicyLoader.LoadOnce(now); err != nil {
				logger.Warn("Tracker runtime policy snapshot reload failed", "error", err)
			} else if result.Activated {
				logger.Info("Tracker runtime policy snapshot activated", "control_sequence", result.Status.ControlSequence, "revision", result.Status.Revision)
			}
		}
	}
}
