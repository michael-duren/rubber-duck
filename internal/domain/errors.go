package domain

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrUsernameTaken = errors.New("username already taken")

	// ErrVersionConflict is returned by store.UpsertVariant when a caller
	// passes a non-nil expectedVersion that no longer matches the stored
	// version — i.e. someone else's write landed first. Callers must not
	// retry blindly; the caller re-fetching and re-presenting is the only
	// safe recovery (see internal/web's saveVariant and httpapi.putVariant's
	// version_conflict response, which `duck educator push` surfaces).
	ErrVersionConflict = errors.New("variant was changed by someone else since it was loaded")
)
