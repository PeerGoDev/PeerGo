package supervisor

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunStopsChildGracefully(t *testing.T) {
	if os.Getenv("PEERGO_SUPERVISOR_HELPER") == "graceful" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		<-signals
		return
	}
	options := testOptions()
	component := Component{
		Name: "helper", Executable: os.Args[0], Arguments: []string{"-test.run=TestRunStopsChildGracefully"},
		Environment: map[string]string{"PEERGO_SUPERVISOR_HELPER": "graceful"},
	}
	runner, err := New(ModeWorker, []Component{component}, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for !runner.Statuses()[0].Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !runner.Statuses()[0].Running {
		cancel()
		t.Fatal("helper component did not start")
	}
	cancel()
	select {
	case runErr := <-result:
		if runErr != nil {
			t.Fatalf("Run returned an error during graceful cancellation: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop the helper component")
	}
}

func TestRunFailsAfterRepeatedStartErrors(t *testing.T) {
	options := testOptions()
	options.MaxFailures = 2
	component := Component{Name: "missing", Executable: "/definitely/not/a/peergo/binary"}
	runner, err := New(ModeWorker, []Component{component}, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	runErr := runner.Run(ctx)
	if runErr == nil || !strings.Contains(runErr.Error(), "failed to start 2 consecutive times") {
		t.Fatalf("Run error = %v, want repeated start failure", runErr)
	}
}

func TestReadinessIncludesChildProbe(t *testing.T) {
	childReady := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer childReady.Close()
	options := testOptions()
	runner, err := New(ModeAPI, []Component{{Name: "api", Executable: "/bin/true", ReadyURL: childReady.URL}}, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	runner.mu.Lock()
	runner.statuses["api"] = ComponentStatus{Name: "api", PID: 42, Running: true, StartedAt: &now}
	runner.mu.Unlock()

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	runner.healthHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ready response = %d, want 200: %s", response.Code, response.Body.String())
	}
	childReady.Close()
	response = httptest.NewRecorder()
	runner.healthHandler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready response = %d, want 503", response.Code)
	}
}

func testOptions() Options {
	options := DefaultOptions()
	options.HealthAddress = "127.0.0.1:0"
	options.RestartMinDelay = time.Millisecond
	options.RestartMaxDelay = 2 * time.Millisecond
	options.StableAfter = time.Second
	options.ShutdownTimeout = time.Second
	options.ReadinessTimeout = 100 * time.Millisecond
	return options
}
