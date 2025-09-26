package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type PGOrderRepository struct {
	db *sql.DB
}

func NewOrderRepository(db *sql.DB) *PGOrderRepository {
	return &PGOrderRepository{db: db}
}

func (r *PGOrderRepository) GetByNumber(ctx context.Context, number string) (*Order, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("repository not initialized")
	}

	const query = `
        SELECT number, user_id, status, accrual, uploaded_at, accrual_applied
        FROM gofirmart.orders
        WHERE number = $1
    `

	var (
		order   Order
		accrual sql.NullFloat64
	)

	err := r.db.QueryRowContext(ctx, query, number).Scan(
		&order.Number,
		&order.UserID,
		&order.Status,
		&accrual,
		&order.UploadedAt,
		&order.AccrualApplied,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("select order: %w", err)
	}

	if accrual.Valid {
		v := accrual.Float64
		order.Accrual = &v
	}

	return &order, nil
}

func (r *PGOrderRepository) CreateOrder(ctx context.Context, order *Order) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}

	const query = `
        INSERT INTO gofirmart.orders (number, user_id, status, accrual, uploaded_at, accrual_applied)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	var accrual any
	if order.Accrual != nil {
		accrual = *order.Accrual
	}

	_, err := r.db.ExecContext(ctx, query,
		order.Number,
		order.UserID,
		order.Status,
		accrual,
		order.UploadedAt,
		order.AccrualApplied,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrOrderExists
			}
		}
		return fmt.Errorf("insert order: %w", err)
	}

	return nil
}

func (r *PGOrderRepository) UpsertProcessing(ctx context.Context, number string, status string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}

	const query = `
        INSERT INTO gofirmart.processing_queue (number, last_check, status)
        VALUES ($1, $2, $3)
        ON CONFLICT (number) DO UPDATE
        SET status = EXCLUDED.status
    `

	if _, err := r.db.ExecContext(ctx, query, number, nil, status); err != nil {
		return fmt.Errorf("upsert processing queue: %w", err)
	}

	return nil
}

func (r *PGOrderRepository) ClaimNextForProcessing(ctx context.Context) (string, error) {
	if r == nil || r.db == nil {
		return "", fmt.Errorf("repository not initialized")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	const selectQuery = `
        SELECT number
        FROM gofirmart.processing_queue
        WHERE status IN ('NEW', 'PROCESSING')
        ORDER BY COALESCE(last_check, 'epoch'::timestamptz) ASC
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    `

	var number string
	if err = tx.QueryRowContext(ctx, selectQuery).Scan(&number); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNoQueueItems
		}
		return "", fmt.Errorf("select queue entry: %w", err)
	}

	checkedAt := time.Now().UTC()
	const updateQuery = `
        UPDATE gofirmart.processing_queue
        SET status = 'PROCESSING', last_check = $2
        WHERE number = $1
    `

	if _, err = tx.ExecContext(ctx, updateQuery, number, checkedAt); err != nil {
		return "", fmt.Errorf("update queue entry: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("commit queue tx: %w", err)
	}
	committed = true

	return number, nil
}

func (r *PGOrderRepository) UpdateOrderStatus(ctx context.Context, number string, status string, accrual *float64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}

	var (
		query string
		args  []any
	)

	if accrual != nil {
		query = `
            UPDATE gofirmart.orders
            SET status = $2, accrual = $3, accrual_applied = false
            WHERE number = $1
        `
		args = []any{number, status, *accrual}
	} else {
		query = `
            UPDATE gofirmart.orders
            SET status = $2, accrual_applied = false
            WHERE number = $1
        `
		args = []any{number, status}
	}

	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update order status: %w", err)
	}

	return nil
}

func (r *PGOrderRepository) UpdateProcessingStatus(ctx context.Context, number string, status string, checkedAt time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}

	const query = `
        UPDATE gofirmart.processing_queue
        SET status = $2, last_check = $3
        WHERE number = $1
    `

	if _, err := r.db.ExecContext(ctx, query, number, status, checkedAt); err != nil {
		return fmt.Errorf("update processing status: %w", err)
	}

	return nil
}

func (r *PGOrderRepository) DeleteProcessingEntry(ctx context.Context, number string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}

	const query = `
        DELETE FROM gofirmart.processing_queue
        WHERE number = $1
    `

	if _, err := r.db.ExecContext(ctx, query, number); err != nil {
		return fmt.Errorf("delete processing entry: %w", err)
	}

	return nil
}

func (r *PGOrderRepository) ListUserOrders(ctx context.Context, userID int64) ([]Order, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("repository not initialized")
	}

	const query = `
        SELECT number, user_id, status, accrual, uploaded_at, accrual_applied
        FROM gofirmart.orders
        WHERE user_id = $1
        ORDER BY uploaded_at DESC
    `

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("select user orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var (
			order   Order
			accrual sql.NullFloat64
		)
		if err := rows.Scan(&order.Number, &order.UserID, &order.Status, &accrual, &order.UploadedAt, &order.AccrualApplied); err != nil {
			return nil, fmt.Errorf("scan user order: %w", err)
		}
		if accrual.Valid {
			v := accrual.Float64
			order.Accrual = &v
		}
		orders = append(orders, order)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user orders: %w", err)
	}

	return orders, nil
}

func (r *PGOrderRepository) ApplyOrderAccrual(ctx context.Context, number string, accrual float64) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("repository not initialized")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	const selectQuery = `
        SELECT user_id, accrual_applied
        FROM gofirmart.orders
        WHERE number = $1
        FOR UPDATE
    `

	var (
		userID  int64
		applied bool
	)

	if err = tx.QueryRowContext(ctx, selectQuery, number).Scan(&userID, &applied); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrOrderNotFound
		}
		return false, fmt.Errorf("select order accrual: %w", err)
	}

	if applied {
		if _, err = tx.ExecContext(ctx, `DELETE FROM gofirmart.processing_queue WHERE number = $1`, number); err != nil {
			return false, fmt.Errorf("delete queue: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("commit tx: %w", err)
		}
		committed = true
		return false, nil
	}

	const updateOrder = `
        UPDATE gofirmart.orders
        SET status = 'PROCESSED', accrual = $2, accrual_applied = true
        WHERE number = $1
    `

	if _, err = tx.ExecContext(ctx, updateOrder, number, accrual); err != nil {
		return false, fmt.Errorf("update order accrual: %w", err)
	}

	const updateUser = `
        UPDATE gofirmart.users
        SET balance = balance + $2
        WHERE id = $1
    `

	if _, err = tx.ExecContext(ctx, updateUser, userID, accrual); err != nil {
		return false, fmt.Errorf("update user balance: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM gofirmart.processing_queue WHERE number = $1`, number); err != nil {
		return false, fmt.Errorf("delete queue: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit tx: %w", err)
	}
	committed = true

	return true, nil
}

var _ OrderRepo = (*PGOrderRepository)(nil)
