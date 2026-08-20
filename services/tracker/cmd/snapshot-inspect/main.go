package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/config"
	"github.com/peergo/peergo/services/tracker/internal/control"
	"github.com/peergo/peergo/services/tracker/internal/runtimepolicy"
	"github.com/peergo/peergo/services/tracker/internal/subjectcontrol"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load()
	if err != nil {
		logger.Error("invalid Tracker snapshot loader configuration", "error", err)
		os.Exit(1)
	}
	store, err := control.NewStore(settings.TrustedKeys, settings.MaxFutureSkew)
	if err != nil {
		logger.Error("compose Tracker control store", "error", err)
		os.Exit(1)
	}
	loader, err := control.NewFileLoader(settings.SnapshotPath, store)
	if err != nil {
		logger.Error("compose Tracker snapshot loader", "error", err)
		os.Exit(1)
	}
	now := time.Now()
	result, err := loader.LoadOnce(now)
	if err != nil {
		logger.Error("verify Tracker control snapshot", "error", err)
		os.Exit(1)
	}
	if err := store.Ready(now, settings.MaxAge); err != nil {
		logger.Error("Tracker control snapshot is not ready", "error", err,
			"control_sequence", result.Status.ControlSequence, "generated_at", result.Status.GeneratedAt)
		os.Exit(1)
	}
	logger.Info("Tracker control snapshot verified",
		"control_sequence", result.Status.ControlSequence, "torrent_count", result.Status.TorrentCount,
		"generated_at", result.Status.GeneratedAt, "key_id", result.Status.KeyID,
		"state_sha256", result.Status.StateSHA256,
	)
	subjectStore, err := subjectcontrol.NewStore(settings.TrustedKeys, settings.PasskeyLookupKey, settings.MaxFutureSkew)
	if err != nil {
		logger.Error("compose Tracker subject control store", "error", err)
		os.Exit(1)
	}
	subjectLoader, err := subjectcontrol.NewFileLoader(settings.SubjectSnapshotPath, subjectStore)
	if err != nil {
		logger.Error("compose Tracker subject snapshot loader", "error", err)
		os.Exit(1)
	}
	subjectResult, err := subjectLoader.LoadOnce(now)
	if err != nil {
		logger.Error("verify Tracker subject control snapshot", "error", err)
		os.Exit(1)
	}
	if err := subjectStore.Ready(now, settings.SubjectMaxAge); err != nil {
		logger.Error("Tracker subject control snapshot is not ready", "error", err,
			"control_sequence", subjectResult.Status.ControlSequence, "generated_at", subjectResult.Status.GeneratedAt)
		os.Exit(1)
	}
	logger.Info("Tracker subject control snapshot verified",
		"control_sequence", subjectResult.Status.ControlSequence, "subject_count", subjectResult.Status.SubjectCount,
		"generated_at", subjectResult.Status.GeneratedAt, "key_id", subjectResult.Status.KeyID,
		"state_sha256", subjectResult.Status.StateSHA256,
	)
	runtimePolicyStore, err := runtimepolicy.NewStore(settings.TrustedKeys, settings.MaxFutureSkew)
	if err != nil {
		logger.Error("compose Tracker runtime policy store", "error", err)
		os.Exit(1)
	}
	runtimePolicyLoader, err := runtimepolicy.NewFileLoader(settings.RuntimePolicyPath, runtimePolicyStore)
	if err != nil {
		logger.Error("compose Tracker runtime policy loader", "error", err)
		os.Exit(1)
	}
	runtimePolicyResult, err := runtimePolicyLoader.LoadOnce(now)
	if err != nil {
		logger.Error("verify Tracker runtime policy snapshot", "error", err)
		os.Exit(1)
	}
	if err := runtimePolicyStore.Ready(now, settings.RuntimePolicyMaxAge); err != nil {
		logger.Error("Tracker runtime policy snapshot is not ready", "error", err,
			"control_sequence", runtimePolicyResult.Status.ControlSequence,
			"generated_at", runtimePolicyResult.Status.GeneratedAt)
		os.Exit(1)
	}
	logger.Info("Tracker runtime policy snapshot verified",
		"control_sequence", runtimePolicyResult.Status.ControlSequence,
		"revision", runtimePolicyResult.Status.Revision,
		"generated_at", runtimePolicyResult.Status.GeneratedAt,
		"key_id", runtimePolicyResult.Status.KeyID,
		"state_sha256", runtimePolicyResult.Status.StateSHA256,
	)
}
