package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"gophermart/repo"
)

var (
	ErrOrderOwnedByAnother   = errors.New("order belongs to another user")
	ErrInvalidOrderNumber    = errors.New("invalid order number")
	ErrInvalidWithdrawAmount = errors.New("invalid withdraw amount")
)

const newOrderStatus = "NEW"

type OrderManager struct {
	orders repo.OrderRepo
}

func NewOrderManager(orders repo.OrderRepo) *OrderManager {
	return &OrderManager{orders: orders}
}

func (m *OrderManager) SubmitOrder(ctx context.Context, userID int64, number string) (bool, error) {
	number = strings.TrimSpace(number)
	if err := ValidateOrderNumber(number); err != nil {
		return false, err
	}

	existing, err := m.orders.GetByNumber(ctx, number)
	if err != nil && !errors.Is(err, repo.ErrOrderNotFound) {
		return false, err
	}

	if existing != nil {
		if existing.UserID == userID {
			return false, nil
		}
		return false, ErrOrderOwnedByAnother
	}

	order := &repo.Order{
		Number:     number,
		UserID:     userID,
		Status:     newOrderStatus,
		UploadedAt: time.Now().UTC(),
	}

	if err := m.orders.CreateOrder(ctx, order); err != nil {
		if errors.Is(err, repo.ErrOrderExists) {
			existing, getErr := m.orders.GetByNumber(ctx, number)
			if getErr != nil {
				return false, getErr
			}
			if existing.UserID == userID {
				return false, nil
			}
			return false, ErrOrderOwnedByAnother
		}
		return false, err
	}

	if err := m.orders.UpsertProcessing(ctx, number, newOrderStatus); err != nil {
		return false, err
	}

	return true, nil
}

func ValidateOrderNumber(number string) error {
	trimmed := strings.TrimSpace(number)
	if trimmed == "" {
		return ErrInvalidOrderNumber
	}
	for _, r := range trimmed {
		if !unicode.IsDigit(r) {
			return ErrInvalidOrderNumber
		}
	}
	if !Luhn(trimmed) {
		return ErrInvalidOrderNumber
	}
	return nil
}

func Luhn(number string) bool {
	var sum int
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		d := int(number[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

var _ OrderService = (*OrderManager)(nil)

func (m *OrderManager) ListUserOrders(ctx context.Context, userID int64) ([]repo.Order, error) {
	if m == nil {
		return nil, nil
	}
	orders, err := m.orders.ListUserOrders(ctx, userID)
	if err != nil {
		return nil, err
	}
	if orders == nil {
		return []repo.Order{}, nil
	}
	return orders, nil
}
