package process

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"runp/internal/config"
)

type AliveFunc func() bool

func WaitHealthy(ctx context.Context, cfg config.HealthConfig, alive AliveFunc) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Timeout))
	defer cancel()

	interval := time.Duration(cfg.Interval)
	if cfg.Type == config.HealthProcess {
		if err := wait(ctx, interval); err != nil {
			return fmt.Errorf("process health: %w", err)
		}
		if alive == nil || !alive() {
			return fmt.Errorf("process health: process exited")
		}
		return nil
	}

	var lastErr error
	for {
		var probeErr error
		switch cfg.Type {
		case config.HealthHTTP:
			probeErr = probeHTTP(ctx, cfg.URL)
		case config.HealthTCP:
			probeErr = probeTCP(ctx, cfg.Address)
		default:
			return fmt.Errorf("%s health: unsupported type", cfg.Type)
		}
		if probeErr == nil {
			return nil
		}
		if lastErr == nil || !isContextError(probeErr) {
			lastErr = probeErr
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%s health: %v: %w", cfg.Type, lastErr, context.Cause(ctx))
		}
		if err := wait(ctx, interval); err != nil {
			return fmt.Errorf("%s health: %v: %w", cfg.Type, lastErr, err)
		}
	}
}

func probeHTTP(ctx context.Context, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	if err := response.Body.Close(); err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected status %d", response.StatusCode)
	}
	return nil
}

func probeTCP(ctx context.Context, address string) error {
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return connection.Close()
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
