package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mduren/getcracked/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2)
		 RETURNING id, username, created_at`,
		username, passwordHash,
	).Scan(&u.ID, &u.Username, &u.CreatedAt)
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
		`SELECT id, username, created_at, password_hash FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, "", domain.ErrNotFound
	}
	return u, hash, err
}
