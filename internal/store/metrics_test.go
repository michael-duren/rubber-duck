package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRegisterMetrics(t *testing.T) {
	// pgxpool.New dials lazily, so pool stats are readable without
	// PostgreSQL; Open would ping, hence the Store literal.
	p, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable&pool_max_conns=7")
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{pool: p}
	defer s.Close()
	reader := sdkmetric.NewManualReader()
	reg, err := s.RegisterMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Unregister()

	var rm metricdata.ResourceMetrics
	if err = reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	points := map[string]int{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				points[m.Name] = len(d.DataPoints)
				if m.Name == "db.client.connection.max" && d.DataPoints[0].Value != 7 {
					t.Errorf("max connections = %d, want 7", d.DataPoints[0].Value)
				}
			case metricdata.Sum[float64]:
				points[m.Name] = len(d.DataPoints)
			}
		}
	}
	want := map[string]int{
		"db.client.connection.count":         2, // idle, used
		"db.client.connection.max":           1,
		"db.client.connection.waits":         1,
		"db.client.connection.wait_duration": 1,
		"db.client.connection.closed":        2, // max_idle_time, max_lifetime
	}
	for name, n := range want {
		if points[name] != n {
			t.Errorf("%s: %d data points, want %d", name, points[name], n)
		}
	}
}
