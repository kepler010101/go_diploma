package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"gophermart/repo"
)

type queueEntry struct {
	status    string
	lastCheck time.Time
}

type memoryOrderRepo struct {
	mu       sync.Mutex
	orders   map[string]*repo.Order
	queue    map[string]*queueEntry
	balances map[int64]float64
}

func newMemoryOrderRepo() *memoryOrderRepo {
	return &memoryOrderRepo{
		orders:   make(map[string]*repo.Order),
		queue:    make(map[string]*queueEntry),
		balances: make(map[int64]float64),
	}
}

func (m *memoryOrderRepo) GetByNumber(_ context.Context, number string) (*repo.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	order, ok := m.orders[number]
	if !ok {
		return nil, repo.ErrOrderNotFound
	}
	copy := *order
	if order.Accrual != nil {
		v := *order.Accrual
		copy.Accrual = &v
	}
	return &copy, nil
}

func (m *memoryOrderRepo) CreateOrder(_ context.Context, order *repo.Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.orders[order.Number]; exists {
		return repo.ErrOrderExists
	}
	copy := *order
	if order.Accrual != nil {
		v := *order.Accrual
		copy.Accrual = &v
	}
	m.orders[order.Number] = &copy
	if _, ok := m.balances[copy.UserID]; !ok {
		m.balances[copy.UserID] = 0
	}
	return nil
}

func (m *memoryOrderRepo) UpsertProcessing(_ context.Context, number string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.queue[number]
	if !ok {
		m.queue[number] = &queueEntry{status: status}
		return nil
	}
	entry.status = status
	return nil
}

func (m *memoryOrderRepo) ClaimNextForProcessing(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var (
		selected     string
		selectedTS   time.Time
		hasSelection bool
	)

	for number, entry := range m.queue {
		if entry.status != "NEW" && entry.status != "PROCESSING" {
			continue
		}
		candidate := entry.lastCheck
		if candidate.IsZero() {
			candidate = time.Unix(0, 0)
		}
		if !hasSelection || candidate.Before(selectedTS) {
			hasSelection = true
			selected = number
			selectedTS = candidate
		}
	}

	if !hasSelection {
		return "", repo.ErrNoQueueItems
	}

	entry := m.queue[selected]
	entry.status = "PROCESSING"
	entry.lastCheck = time.Now()

	return selected, nil
}

func (m *memoryOrderRepo) UpdateOrderStatus(_ context.Context, number string, status string, accrual *float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	order, ok := m.orders[number]
	if !ok {
		return repo.ErrOrderNotFound
	}
	order.Status = status
	if accrual != nil {
		v := *accrual
		order.Accrual = &v
	} else {
		order.Accrual = nil
	}
	if status != statusProcessed {
		order.AccrualApplied = false
	}
	return nil
}

func (m *memoryOrderRepo) UpdateProcessingStatus(_ context.Context, number string, status string, checkedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.queue[number]
	if !ok {
		return repo.ErrOrderNotFound
	}
	entry.status = status
	entry.lastCheck = checkedAt
	return nil
}

func (m *memoryOrderRepo) DeleteProcessingEntry(_ context.Context, number string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.queue, number)
	return nil
}

func (m *memoryOrderRepo) ApplyOrderAccrual(_ context.Context, number string, accrual float64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, ok := m.orders[number]
	if !ok {
		return false, repo.ErrOrderNotFound
	}

	if order.AccrualApplied {
		delete(m.queue, number)
		return false, nil
	}

	order.Status = statusProcessed
	v := accrual
	order.Accrual = &v
	order.AccrualApplied = true
	m.balances[order.UserID] += accrual
	delete(m.queue, number)

	return true, nil
}

