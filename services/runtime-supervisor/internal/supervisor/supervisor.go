package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type ComponentStatus struct {
	Name                string     `json:"name"`
	PID                 int        `json:"pid,omitempty"`
	Running             bool       `json:"running"`
	Restarts            int        `json:"restarts"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	ExitedAt            *time.Time `json:"exited_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}

type Supervisor struct {
	mode       Mode
	components []Component
	options    Options
	logger     *slog.Logger

	mu        sync.RWMutex
	statuses  map[string]ComponentStatus
	processes map[string]*os.Process
	fatal     chan error
	workers   sync.WaitGroup
	stopping  atomic.Bool
	client    *http.Client
}

func New(mode Mode, components []Component, options Options, logger *slog.Logger) (*Supervisor, error) {
	if len(components) == 0 {
		return nil, errors.New("at least one supervised component is required")
	}
	if logger == nil {
		return nil, errors.New("supervisor logger is required")
	}
	seen := make(map[string]struct{}, len(components))
	statuses := make(map[string]ComponentStatus, len(components))
	for _, component := range components {
		if strings.TrimSpace(component.Name) == "" || strings.TrimSpace(component.Executable) == "" {
			return nil, errors.New("component name and executable are required")
		}
		if _, exists := seen[component.Name]; exists {
			return nil, fmt.Errorf("duplicate component %q", component.Name)
		}
		seen[component.Name] = struct{}{}
		statuses[component.Name] = ComponentStatus{Name: component.Name}
	}
	return &Supervisor{
		mode: mode, components: slices.Clone(components), options: options, logger: logger,
		statuses: statuses, processes: make(map[string]*os.Process, len(components)), fatal: make(chan error, 1),
		client: &http.Client{Timeout: options.ReadinessTimeout},
	}, nil
}

func (supervisor *Supervisor) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", supervisor.options.HealthAddress)
	if err != nil {
		return fmt.Errorf("listen on supervisor health address: %w", err)
	}
	healthServer := &http.Server{
		Handler: supervisor.healthHandler(), ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
	}
	healthErrors := make(chan error, 1)
	go func() {
		if serveErr := healthServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			healthErrors <- serveErr
		}
	}()

	runCtx, cancel := context.WithCancel(ctx)
	for _, component := range supervisor.components {
		supervisor.workers.Add(1)
		go supervisor.runComponent(runCtx, component)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-supervisor.fatal:
	case serveErr := <-healthErrors:
		runErr = fmt.Errorf("supervisor health server failed: %w", serveErr)
	}

	supervisor.stopping.Store(true)
	cancel()
	supervisor.signalAll(syscall.SIGTERM)

	workersDone := make(chan struct{})
	go func() {
		supervisor.workers.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-time.After(supervisor.options.ShutdownTimeout):
		supervisor.logger.Warn("supervisor graceful shutdown timed out; killing remaining components")
		supervisor.signalAll(syscall.SIGKILL)
		select {
		case <-workersDone:
		case <-time.After(5 * time.Second):
			if runErr == nil {
				runErr = errors.New("supervised components did not exit after SIGKILL")
			}
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if shutdownErr := healthServer.Shutdown(shutdownCtx); shutdownErr != nil && runErr == nil {
		runErr = fmt.Errorf("shutdown supervisor health server: %w", shutdownErr)
	}
	return runErr
}

func (supervisor *Supervisor) runComponent(ctx context.Context, component Component) {
	defer supervisor.workers.Done()
	delay := supervisor.options.RestartMinDelay
	failures := 0
	restarts := 0
	for {
		if ctx.Err() != nil {
			return
		}
		startedAt := time.Now().UTC()
		command := exec.Command(component.Executable, component.Arguments...)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		command.Env = mergeEnvironment(os.Environ(), component.Environment)
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
		if err := command.Start(); err != nil {
			failures++
			supervisor.recordStartFailure(component.Name, restarts, failures, err)
			supervisor.logger.Error("supervised component failed to start", "component", component.Name, "error", err, "failure", failures)
			if failures >= supervisor.options.MaxFailures {
				supervisor.reportFatal(fmt.Errorf("component %s failed to start %d consecutive times: %w", component.Name, failures, err))
				return
			}
			restarts++
			if !waitForRestart(ctx, delay) {
				return
			}
			delay = nextDelay(delay, supervisor.options.RestartMaxDelay)
			continue
		}

		supervisor.recordStarted(component.Name, command.Process, restarts, startedAt)
		supervisor.logger.Info("supervised component started", "component", component.Name, "pid", command.Process.Pid, "restart", restarts)
		// Cancellation may race with Start after signalAll took its process
		// snapshot. Close that window so a late child never consumes the full
		// forced-shutdown timeout.
		if ctx.Err() != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		}
		waitErr := command.Wait()
		exitedAt := time.Now().UTC()
		runtime := exitedAt.Sub(startedAt)
		if ctx.Err() != nil {
			supervisor.recordExited(component.Name, restarts, failures, exitedAt, waitErr)
			return
		}

		if runtime >= supervisor.options.StableAfter {
			failures = 0
			delay = supervisor.options.RestartMinDelay
		}
		failures++
		restarts++
		supervisor.recordExited(component.Name, restarts, failures, exitedAt, waitErr)
		supervisor.logger.Error("supervised component exited unexpectedly", "component", component.Name, "error", exitDescription(waitErr), "runtime", runtime, "failure", failures)
		if failures >= supervisor.options.MaxFailures {
			supervisor.reportFatal(fmt.Errorf("component %s exited %d consecutive times: %s", component.Name, failures, exitDescription(waitErr)))
			return
		}
		if !waitForRestart(ctx, delay) {
			return
		}
		delay = nextDelay(delay, supervisor.options.RestartMaxDelay)
	}
}

