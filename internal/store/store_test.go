package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mduren/getcracked/internal/auth"
	"github.com/mduren/getcracked/internal/domain"
)

// testStore migrates the test database from scratch and returns an open store.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration`")
	}
	if err := Migrate(url, true); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := Migrate(url, false); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestUsers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "alice", "hash1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == 0 || u.Username != "alice" {
		t.Errorf("unexpected user %+v", u)
	}

	// citext: same name, different case, must collide
	if _, err := s.CreateUser(ctx, "ALICE", "hash2"); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Errorf("dup create err = %v, want ErrUsernameTaken", err)
	}

	got, hash, err := s.UserByUsername(ctx, "alice")
	if err != nil || got.ID != u.ID || hash != "hash1" {
		t.Errorf("lookup = %+v, %q, %v", got, hash, err)
	}

	if _, _, err := s.UserByUsername(ctx, "nobody"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing user err = %v, want ErrNotFound", err)
	}
}

func TestSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "bob", "h")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	cases := []struct {
		name      string
		expiresAt time.Time
		wantErr   error
	}{
		{"valid", time.Now().Add(time.Hour), nil},
		{"expired", time.Now().Add(-time.Hour), domain.ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, hash := auth.NewSessionToken()
			if err := s.CreateSession(ctx, hash, u.ID, c.expiresAt); err != nil {
				t.Fatalf("create session: %v", err)
			}
			got, err := s.UserBySession(ctx, hash)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr == nil && got.ID != u.ID {
				t.Errorf("user = %+v, want id %d", got, u.ID)
			}
		})
	}

	_, hash := auth.NewSessionToken()
	if err := s.CreateSession(ctx, hash, u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted session err = %v, want ErrNotFound", err)
	}
}

func TestAPIKeys(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	key, hash := auth.NewAPIKey()
	if _, err := s.CreateAPIKey(ctx, "test", hash); err != nil {
		t.Fatalf("create: %v", err)
	}

	ok, err := s.APIKeyValid(ctx, auth.HashToken(key))
	if err != nil || !ok {
		t.Errorf("valid = %v, %v; want true", ok, err)
	}
	ok, err = s.APIKeyValid(ctx, auth.HashToken("gc_wrong"))
	if err != nil || ok {
		t.Errorf("invalid key = %v, %v; want false", ok, err)
	}
}
