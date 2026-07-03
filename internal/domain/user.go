package domain

import "time"

type User struct {
	ID        int64
	Username  string
	CreatedAt time.Time
}

// UserToken is a CLI bearer token minted from the profile page.
type UserToken struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	RevokedAt *time.Time
}