func (supervisor *Supervisor) recordStarted(name string, process *os.Process, restarts int, startedAt time.Time) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	status := supervisor.statuses[name]
	status.PID = process.Pid
	status.Running = true
	status.Restarts = restarts
	status.StartedAt = &startedAt
	status.ExitedAt = nil
	status.LastError = ""
	supervisor.statuses[name] = status
	supervisor.processes[name] = process
}

func (supervisor *Supervisor) recordStartFailure(name string, restarts, failures int, startErr error) {
	now := time.Now().UTC()
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	status := supervisor.statuses[name]
	status.PID = 0
	status.Running = false
	status.Restarts = restarts
	status.ConsecutiveFailures = failures
	status.ExitedAt = &now
	status.LastError = startErr.Error()
	supervisor.statuses[name] = status
	delete(supervisor.processes, name)
}

func (supervisor *Supervisor) recordExited(name string, restarts, failures int, exitedAt time.Time, waitErr error) {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	status := supervisor.statuses[name]
	status.PID = 0
	status.Running = false
	status.Restarts = restarts
	status.ConsecutiveFailures = failures
	status.ExitedAt = &exitedAt
	status.LastError = exitDescription(waitErr)
	supervisor.statuses[name] = status
	delete(supervisor.processes, name)
}

func (supervisor *Supervisor) signalAll(signal syscall.Signal) {
	supervisor.mu.RLock()
	processes := make([]*os.Process, 0, len(supervisor.processes))
	for _, process := range supervisor.processes {
		processes = append(processes, process)
	}
	supervisor.mu.RUnlock()
	for _, process := range processes {
		if err := syscall.Kill(-process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
			supervisor.logger.Warn("signal supervised component process group", "pid", process.Pid, "signal", signal, "error", err)
		}
	}
}

func (supervisor *Supervisor) reportFatal(err error) {
	select {
	case supervisor.fatal <- err:
	default:
	}
}

func (supervisor *Supervisor) Statuses() []ComponentStatus {
	supervisor.mu.RLock()
	defer supervisor.mu.RUnlock()
	statuses := make([]ComponentStatus, 0, len(supervisor.components))
	for _, component := range supervisor.components {
		statuses = append(statuses, supervisor.statuses[component.Name])
	}
	return statuses
}

func waitForRestart(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextDelay(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}
	return current * 2
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		if found {
			merged[name] = value
		}
	}
	for name, value := range overrides {
		merged[name] = value
	}
	result := make([]string, 0, len(merged))
	for name, value := range merged {
		result = append(result, name+"="+value)
	}
	slices.Sort(result)
	return result
}

func exitDescription(err error) string {
	if err == nil {
		return "process exited successfully"
	}
	return err.Error()
}
