package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeHTTP(t *testing.T) {
	var m Metrics
	body := func() string {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
		return rec.Body.String()
	}
	if b := body(); !strings.Contains(b, "cdp_up 0\n") || !strings.Contains(b, "cdp_approvals_total 0\n") {
		t.Fatalf("initial metrics wrong:\n%s", b)
	}
	if b := body(); !strings.Contains(b, "cdp_last_successful_scan_timestamp_seconds 0\n") {
		t.Fatalf("scan timestamp should start at 0:\n%s", b)
	}
	m.SetUp(true)
	m.IncApprovals()
	m.IncApprovals()
	m.IncRejections()
	b := body()
	for _, want := range []string{"cdp_up 1\n", "cdp_approvals_total 2\n", "cdp_rejections_total 1\n"} {
		if !strings.Contains(b, want) {
			t.Fatalf("missing %q in:\n%s", want, b)
		}
	}
	if strings.Contains(b, "cdp_last_successful_scan_timestamp_seconds 0\n") {
		t.Fatalf("scan timestamp not updated:\n%s", b)
	}
	m.SetUp(false)
	if b := body(); !strings.Contains(b, "cdp_up 0\n") || strings.Contains(b, "cdp_last_successful_scan_timestamp_seconds 0\n") {
		t.Fatalf("failed scan must clear up but keep the timestamp:\n%s", b)
	}
}
