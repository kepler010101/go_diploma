package repo

import (
	"context"
	"database/sql"
	"fmt"
)

func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	statements := []string{
		"CREATE SCHEMA IF NOT EXISTS gofirmart",
		"CREATE SCHEMA IF NOT EXISTS accrual",
		`CREATE TABLE IF NOT EXISTS gofirmart.users (
            id SERIAL PRIMARY KEY,
            login TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL,
            balance NUMERIC(18,2) DEFAULT 0,
            withdrawn NUMERIC(18,2) DEFAULT 0
        )`,
		`CREATE TABLE IF NOT EXISTS gofirmart.orders (
            number TEXT PRIMARY KEY,
            user_id INT NOT NULL,
            status TEXT NOT NULL,
            accrual NUMERIC(18,2),
            uploaded_at TIMESTAMPTZ NOT NULL,
            accrual_applied BOOLEAN NOT NULL DEFAULT false
        )`,
		`CREATE TABLE IF NOT EXISTS gofirmart.withdrawals (
            id SERIAL PRIMARY KEY,
            user_id INT NOT NULL,
            "order" TEXT NOT NULL,
            sum NUMERIC(18,2) NOT NULL,
            processed_at TIMESTAMPTZ NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS gofirmart.processing_queue (
            number TEXT PRIMARY KEY,
            last_check TIMESTAMPTZ,
            status TEXT NOT NULL
        )`,
		`ALTER TABLE gofirmart.orders ADD COLUMN IF NOT EXISTS accrual_applied BOOLEAN NOT NULL DEFAULT false`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("run migrate statement %q: %w", stmt, err)
		}
	}

	return nil
}
