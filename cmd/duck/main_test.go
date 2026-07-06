package main

import (
	"flag"
	"io"
	"slices"
	"testing"
)

func TestParseInterleaved(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPos  []string
		wantBase string
		wantErr  bool
	}{
		{"flags first", []string{"--base", "http://x", "course/go"}, []string{"course/go"}, "http://x", false},
		{"flags after positional (the documented order)", []string{"course/go", "--base", "http://x"}, []string{"course/go"}, "http://x", false},
		{"bool flag after positional", []string{"course/go", "--remote"}, []string{"course/go"}, "", false},
		{"no flags", []string{"course/go"}, []string{"course/go"}, "", false},
		{"no args", nil, nil, "", false},
		{"unknown flag errors", []string{"course/go", "--nope"}, nil, "", true},
		{"missing flag value errors", []string{"course/go", "--base"}, nil, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			base := fs.String("base", "", "")
			fs.Bool("remote", false, "")

			pos, err := parseInterleaved(fs, c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseInterleaved(%v) = %v, want error", c.args, pos)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInterleaved(%v): %v", c.args, err)
			}
			if !slices.Equal(pos, c.wantPos) {
				t.Errorf("positionals = %v, want %v", pos, c.wantPos)
			}
			if *base != c.wantBase {
				t.Errorf("base = %q, want %q", *base, c.wantBase)
			}
		})
	}
}
