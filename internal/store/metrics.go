package store

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const (
	meterName = "github.com/michael-duren/rubber-duck/internal/store"
	poolName  = "duckserver"
)

// RegisterMetrics reports the pgx pool's Stat as observable instruments,
// read once per collection. Names and attributes match the database/sql
// services' so one dashboard serves both; pgx has no max-idle-count
// eviction, so closed carries only max_idle_time and max_lifetime.
// Unregister the returned registration before closing the Store.
func (s *Store) RegisterMetrics(mp metric.MeterProvider) (metric.Registration, error) {
	m := mp.Meter(meterName)
	count, err := m.Int64ObservableUpDownCounter("db.client.connection.count",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Connections currently in the pool, by state."))
	if err != nil {
		return nil, err
	}
	maxConns, err := m.Int64ObservableUpDownCounter("db.client.connection.max",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Maximum number of open connections allowed."))
	if err != nil {
		return nil, err
	}
	waits, err := m.Int64ObservableCounter("db.client.connection.waits",
		metric.WithUnit("{wait}"),
		metric.WithDescription("Connection requests that had to wait for a free connection."))
	if err != nil {
		return nil, err
	}
	waitDuration, err := m.Float64ObservableCounter("db.client.connection.wait_duration",
		metric.WithUnit("s"),
		metric.WithDescription("Total time spent waiting for a free connection."))
	if err != nil {
		return nil, err
	}
	closed, err := m.Int64ObservableCounter("db.client.connection.closed",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Connections closed by the pool, by reason."))
	if err != nil {
		return nil, err
	}

	pool := semconv.DBClientConnectionPoolName(poolName)
	var (
		all      = metric.WithAttributes(pool)
		idle     = metric.WithAttributes(pool, semconv.DBClientConnectionStateIdle)
		used     = metric.WithAttributes(pool, semconv.DBClientConnectionStateUsed)
		reason   = attribute.Key("reason")
		idleTime = metric.WithAttributes(pool, reason.String("max_idle_time"))
		lifetime = metric.WithAttributes(pool, reason.String("max_lifetime"))
	)
	return m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		st := s.pool.Stat()
		o.ObserveInt64(count, int64(st.IdleConns()), idle)
		o.ObserveInt64(count, int64(st.AcquiredConns()), used)
		o.ObserveInt64(maxConns, int64(st.MaxConns()), all)
		// EmptyAcquireCount is pgx's "acquire found no idle connection",
		// the same event database/sql counts as WaitCount.
		o.ObserveInt64(waits, st.EmptyAcquireCount(), all)
		o.ObserveFloat64(waitDuration, st.EmptyAcquireWaitTime().Seconds(), all)
		o.ObserveInt64(closed, st.MaxIdleDestroyCount(), idleTime)
		o.ObserveInt64(closed, st.MaxLifetimeDestroyCount(), lifetime)
		return nil
	}, count, maxConns, waits, waitDuration, closed)
}
