package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type PGUserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *PGUserRepository {
	return &PGUserRepository{db: db}
}

func (r *PGUserRepository) CreateUser(ctx context.Context, user *User) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("repository not initialized")
	}
	query := `
        INSERT INTO gofirmart.users (login, password_hash)
        VALUES ($1, $2)
        RETURNING id, balance, withdrawn
    `
	row := r.db.QueryRowContext(ctx, query, user.Login, user.PasswordHash)
	if err := row.Scan(&user.ID, &user.Balance, &user.Withdrawn); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" {
				return ErrUserExists
			}
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PGUserRepository) GetByLogin(ctx context.Context, login string) (*User, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	query := `
        SELECT id, login, password_hash, balance, withdrawn
        FROM gofirmart.users
        WHERE login = $1
    `
	user := &User{}
	err := r.db.QueryRowContext(ctx, query, login).Scan(
		&user.ID,
		&user.Login,
		&user.PasswordHash,
		&user.Balance,
		&user.Withdrawn,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("select user: %w", err)
	}
	return user, nil
}

func (r *PGUserRepository) GetBalance(ctx context.Context, userID int64) (float64, float64, error) {
	if r == nil || r.db == nil {
		return 0, 0, fmt.Errorf("repository not initialized")
	}

	const query = `
        SELECT balance, withdrawn
        FROM gofirmart.users
        WHERE id = $1
    `

	var balance, withdrawn float64
	if err := r.db.QueryRowContext(ctx, query, userID).Scan(&balance, &withdrawn); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrUserNotFound
		}
		return 0, 0, fmt.Errorf("select balance: %w", err)
	}

	return balance, withdrawn, nil
}

var _ UserRepo = (*PGUserRepository)(nil)
