package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

func (s *Store) CreateUserToken(ctx context.Context, userID int64, name string, tokenHash []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_tokens (user_id, name, token_hash) VALUES ($1, $2, $3) RETURNING id`,
		userID, name, tokenHash,
	).Scan(&id)
	return id, err
}

// UserByToken resolves an unrevoked CLI token hash to its owning user.
func (s *Store) UserByToken(ctx context.Context, tokenHash []byte) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.username, u.role, u.created_at
		 FROM user_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = $1 AND t.revoked_at IS NULL`,
		tokenHash,
	).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// ListUserTokens returns a user's tokens, newest first.
func (s *Store) ListUserTokens(ctx context.Context, userID int64) ([]domain.UserToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at, revoked_at FROM user_tokens
		 WHERE user_id = $1 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []domain.UserToken
	for rows.Next() {
		var t domain.UserToken
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// RevokeUserToken revokes a token, scoped to its owner so users cannot
// revoke each other's tokens.
func (s *Store) RevokeUserToken(ctx context.Context, userID, tokenID int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE user_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		tokenID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
