package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type PGWithdrawRepository struct {
	db *sql.DB
}

func NewWithdrawRepository(db *sql.DB) *PGWithdrawRepository {
	return &PGWithdrawRepository{db: db}
}

func (r *PGWithdrawRepository) Withdraw(ctx context.Context, userID int64, order string, amount float64) (err error) {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const selectQuery = `
        SELECT balance
        FROM gofirmart.users
        WHERE id = $1
        FOR UPDATE
    `

	var balance float64
	if scanErr := tx.QueryRowContext(ctx, selectQuery, userID).Scan(&balance); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			err = ErrUserNotFound
			return err
		}
		err = fmt.Errorf("select balance: %w", scanErr)
		return err
	}

	if balance < amount {
		err = ErrInsufficientFunds
		return err
	}

	const updateUser = `
        UPDATE gofirmart.users
        SET balance = balance - $2, withdrawn = withdrawn + $2
        WHERE id = $1
    `

	if _, execErr := tx.ExecContext(ctx, updateUser, userID, amount); execErr != nil {
		err = fmt.Errorf("update user balance: %w", execErr)
		return err
	}

	const insertWithdrawal = `
        INSERT INTO gofirmart.withdrawals (user_id, "order", sum, processed_at)
        VALUES ($1, $2, $3, NOW())
    `

	if _, execErr := tx.ExecContext(ctx, insertWithdrawal, userID, order, amount); execErr != nil {
		err = fmt.Errorf("insert withdrawal: %w", execErr)
		return err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("commit tx: %w", commitErr)
		return err
	}

	return nil
}

func (r *PGWithdrawRepository) ListWithdrawals(ctx context.Context, userID int64) ([]Withdrawal, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("repository not initialized")
	}

	const query = `
        SELECT user_id, "order", sum, processed_at
        FROM gofirmart.withdrawals
        WHERE user_id = $1
        ORDER BY processed_at DESC
    `

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("select withdrawals: %w", err)
	}
	defer rows.Close()

	var result []Withdrawal
	for rows.Next() {
		var w Withdrawal
		if err := rows.Scan(&w.UserID, &w.OrderNumber, &w.Sum, &w.ProcessedAt); err != nil {
			return nil, fmt.Errorf("scan withdrawal: %w", err)
		}
		result = append(result, w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate withdrawals: %w", err)
	}

	return result, nil
}

var _ WithdrawRepo = (*PGWithdrawRepository)(nil)
