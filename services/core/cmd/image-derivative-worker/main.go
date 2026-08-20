package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
	platformobjectstore "github.com/peergo/peergo/services/core/internal/platform/objectstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	drain := flag.Bool("drain", false, "process every currently eligible derivative then exit")
	retryDead := flag.Bool("retry-dead", false, "requeue all dead webp-v1 jobs before processing")
	maxJobs := flag.Int("max-jobs", 0, "process at most this many eligible jobs, then exit (diagnostic only)")
	concurrency := flag.Int("concurrency", 1, "number of cooperating processors used by --drain (1-16)")
	drainTimeout := flag.Duration("drain-timeout", 30*time.Minute, "maximum duration for --drain")
	flag.Parse()
	if *maxJobs < 0 || (*maxJobs > 0 && *drain) || *concurrency < 1 || *concurrency > 16 || (!*drain && *concurrency != 1) {
		logger.Error("image derivative worker flags are invalid")
		os.Exit(2)
	}
	settings, err := platformconfig.LoadImageDerivativeWorker()
	if err != nil {
		logger.Error("invalid image derivative worker configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(settings.DatabaseURL)
	if err != nil {
		logger.Error("invalid image derivative database URL")
		os.Exit(1)
	}
	poolConfig.MaxConns = int32(*concurrency + 3)
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "peergo-image-derivative-worker"
	pool, err := pgxpool.NewWithConfig(startupCtx, poolConfig)
	if err != nil {
		logger.Error("open image derivative database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("ping image derivative database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		logger.Error("image derivative database migrations are not current", "error", err)
		os.Exit(1)
	}
	store, err := platformobjectstore.NewConfigured(startupCtx, settings.Store)
	if err != nil {
		logger.Error("compose image derivative object store", "error", err)
		os.Exit(1)
	}
	stores, err := objectstorage.NewRegistry(store)
	if err != nil {
		logger.Error("compose image derivative store registry", "error", err)
		os.Exit(1)
	}
	vipsConcurrency := runtime.NumCPU() / *concurrency
	if vipsConcurrency < 1 {
		vipsConcurrency = 1
	}
	transformer, err := imaging.NewVipsTransformer(imaging.VipsConfig{
		Binary: settings.VipsBinary, TempDir: settings.TempDir, Concurrency: vipsConcurrency,
	})
	if err != nil {
		logger.Error("compose libvips image transformer", "error", err)
		os.Exit(1)
	}
	if err := transformer.Probe(startupCtx); err != nil {
		logger.Error("libvips image transformer probe failed", "error", err)
		os.Exit(1)
	}
	logger.Info("configured image derivative processors", "processors", *concurrency, "vips_threads_per_process", vipsConcurrency)
	repository, err := imaging.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose image derivative repository", "error", err)
		os.Exit(1)
	}
	processor, err := imaging.NewProcessor(repository, stores, transformer, imaging.ProcessorConfig{
		ActiveBackendID: store.BackendID(), LeaseDuration: settings.LeaseDuration,
	})
	if err != nil {
		logger.Error("compose image derivative processor", "error", err)
		os.Exit(1)
	}
	if *retryDead {
		count, err := repository.RetryDead(ctx, time.Now())
		if err != nil {
			logger.Error("requeue dead image derivatives", "error", err)
			os.Exit(1)
		}
		logger.Info("requeued dead image derivatives", "count", count)
	}
	if *drain {
		if *drainTimeout < time.Second || *drainTimeout > 24*time.Hour {
			logger.Error("image derivative drain timeout is invalid")
			os.Exit(2)
		}
		drainCtx, drainCancel := context.WithTimeout(ctx, *drainTimeout)
		defer drainCancel()
		if err := drainEligible(drainCtx, processor, repository, *concurrency, settings.PollInterval, logger); err != nil {
			logger.Error("image derivative drain failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *maxJobs > 0 {
		if err := processAtMost(ctx, processor, *maxJobs, logger); err != nil {
			logger.Error("bounded image derivative processing failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(ctx, processor, settings.PollInterval, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("image derivative worker stopped", "error", err)
		os.Exit(1)
	}
}

func processAtMost(ctx context.Context, processor *imaging.Processor, maximum int, logger *slog.Logger) error {
	processed := 0
	for processed < maximum {
		worked, err := processor.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if !worked {
			break
		}
		processed++
	}
	logger.Info("bounded image derivative processing complete", "processed", processed, "maximum", maximum)
	return nil
}

func run(ctx context.Context, processor *imaging.Processor, poll time.Duration, logger *slog.Logger) error {
	for {
		processed, err := processor.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			logger.Debug("image derivative queue is idle")
		}
	}
}

type derivativeProcessor interface {
	ProcessNext(context.Context) (bool, error)
}

type derivativeOverview interface {
	Overview(context.Context) (imaging.QueueOverview, error)
}

func drainEligible(
	ctx context.Context,
	processor derivativeProcessor,
	repository derivativeOverview,
	concurrency int,
	pollInterval time.Duration,
	logger *slog.Logger,
) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// ProcessNext claims work with a database lease and SKIP LOCKED. A bounded
	// local worker group therefore increases migration throughput without
	// allowing two processors to publish the same derivative job.
	var processed atomic.Int64
	errCh := make(chan error, concurrency)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for {
				worked, err := processor.ProcessNext(workerCtx)
				if err != nil {
					if workerCtx.Err() == nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
					}
					return
				}
				if worked {
					count := processed.Add(1)
					if count%250 == 0 {
						logger.Info("draining image derivatives", "processed", count)
					}
					continue
				}

				overview, err := repository.Overview(workerCtx)
				if err != nil {
					if workerCtx.Err() == nil {
						select {
						case errCh <- err:
						default:
						}
						cancel()
					}
					return
				}
				if overview.Dead > 0 {
					select {
					case errCh <- errors.New("image derivative drain completed with dead work"):
					default:
					}
					cancel()
					return
				}
				if overview.Pending+overview.Processing+overview.Retrying == 0 {
					return
				}

				// Another processor may hold a lease, or retry backoff may not yet
				// be due. Keep polling until the queue is terminal or the bounded
				// drain context expires.
				timer := time.NewTimer(pollInterval)
				select {
				case <-workerCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	workers.Wait()
	select {
	case err := <-errCh:
		return err
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	overview, err := repository.Overview(ctx)
	if err != nil {
		return err
	}
	if overview.Pending+overview.Processing+overview.Retrying > 0 {
		return fmt.Errorf("image derivative drain stopped with %d non-terminal jobs", overview.Pending+overview.Processing+overview.Retrying)
	}
	if overview.Dead > 0 {
		return errors.New("image derivative drain completed with dead work")
	}
	logger.Info("image derivative drain complete", "processed", processed.Load(), "ready", overview.Ready)
	return nil
}
