package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AccrualClient struct {
	baseURL        string
	httpClient     *http.Client
	mu             sync.RWMutex
	rateLimitUntil time.Time
}

func NewAccrualClient(baseURL string) *AccrualClient {
	trimmed := strings.TrimRight(baseURL, "/")
	if trimmed == "" {
		trimmed = baseURL
	}
	return &AccrualClient{
		baseURL:    trimmed,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *AccrualClient) WaitIfRateLimited(ctx context.Context) error {
	for {
		c.mu.RLock()
		until := c.rateLimitUntil
		c.mu.RUnlock()

		if until.IsZero() || time.Now().After(until) {
			return nil
		}

		wait := time.Until(until)
		if wait <= 0 {
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *AccrualClient) DoGet(ctx context.Context, path string) (*http.Response, error) {
	if err := c.WaitIfRateLimited(ctx); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	return c.httpClient.Do(req)
}

func (c *AccrualClient) SetRateLimitUntil(t time.Time) {
	c.mu.Lock()
	if t.After(c.rateLimitUntil) {
		c.rateLimitUntil = t
	}
	c.mu.Unlock()
}
