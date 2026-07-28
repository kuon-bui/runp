package process_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runp/internal/config"
	"runp/internal/process"
)

func TestProcessHealthRequiresSurvivalInterval(t *testing.T) {
	cfg := config.HealthConfig{
		Type:     "process",
		Interval: config.Duration(time.Millisecond),
		Timeout:  config.Duration(time.Second),
	}
	if err := process.WaitHealthy(context.Background(), cfg, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
}

func TestProcessHealthFailsWhenDead(t *testing.T) {
	cfg := config.HealthConfig{
		Type:     "process",
		Interval: config.Duration(time.Millisecond),
		Timeout:  config.Duration(time.Second),
	}
	err := process.WaitHealthy(context.Background(), cfg, func() bool { return false })
	if err == nil || !strings.Contains(err.Error(), "process health") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPHealthAcceptsSuccessAndRedirectStatuses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			cfg := config.HealthConfig{
				Type:     "http",
				URL:      server.URL,
				Interval: config.Duration(time.Millisecond),
				Timeout:  config.Duration(time.Second),
			}
			if err := process.WaitHealthy(context.Background(), cfg, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPHealthRetriesTerminalFailureUntilTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.HealthConfig{
		Type:     "http",
		URL:      server.URL,
		Interval: config.Duration(time.Millisecond),
		Timeout:  config.Duration(100 * time.Millisecond),
	}
	err := process.WaitHealthy(context.Background(), cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "http health") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPHealthHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := config.HealthConfig{
		Type:     "http",
		URL:      "http://127.0.0.1:1",
		Interval: config.Duration(time.Millisecond),
		Timeout:  config.Duration(time.Second),
	}
	err := process.WaitHealthy(ctx, cfg, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestTCPHealthConnects(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	cfg := config.HealthConfig{
		Type:     "tcp",
		Address:  listener.Addr().String(),
		Interval: config.Duration(time.Millisecond),
		Timeout:  config.Duration(time.Second),
	}
	if err := process.WaitHealthy(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTCPHealthRetriesUntilTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.HealthConfig{
		Type:     "tcp",
		Address:  address,
		Interval: config.Duration(time.Millisecond),
		Timeout:  config.Duration(10 * time.Millisecond),
	}
	err = process.WaitHealthy(context.Background(), cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "tcp health") {
		t.Fatalf("error = %v", err)
	}
}
