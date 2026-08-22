// Package telemetry exposes bounded production metrics for Tracker cutover
// and incident response. Metric labels never contain subject, torrent, peer or
// network identifiers.
package telemetry

import (
	"fmt"

	"github.com/peergo/peergo/services/tracker/internal/httpserver"
	"github.com/peergo/peergo/services/tracker/internal/wal"
	"github.com/prometheus/client_golang/prometheus"
)

type SwarmCounts interface {
	Counts() (swarms, peers int64)
}

type WALStats interface {
	Stats() wal.Stats
}

type Metrics struct {
	requests    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	rateLimited *prometheus.CounterVec
}

func New(registerer prometheus.Registerer, swarms SwarmCounts, eventWAL WALStats) (*Metrics, error) {
	if registerer == nil || swarms == nil || eventWAL == nil {
		return nil, fmt.Errorf("Tracker telemetry dependencies are required")
	}
	metrics := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "requests_total",
			Help: "Tracker protocol requests by bounded outcome and client dimensions.",
		}, []string{"action", "result", "address_family", "client_family", "event"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "request_duration_seconds",
			Help:    "Tracker protocol request duration by action and bounded result.",
			Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"action", "result"}),
		rateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "rate_limited_total",
			Help: "Tracker requests rejected by the bounded address or user limiter.",
		}, []string{"action", "scope", "address_family"}),
	}
	collectors := []prometheus.Collector{
		metrics.requests,
		metrics.duration,
		metrics.rateLimited,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "active_swarms",
			Help: "Current in-memory swarm count.",
		}, func() float64 {
			count, _ := swarms.Counts()
			return float64(count)
		}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "active_peers",
			Help: "Current in-memory peer count.",
		}, func() float64 {
			_, count := swarms.Counts()
			return float64(count)
		}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "wal_bytes",
			Help: "Current durable announce WAL bytes.",
		}, func() float64 { return float64(eventWAL.Stats().Bytes) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "wal_unacknowledged_bytes",
			Help: "Announce WAL bytes not yet acknowledged by JetStream.",
		}, func() float64 { return float64(eventWAL.Stats().UnacknowledgedBytes) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "peergo", Subsystem: "tracker", Name: "wal_capacity_bytes",
			Help: "Configured durable announce WAL capacity in bytes.",
		}, func() float64 { return float64(eventWAL.Stats().CapacityBytes) }),
	}
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			return nil, fmt.Errorf("register Tracker telemetry: %w", err)
		}
	}
	return metrics, nil
}

func (metrics *Metrics) ObserveRequest(observation httpserver.RequestObservation) {
	if metrics == nil {
		return
	}
	metrics.requests.WithLabelValues(
		observation.Action, observation.Result, observation.AddressFamily,
		observation.ClientFamily, observation.Event,
	).Inc()
	metrics.duration.WithLabelValues(observation.Action, observation.Result).Observe(observation.Duration.Seconds())
	if observation.Result == "rate_limited" &&
		(observation.RateLimitScope == "address" || observation.RateLimitScope == "user") {
		metrics.rateLimited.WithLabelValues(
			observation.Action, observation.RateLimitScope, observation.AddressFamily,
		).Inc()
	}
}
