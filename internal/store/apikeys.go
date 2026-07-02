package store

import "context"

func (s *Store) CreateAPIKey(ctx context.Context, name string, keyHash []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (name, key_hash) VALUES ($1, $2) RETURNING id`,
		name, keyHash,
	).Scan(&id)
	return id, err
}

// APIKeyValid reports whether an unrevoked key with this hash exists.
func (s *Store) APIKeyValid(ctx context.Context, keyHash []byte) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL)`,
		keyHash,
	).Scan(&ok)
	return ok, err
}
