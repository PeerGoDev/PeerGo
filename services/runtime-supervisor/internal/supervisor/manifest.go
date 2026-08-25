package supervisor

import (
	"errors"
	"fmt"
)

type Mode string

const (
	ModeAPI    Mode = "api"
	ModeWorker Mode = "worker"
)

type Component struct {
	Name        string
	Executable  string
	Arguments   []string
	Environment map[string]string
	ReadyURL    string
}

func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	switch mode {
	case ModeAPI, ModeWorker:
		return mode, nil
	default:
		return "", fmt.Errorf("runtime mode must be %q or %q", ModeAPI, ModeWorker)
	}
}

func Components(mode Mode) ([]Component, error) {
	switch mode {
	case ModeAPI:
		return []Component{
			{Name: "core-api", Executable: "/usr/local/bin/core-api", ReadyURL: "http://127.0.0.1:8080/readyz"},
			{Name: "settlement-control-api", Executable: "/usr/local/bin/settlement-control-api", ReadyURL: "http://127.0.0.1:8085/healthz"},
			{Name: "email-relay", Executable: "/usr/local/bin/email-relay", ReadyURL: "http://127.0.0.1:8086/readyz"},
		}, nil
	case ModeWorker:
		names := []string{
			"core-snapshot-publisher",
			"core-audit-worker",
			"core-control-projector",
			"core-policy-worker",
			"core-image-derivative-worker",
			"core-seeding-reward-worker",
			"core-contribution-experience-worker",
			"core-storage-maintenance",
			"core-progression-level-worker",
			"settlement-ingest",
			"settlement-seeding-snapshot-projector",
			"settlement-seeding-evidence-worker",
			"settlement-policy-worker",
			"settlement-storage-maintenance",
			"settlement-traffic-dispatcher",
			"settlement-seeding-evidence-dispatcher",
			"settlement-hnr-worker",
			"settlement-hnr-dispatcher",
			"core-traffic-projector",
			"core-seeding-evidence-projector",
			"core-hnr-projector",
			"core-swarm-projector",
		}
		components := make([]Component, 0, len(names))
		for _, name := range names {
			components = append(components, Component{Name: name, Executable: "/usr/local/bin/" + name})
		}
		return components, nil
	default:
		return nil, errors.New("unknown runtime mode")
	}
}
