package supervisor

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	HealthAddress    string
	RestartMinDelay  time.Duration
	RestartMaxDelay  time.Duration
	StableAfter      time.Duration
	MaxFailures      int
	ShutdownTimeout  time.Duration
	ReadinessTimeout time.Duration
}

func DefaultOptions() Options {
	return Options{
		HealthAddress:    ":8099",
		RestartMinDelay:  time.Second,
		RestartMaxDelay:  30 * time.Second,
		StableAfter:      5 * time.Minute,
		MaxFailures:      5,
		ShutdownTimeout:  15 * time.Second,
		ReadinessTimeout: time.Second,
	}
}

func OptionsFromEnvironment() (Options, error) {
	options := DefaultOptions()
	if value := strings.TrimSpace(os.Getenv("PEERGO_SUPERVISOR_HEALTH_ADDR")); value != "" {
		options.HealthAddress = value
	}
	var err error
	if options.RestartMinDelay, err = durationEnvironment("PEERGO_SUPERVISOR_RESTART_MIN_DELAY", options.RestartMinDelay); err != nil {
		return Options{}, err
	}
	if options.RestartMaxDelay, err = durationEnvironment("PEERGO_SUPERVISOR_RESTART_MAX_DELAY", options.RestartMaxDelay); err != nil {
		return Options{}, err
	}
	if options.StableAfter, err = durationEnvironment("PEERGO_SUPERVISOR_STABLE_AFTER", options.StableAfter); err != nil {
		return Options{}, err
	}
	if options.ShutdownTimeout, err = durationEnvironment("PEERGO_SUPERVISOR_SHUTDOWN_TIMEOUT", options.ShutdownTimeout); err != nil {
		return Options{}, err
	}
	if options.ReadinessTimeout, err = durationEnvironment("PEERGO_SUPERVISOR_READINESS_TIMEOUT", options.ReadinessTimeout); err != nil {
		return Options{}, err
	}
	if value := strings.TrimSpace(os.Getenv("PEERGO_SUPERVISOR_MAX_FAILURES")); value != "" {
		options.MaxFailures, err = strconv.Atoi(value)
		if err != nil || options.MaxFailures < 1 || options.MaxFailures > 100 {
			return Options{}, fmt.Errorf("PEERGO_SUPERVISOR_MAX_FAILURES must be between 1 and 100")
		}
	}
	if options.RestartMinDelay <= 0 || options.RestartMaxDelay < options.RestartMinDelay || options.StableAfter <= 0 ||
		options.ShutdownTimeout <= 0 || options.ReadinessTimeout <= 0 {
		return Options{}, fmt.Errorf("supervisor durations must be positive and restart max delay must not be below min delay")
	}
	return options, nil
}

func durationEnvironment(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}
	return duration, nil
}
