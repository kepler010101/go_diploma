package repo

import (
	"context"
	"errors"
	"time"
)

type User struct {
	ID           int64
	Login        string
	PasswordHash string
	Balance      float64
	Withdrawn    float64
}

type Order struct {
	Number         string
	UserID         int64
	Status         string
	Accrual        *float64
	UploadedAt     time.Time
	AccrualApplied bool
}

type Withdrawal struct {
	ID          int64
	UserID      int64
	OrderNumber string
	Sum         float64
	ProcessedAt time.Time
}

var (
	ErrUserExists        = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrOrderNotFound     = errors.New("order not found")
	ErrOrderExists       = errors.New("order already exists")
	ErrNoQueueItems      = errors.New("no items in processing queue")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

type UserRepo interface {
	CreateUser(ctx context.Context, user *User) error
	GetByLogin(ctx context.Context, login string) (*User, error)
	GetBalance(ctx context.Context, userID int64) (float64, float64, error)
}

type OrderRepo interface {
	GetByNumber(ctx context.Context, number string) (*Order, error)
	CreateOrder(ctx context.Context, order *Order) error
	UpsertProcessing(ctx context.Context, number string, status string) error
	ClaimNextForProcessing(ctx context.Context) (string, error)
	UpdateOrderStatus(ctx context.Context, number string, status string, accrual *float64) error
	UpdateProcessingStatus(ctx context.Context, number string, status string, checkedAt time.Time) error
	DeleteProcessingEntry(ctx context.Context, number string) error
	ListUserOrders(ctx context.Context, userID int64) ([]Order, error)
	ApplyOrderAccrual(ctx context.Context, number string, accrual float64) (bool, error)
}

type WithdrawRepo interface {
	Withdraw(ctx context.Context, userID int64, order string, amount float64) error
	ListWithdrawals(ctx context.Context, userID int64) ([]Withdrawal, error)
}
