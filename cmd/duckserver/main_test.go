package main

import (
	"strings"
	"testing"
)

// TestUserCmdValidation covers every refusal userCmd makes before touching
// the database — all of these must fail fast with usage guidance, never
// reach store.Open.
func TestUserCmdValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no subcommand", nil, "usage:"},
		{"unknown subcommand", []string{"demote"}, "usage:"},
		{"missing username", []string{"promote"}, "usage:"},
		{"bad role", []string{"promote", "--username", "alice", "--role", "root"}, "--role must be admin or user"},
		// stdlib flag stops at the first positional, so --db here would be
		// silently ignored; userCmd must refuse instead.
		{"flags after positional", []string{"promote", "--username", "alice", "extra", "--db", "x"}, "usage:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := userCmd(c.args)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("userCmd(%v) = %v, want error containing %q", c.args, err, c.want)
			}
		})
	}
}

func TestApprovalThreshold(t *testing.T) {
	cases := []struct {
		env     string
		want    int
		wantErr bool
	}{
		{"", 3, false}, // the documented default
		{"1", 1, false},
		{"5", 5, false},
		{"0", 0, true},
		{"-2", 0, true},
		{"lots", 0, true},
	}
	for _, c := range cases {
		t.Run("GC_APPROVAL_THRESHOLD="+c.env, func(t *testing.T) {
			t.Setenv("GC_APPROVAL_THRESHOLD", c.env)
			got, err := approvalThreshold()
			if (err != nil) != c.wantErr || got != c.want {
				t.Errorf("approvalThreshold() = %d, %v; want %d, err=%v", got, err, c.want, c.wantErr)
			}
		})
	}
}
