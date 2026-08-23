// Package jetstreamprovision owns the explicit operator-side creation check for
// the announce stream. Tracker runtime credentials never call this package and
// therefore do not need stream management permission.
package jetstreamprovision

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

var (
	ErrConfig = errors.New("Tracker announce stream configuration is invalid")
	ErrDrift  = errors.New("existing Tracker announce stream configuration differs")
)

type Manager interface {
	Get(context.Context, string) (jetstream.StreamConfig, error)
	Create(context.Context, jetstream.StreamConfig) error
	Update(context.Context, jetstream.StreamConfig) error
}

type NATSManager struct {
	jetStream jetstream.JetStream
}

func NewNATSManager(js jetstream.JetStream) (*NATSManager, error) {
	if js == nil {
		return nil, ErrConfig
	}
	return &NATSManager{jetStream: js}, nil
}

func (manager *NATSManager) Get(ctx context.Context, name string) (jetstream.StreamConfig, error) {
	stream, err := manager.jetStream.Stream(ctx, name)
	if err != nil {
		return jetstream.StreamConfig{}, err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return jetstream.StreamConfig{}, err
	}
	return info.Config, nil
}

func (manager *NATSManager) Create(ctx context.Context, config jetstream.StreamConfig) error {
	_, err := manager.jetStream.CreateStream(ctx, config)
	return err
}

func (manager *NATSManager) Update(ctx context.Context, config jetstream.StreamConfig) error {
	_, err := manager.jetStream.UpdateStream(ctx, config)
	return err
}

// Ensure creates a missing stream and permits only an in-place MaxAge change.
// Every identity, subject, storage, capacity and deletion-control field still
// fails closed as drift.
func Ensure(ctx context.Context, manager Manager, desired jetstream.StreamConfig) (bool, error) {
	if manager == nil || validate(desired) != nil {
		return false, ErrConfig
	}
	current, err := manager.Get(ctx, desired.Name)
	if err == nil {
		if !equivalent(current, desired) {
			if !maxAgeOnlyDrift(current, desired) {
				return false, ErrDrift
			}
			if err := manager.Update(ctx, desired); err != nil {
				return false, fmt.Errorf("update Tracker stream MaxAge: %w", err)
			}
		}
		return false, nil
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return false, fmt.Errorf("inspect Tracker announce stream: %w", err)
	}
	if err := manager.Create(ctx, desired); err != nil {
		return false, fmt.Errorf("create Tracker announce stream: %w", err)
	}
	return true, nil
}

func maxAgeOnlyDrift(current, desired jetstream.StreamConfig) bool {
	current.MaxAge = desired.MaxAge
	return equivalent(current, desired)
}

func validate(config jetstream.StreamConfig) error {
	if config.Name == "" || len(config.Subjects) != 1 || config.Subjects[0] == "" ||
		config.Retention != jetstream.LimitsPolicy || (config.Discard != jetstream.DiscardNew && config.Discard != jetstream.DiscardOld) ||
		config.Storage != jetstream.FileStorage || config.MaxBytes < 1 || config.MaxAge <= 0 ||
		config.MaxMsgSize < 1 || config.Replicas < 1 || config.Replicas > 5 || config.NoAck ||
		config.Duplicates <= 0 || config.MaxConsumers < 1 || config.PersistMode != jetstream.DefaultPersistMode ||
		config.Mirror != nil || len(config.Sources) != 0 || config.SubjectTransform != nil || config.RePublish != nil ||
		config.AllowRollup || config.AllowMsgTTL || config.AllowMsgCounter || config.AllowMsgSchedules {
		return ErrConfig
	}
	for key := range config.Metadata {
		if strings.HasPrefix(key, "_nats.") {
			return ErrConfig
		}
	}
	return nil
}

func equivalent(current, desired jetstream.StreamConfig) bool {
	return current.Name == desired.Name && current.Description == desired.Description &&
		reflect.DeepEqual(current.Subjects, desired.Subjects) &&
		current.Retention == desired.Retention && current.MaxConsumers == desired.MaxConsumers &&
		current.MaxMsgs == desired.MaxMsgs && current.MaxBytes == desired.MaxBytes &&
		current.Discard == desired.Discard && current.DiscardNewPerSubject == desired.DiscardNewPerSubject &&
		current.MaxAge == desired.MaxAge &&
		current.MaxMsgsPerSubject == desired.MaxMsgsPerSubject && current.MaxMsgSize == desired.MaxMsgSize &&
		current.Storage == desired.Storage && current.Replicas == desired.Replicas && current.NoAck == desired.NoAck &&
		current.Duplicates == desired.Duplicates && reflect.DeepEqual(current.Placement, desired.Placement) &&
		reflect.DeepEqual(current.Mirror, desired.Mirror) && reflect.DeepEqual(current.Sources, desired.Sources) &&
		current.Sealed == desired.Sealed && current.DenyDelete == desired.DenyDelete &&
		current.DenyPurge == desired.DenyPurge && current.AllowRollup == desired.AllowRollup &&
		current.Compression == desired.Compression && current.FirstSeq == desired.FirstSeq &&
		reflect.DeepEqual(current.SubjectTransform, desired.SubjectTransform) &&
		reflect.DeepEqual(current.RePublish, desired.RePublish) && current.AllowDirect == desired.AllowDirect &&
		current.MirrorDirect == desired.MirrorDirect && reflect.DeepEqual(current.ConsumerLimits, desired.ConsumerLimits) &&
		equivalentMetadata(current.Metadata, desired.Metadata) && current.Template == desired.Template &&
		current.AllowMsgTTL == desired.AllowMsgTTL && current.SubjectDeleteMarkerTTL == desired.SubjectDeleteMarkerTTL &&
		current.AllowMsgCounter == desired.AllowMsgCounter && current.AllowAtomicPublish == desired.AllowAtomicPublish &&
		current.AllowMsgSchedules == desired.AllowMsgSchedules && current.PersistMode == desired.PersistMode &&
		current.AllowBatchPublish == desired.AllowBatchPublish
}

// Recent NATS servers attach reserved _nats.* request metadata when they
// normalize a StreamConfig. Those server-owned keys are not operator drift;
// every application-owned key still has to match exactly.
func equivalentMetadata(current, desired map[string]string) bool {
	applicationCurrent := make(map[string]string, len(current))
	for key, value := range current {
		if !strings.HasPrefix(key, "_nats.") {
			applicationCurrent[key] = value
		}
	}
	return reflect.DeepEqual(applicationCurrent, desired)
}
