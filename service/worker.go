package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gophermart/repo"
)

const (
	statusProcessing = "PROCESSING"
	statusInvalid    = "INVALID"
	statusProcessed  = "PROCESSED"
)

type WorkerPool struct {
	repo     repo.OrderRepo
	client   *AccrualClient
	interval time.Duration
	workers  int

	wg sync.WaitGroup
}

func NewWorkerPool(orderRepo repo.OrderRepo, client *AccrualClient, workers int, interval time.Duration) *WorkerPool {
	return &WorkerPool{
		repo:     orderRepo,
		client:   client,
		interval: interval,
		workers:  workers,
	}
}

func (w *WorkerPool) Start(ctx context.Context) {
	if w.workers <= 0 {
		return
	}
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go w.runWorker(ctx)
	}
}

func (w *WorkerPool) Wait() {
	w.wg.Wait()
}

func (w *WorkerPool) runWorker(ctx context.Context) {
	defer w.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := w.client.WaitIfRateLimited(ctx); err != nil {
			return
		}

		number, err := w.repo.ClaimNextForProcessing(ctx)
		if err != nil {
			if errors.Is(err, repo.ErrNoQueueItems) {
				if !w.sleep(ctx) {
					return
				}
				continue
			}
			log.Printf("worker: claim queue: %v", err)
			if !w.sleep(ctx) {
				return
			}
			continue
		}

		if err := w.processNumber(ctx, number); err != nil {
			log.Printf("worker: process %s: %v", number, err)
		}

		if !w.sleep(ctx) {
			return
		}
	}
}

func (w *WorkerPool) sleep(ctx context.Context) bool {
	if w.interval <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(w.interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (w *WorkerPool) processNumber(ctx context.Context, number string) error {
	path := fmt.Sprintf("/api/orders/%s", number)

	resp, err := w.client.DoGet(ctx, path)
	if err != nil {
		_ = w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC())
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var payload struct {
			Order   string   `json:"order"`
			Status  string   `json:"status"`
			Accrual *float64 `json:"accrual"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC())
			return fmt.Errorf("decode response: %w", err)
		}

		status := strings.ToUpper(payload.Status)
		if status == "" {
			status = statusProcessing
		}

		var applied bool

		switch status {
		case statusProcessed:
			if payload.Accrual != nil {
				if _, err := w.repo.ApplyOrderAccrual(ctx, number, *payload.Accrual); err != nil {
					return err
				}
				applied = true
			} else {
				if err := w.repo.UpdateOrderStatus(ctx, number, status, nil); err != nil {
					return err
				}
			}
			if !applied {
				if err := w.repo.DeleteProcessingEntry(ctx, number); err != nil {
					return err
				}
			}
		case statusInvalid:
			if err := w.repo.UpdateOrderStatus(ctx, number, status, nil); err != nil {
				return err
			}
			if err := w.repo.DeleteProcessingEntry(ctx, number); err != nil {
				return err
			}
		default:
			if err := w.repo.UpdateOrderStatus(ctx, number, status, payload.Accrual); err != nil {
				return err
			}
			if err := w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC()); err != nil {
				return err
			}
		}

	case http.StatusNoContent:
		if err := w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC()); err != nil {
			return err
		}

	case http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		w.client.SetRateLimitUntil(time.Now().Add(retryAfter))
		if err := w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC()); err != nil {
			return err
		}

	case http.StatusInternalServerError:
		log.Printf("worker: accrual 500 for %s", number)
		if err := w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC()); err != nil {
			return err
		}

	default:
		log.Printf("worker: unexpected status %d for %s", resp.StatusCode, number)
		if err := w.repo.UpdateProcessingStatus(ctx, number, statusProcessing, time.Now().UTC()); err != nil {
			return err
		}
	}

	return nil
}

func parseRetryAfter(value string) time.Duration {
	v := strings.TrimSpace(value)
	if v == "" {
		return 60 * time.Second
	}
	sec, err := strconv.Atoi(v)
	if err != nil || sec <= 0 {
		return 60 * time.Second
	}
	return time.Duration(sec) * time.Second
}
