package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"gophermart/repo"
)

type memoryWithdrawRepo struct {
	mu         sync.Mutex
	balance    float64
	withdrawn  float64
	operations []string
}

func newMemoryWithdrawRepo(initial float64) *memoryWithdrawRepo {
	return &memoryWithdrawRepo{balance: initial}
}

func (m *memoryWithdrawRepo) Withdraw(_ context.Context, userID int64, order string, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.balance < amount {
		return repo.ErrInsufficientFunds
	}

	m.balance -= amount
	m.withdrawn += amount
	m.operations = append(m.operations, order)
	return nil
}

func (m *memoryWithdrawRepo) ListWithdrawals(_ context.Context, userID int64) ([]repo.Withdrawal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []repo.Withdrawal{}, nil
}

func TestWithdrawHappyPath(t *testing.T) {
	store := newMemoryWithdrawRepo(200)
	svc := NewWithdrawManager(store)

	if err := svc.Withdraw(context.Background(), 1, "79927398713", 50); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	if store.balance != 150 {
		t.Fatalf("expected balance 150, got %v", store.balance)
	}
	if store.withdrawn != 50 {
		t.Fatalf("expected withdrawn 50, got %v", store.withdrawn)
	}
}

func TestWithdrawInsufficientFunds(t *testing.T) {
	store := newMemoryWithdrawRepo(10)
	svc := NewWithdrawManager(store)

	err := svc.Withdraw(context.Background(), 1, "79927398713", 20)
	if err == nil || err != repo.ErrInsufficientFunds {
		t.Fatalf("expected insufficient funds error, got %v", err)
	}

	if store.balance != 10 {
		t.Fatalf("balance should remain unchanged, got %v", store.balance)
	}
}

func TestWithdrawInvalidOrder(t *testing.T) {
	store := newMemoryWithdrawRepo(100)
	svc := NewWithdrawManager(store)

	if err := svc.Withdraw(context.Background(), 1, "12ab", 10); err != ErrInvalidOrderNumber {
		t.Fatalf("expected ErrInvalidOrderNumber, got %v", err)
	}
}

func TestWithdrawConcurrentNoNegative(t *testing.T) {
	store := newMemoryWithdrawRepo(100)
	svc := NewWithdrawManager(store)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = svc.Withdraw(context.Background(), 1, "79927398713", 60)
	}()

	go func() {
		defer wg.Done()

		time.Sleep(10 * time.Millisecond)
		_ = svc.Withdraw(context.Background(), 1, "49927398716", 50)
	}()

	wg.Wait()

	store.mu.Lock()
	defer store.mu.Unlock()

	if store.balance < 0 {
		t.Fatalf("balance went negative: %v", store.balance)
	}
}

var _ repo.WithdrawRepo = (*memoryWithdrawRepo)(nil)
