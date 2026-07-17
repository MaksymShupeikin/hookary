package shared

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// startHealthServer runs a health server on a free port and returns its base
// URL. It fails the test if the server does not accept connections in time.
func startHealthServer(t *testing.T, ready ReadyFunc) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunHealthServer(ctx, addr, ready) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server exited with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down within 5s of context cancel")
		}
	})

	waitForListener(t, addr)
	return "http://" + addr
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server never listened on %s", addr)
}

func statusOf(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestLiveIsAlwaysOK(t *testing.T) {
	base := startHealthServer(t, func(context.Context) error {
		return errors.New("dependency down")
	})

	// Liveness must ignore the probe: a failing dependency means "stop sending
	// traffic", not "restart me".
	if got := statusOf(t, base+"/health/live"); got != http.StatusOK {
		t.Errorf("live status = %d, want %d", got, http.StatusOK)
	}
}

func TestReadyWithoutProbeIsOK(t *testing.T) {
	base := startHealthServer(t, nil)

	if got := statusOf(t, base+"/health/ready"); got != http.StatusOK {
		t.Errorf("ready status = %d, want %d", got, http.StatusOK)
	}
}

func TestReadyReportsProbeFailure(t *testing.T) {
	base := startHealthServer(t, func(context.Context) error {
		return errors.New("postgres unreachable")
	})

	if got := statusOf(t, base+"/health/ready"); got != http.StatusServiceUnavailable {
		t.Errorf("ready status = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func TestReadyProbeGetsDeadline(t *testing.T) {
	deadlines := make(chan bool, 1)
	base := startHealthServer(t, func(ctx context.Context) error {
		_, ok := ctx.Deadline()
		deadlines <- ok
		return nil
	})

	statusOf(t, base+"/health/ready")

	select {
	case hasDeadline := <-deadlines:
		if !hasDeadline {
			t.Error("probe context has no deadline; a hung probe would wedge the endpoint")
		}
	case <-time.After(time.Second):
		t.Fatal("probe was never called")
	}
}
