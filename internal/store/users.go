package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2)
		 RETURNING id, username, role, created_at`,
		username, passwordHash,
	).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return domain.User{}, domain.ErrUsernameTaken
	}
	return u, err
}

// UserByUsername returns the user and their password hash for login checks.
func (s *Store) UserByUsername(ctx context.Context, username string) (domain.User, string, error) {
	var u domain.User
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, role, created_at, password_hash FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, "", domain.ErrNotFound
	}
	return u, hash, err
}

// PasswordHash returns a user's current password hash, for verifying the
// "current password" field on a change-password form.
func (s *Store) PasswordHash(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return hash, err
}

func (s *Store) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, passwordHash, userID)
	return err
}

// PromoteUser sets a user's role by username. Role changes are an operator
// action (`duckserver user promote`), not a web flow, so lookup is by the
// name the operator sees rather than an internal id.
func (s *Store) PromoteUser(ctx context.Context, username, role string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET role = $1 WHERE username = $2`, role, username)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
