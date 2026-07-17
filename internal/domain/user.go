package domain

import "time"

// User roles. Admins can publish or reject any course proposal outright;
// regular users need the approval threshold (see web.Register).
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

type User struct {
	ID        int64
	Username  string
	Role      string
	CreatedAt time.Time
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// UserToken is a CLI bearer token minted from the profile page.
type UserToken struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	RevokedAt *time.Time
}
