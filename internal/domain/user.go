package domain

import "time"

type User struct {
	ID        int64
	Username  string
	CreatedAt time.Time
}