func (m *memoryOrderRepo) ListUserOrders(_ context.Context, userID int64) ([]repo.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var orders []repo.Order
	for _, order := range m.orders {
		if order.UserID != userID {
			continue
		}
		copy := *order
		if order.Accrual != nil {
			v := *order.Accrual
			copy.Accrual = &v
		}
		orders = append(orders, copy)
	}
	return orders, nil
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func setupOrder(store *memoryOrderRepo, number string) {
	store.orders[number] = &repo.Order{
		Number:         number,
		UserID:         1,
		Status:         "NEW",
		UploadedAt:     time.Now(),
		AccrualApplied: false,
	}
	store.queue[number] = &queueEntry{status: "NEW"}
	if _, ok := store.balances[1]; !ok {
		store.balances[1] = 0
	}
}

func TestWorkerKeepsOrderInQueueForNonFinalStatuses(t *testing.T) {
	scenarios := []struct {
		name   string
		status string
	}{
		{name: "registered", status: "REGISTERED"},
		{name: "processing", status: "PROCESSING"},
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			mRepo := newMemoryOrderRepo()
			setupOrder(mRepo, "123456")

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"order":"123456","status":"` + tc.status + `"}`))
			}))
			defer server.Close()

			client := NewAccrualClient(server.URL)
			client.httpClient = server.Client()

			pool := NewWorkerPool(mRepo, client, 1, 50*time.Millisecond)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			pool.Start(ctx)

			waitForCondition(t, 1*time.Second, func() bool {
				mRepo.mu.Lock()
				defer mRepo.mu.Unlock()
				entry, ok := mRepo.queue["123456"]
				if !ok {
					return false
				}
				return entry.status == "PROCESSING" && !entry.lastCheck.IsZero()
			})

			cancel()
			pool.Wait()
		})
	}
}

func TestWorkerRemovesOrderForFinalStatuses(t *testing.T) {
	scenarios := []struct {
		name    string
		status  string
		accrual *float64
	}{
		{name: "invalid", status: "INVALID"},
		{name: "processed", status: "PROCESSED", accrual: func() *float64 { v := 42.5; return &v }()},
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			mRepo := newMemoryOrderRepo()
			setupOrder(mRepo, "654321")

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				payload := `{"order":"654321","status":"` + tc.status + `"`
				if tc.accrual != nil {
					payload += `,"accrual":` + strconv.FormatFloat(*tc.accrual, 'f', -1, 64)
				}
				payload += "}"
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()

			client := NewAccrualClient(server.URL)
			client.httpClient = server.Client()

			pool := NewWorkerPool(mRepo, client, 1, 50*time.Millisecond)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			pool.Start(ctx)

			waitForCondition(t, 1*time.Second, func() bool {
				mRepo.mu.Lock()
				defer mRepo.mu.Unlock()
				_, inQueue := mRepo.queue["654321"]
				if inQueue {
					return false
				}
				order := mRepo.orders["654321"]
				if order.Status != tc.status {
					return false
				}
				if tc.accrual != nil {
					if order.Accrual == nil {
						return false
					}
					return *order.Accrual == *tc.accrual
				}
				return true
			})

			cancel()
			pool.Wait()
		})
	}
}

func TestWorkerAppliesAccrualToBalance(t *testing.T) {
	const (
		orderNumber  = "555555"
		accrualValue = 17.5
	)

	mRepo := newMemoryOrderRepo()
	setupOrder(mRepo, orderNumber)

	mRepo.mu.Lock()
	userID := int64(42)
	order := mRepo.orders[orderNumber]
	order.UserID = userID
	mRepo.balances[userID] = 0
	mRepo.mu.Unlock()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "{\"order\":\"%s\",\"status\":\"PROCESSED\",\"accrual\":%.1f}", orderNumber, accrualValue)
	}))
	defer server.Close()

	client := NewAccrualClient(server.URL)
	client.httpClient = server.Client()

	pool := NewWorkerPool(mRepo, client, 1, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool.Start(ctx)

	waitForCondition(t, 1*time.Second, func() bool {
		mRepo.mu.Lock()
		defer mRepo.mu.Unlock()
		if _, inQueue := mRepo.queue[orderNumber]; inQueue {
			return false
		}
		order := mRepo.orders[orderNumber]
		if order == nil {
			return false
		}
		if order.Status != statusProcessed || !order.AccrualApplied {
			return false
		}
		if order.Accrual == nil || *order.Accrual != accrualValue {
			return false
		}
		return mRepo.balances[order.UserID] == accrualValue
	})

	cancel()
	pool.Wait()
}

func TestWorkerSkipsAlreadyAppliedAccrual(t *testing.T) {
	const (
		orderNumber  = "565656"
		accrualValue = 9.0
	)

	mRepo := newMemoryOrderRepo()
	setupOrder(mRepo, orderNumber)

	mRepo.mu.Lock()
	userID := int64(99)
	order := mRepo.orders[orderNumber]
	order.UserID = userID
	order.Status = statusProcessed
	order.AccrualApplied = true
	orderAccrual := accrualValue
	order.Accrual = &orderAccrual
	mRepo.balances[userID] = accrualValue
	mRepo.mu.Unlock()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "{\"order\":\"%s\",\"status\":\"PROCESSED\",\"accrual\":%.1f}", orderNumber, accrualValue)
	}))
	defer server.Close()

	client := NewAccrualClient(server.URL)
	client.httpClient = server.Client()

	pool := NewWorkerPool(mRepo, client, 1, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool.Start(ctx)

	waitForCondition(t, 1*time.Second, func() bool {
		mRepo.mu.Lock()
		defer mRepo.mu.Unlock()
		if _, inQueue := mRepo.queue[orderNumber]; inQueue {
			return false
		}
		if mRepo.balances[userID] != accrualValue {
			return false
		}
		order := mRepo.orders[orderNumber]
		return order != nil && order.AccrualApplied && order.Accrual != nil && *order.Accrual == accrualValue
	})

	cancel()
	pool.Wait()
}

func TestWorkerUpdatesQueueOnNoContent(t *testing.T) {
	mRepo := newMemoryOrderRepo()
	setupOrder(mRepo, "777777")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewAccrualClient(server.URL)
	client.httpClient = server.Client()

	pool := NewWorkerPool(mRepo, client, 1, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool.Start(ctx)

	waitForCondition(t, 1*time.Second, func() bool {
		mRepo.mu.Lock()
		defer mRepo.mu.Unlock()
		entry := mRepo.queue["777777"]
		return !entry.lastCheck.IsZero()
	})

	cancel()
	pool.Wait()
}

func TestWorkerRespectsRetryAfter(t *testing.T) {
	mRepo := newMemoryOrderRepo()
	setupOrder(mRepo, "999999")

	var (
		mu    sync.Mutex
		times []time.Time
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		mu.Lock()
		idx := len(times)
		times = append(times, now)
		mu.Unlock()

		if idx == 0 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"order":"999999","status":"PROCESSED","accrual":5}`))
	}))
	defer server.Close()

	client := NewAccrualClient(server.URL)
	client.httpClient = server.Client()

	pool := NewWorkerPool(mRepo, client, 1, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	pool.Start(ctx)

	waitForCondition(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(times) >= 2
	})

	mu.Lock()
	if len(times) < 2 {
		mu.Unlock()
		t.Fatalf("expected at least two requests, got %d", len(times))
	}
	diff := times[1].Sub(times[0])
	mu.Unlock()

	if diff < 2*time.Second {
		t.Fatalf("requests were too close: %v", diff)
	}

	cancel()
	pool.Wait()
}

var _ repo.OrderRepo = (*memoryOrderRepo)(nil)
