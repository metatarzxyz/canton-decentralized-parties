// Package metrics exposes the approver's state in the Prometheus text format
// (no client library needed for two series).
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics holds the approver's counters. The zero value is ready to use.
type Metrics struct {
	up         atomic.Bool
	approvals  atomic.Uint64
	rejections atomic.Uint64
	lastScan   atomic.Int64 // unix seconds of the last successful scan, 0 = none yet
}

// SetUp records whether the last scan of the participant's Admin API succeeded.
// A successful scan also updates the last-successful-scan timestamp.
func (m *Metrics) SetUp(ok bool) {
	m.up.Store(ok)
	if ok {
		m.lastScan.Store(time.Now().Unix())
	}
}

// IncRejections counts one proposal rejected by the policy.
func (m *Metrics) IncRejections() { m.rejections.Add(1) }

// IncApprovals counts one authorized hosting proposal.
func (m *Metrics) IncApprovals() { m.approvals.Add(1) }

// ServeHTTP writes the metrics in the Prometheus text exposition format.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	up := 0
	if m.up.Load() {
		up = 1
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP cdp_up 1 if the last scan of the participant's Admin API succeeded, else 0.\n")
	fmt.Fprintf(w, "# TYPE cdp_up gauge\ncdp_up %d\n", up)
	fmt.Fprintf(w, "# HELP cdp_approvals_total Hosting proposals authorized since this process started.\n")
	fmt.Fprintf(w, "# TYPE cdp_approvals_total counter\ncdp_approvals_total %d\n", m.approvals.Load())
	fmt.Fprintf(w, "# HELP cdp_rejections_total Hosting proposals rejected by the policy since this process started.\n")
	fmt.Fprintf(w, "# TYPE cdp_rejections_total counter\ncdp_rejections_total %d\n", m.rejections.Load())
	fmt.Fprintf(w, "# HELP cdp_last_successful_scan_timestamp_seconds Unix time of the last successful scan of the Admin API (0 = none yet).\n")
	fmt.Fprintf(w, "# TYPE cdp_last_successful_scan_timestamp_seconds gauge\ncdp_last_successful_scan_timestamp_seconds %d\n", m.lastScan.Load())
}

// Serve serves GET /metrics on address until ctx is done.
func (m *Metrics) Serve(ctx context.Context, address string) error {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("metrics server: %v\n", err)
		}
	}()
	return nil
}
