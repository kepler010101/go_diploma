package service

import (
	"context"
	"strings"

	"gophermart/repo"
)

type AuthService interface {
	Register(ctx context.Context, login, password string) (string, error)
	Login(ctx context.Context, login, password string) (string, error)
	ValidateToken(ctx context.Context, token string) (int64, error)
}

type OrderService interface {
	SubmitOrder(ctx context.Context, userID int64, number string) (created bool, err error)
	ListUserOrders(ctx context.Context, userID int64) ([]repo.Order, error)
}

type Dependencies struct {
	UserRepo    repo.UserRepo
	OrderRepo   repo.OrderRepo
	TokenSecret string
}

type BalanceService interface {
	GetBalance(ctx context.Context, userID int64) (float64, float64, error)
}

type BalanceManager struct {
	users repo.UserRepo
}

func NewBalanceManager(users repo.UserRepo) *BalanceManager {
	return &BalanceManager{users: users}
}

func (m *BalanceManager) GetBalance(ctx context.Context, userID int64) (float64, float64, error) {
	if m == nil {
		return 0, 0, nil
	}
	return m.users.GetBalance(ctx, userID)
}

type WithdrawService interface {
	Withdraw(ctx context.Context, userID int64, order string, amount float64) error
	ListUserWithdrawals(ctx context.Context, userID int64) ([]repo.Withdrawal, error)
}

type WithdrawManager struct {
	repo repo.WithdrawRepo
}

func NewWithdrawManager(repo repo.WithdrawRepo) *WithdrawManager {
	return &WithdrawManager{repo: repo}
}

func (m *WithdrawManager) Withdraw(ctx context.Context, userID int64, order string, amount float64) error {
	order = strings.TrimSpace(order)
	if err := ValidateOrderNumber(order); err != nil {
		return ErrInvalidOrderNumber
	}
	if amount <= 0 {
		return ErrInvalidWithdrawAmount
	}
	return m.repo.Withdraw(ctx, userID, order, amount)
}

func (m *WithdrawManager) ListUserWithdrawals(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
	if m == nil {
		return nil, nil
	}
	withdrawals, err := m.repo.ListWithdrawals(ctx, userID)
	if err != nil {
		return nil, err
	}
	if withdrawals == nil {
		return []repo.Withdrawal{}, nil
	}
	return withdrawals, nil
}
