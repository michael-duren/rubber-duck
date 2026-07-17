package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRenderMainHelpListsEveryCommand guards against a command being added to
// dispatch but forgotten in the help registry (or vice versa): the top-level
// help must name every command, since it's the entry point for discovery.
func TestRenderMainHelpListsEveryCommand(t *testing.T) {
	var buf bytes.Buffer
	renderMainHelp(&buf)
	out := buf.String()
	for _, name := range []string{"pull", "test", "submit", "auth", "educator", "version", "help"} {
		if !strings.Contains(out, name) {
			t.Errorf("main help missing command %q", name)
		}
	}
	if !strings.Contains(out, "DUCK_TOKEN") {
		t.Error("main help should document DUCK_TOKEN")
	}
}

// TestRenderCmdHelpSections checks a detailed command page renders its title,
// usage, flags, and examples.
func TestRenderCmdHelpSections(t *testing.T) {
	c := findCmd("submit")
	if c == nil {
		t.Fatal("findCmd(submit) = nil")
	}
	var buf bytes.Buffer
	renderCmdHelp(&buf, *c)
	out := buf.String()
	for _, want := range []string{"duck submit", "Usage:", "--remote", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Errorf("submit help missing %q\n%s", want, out)
		}
	}
}

func TestFindCmd(t *testing.T) {
	cases := []struct {
		name string
		want string // resolved cmdHelp.name, or "" for nil
	}{
		{"pull", "pull"},
		{"educator", "educator"},
		{"ed", "educator"}, // alias resolves to the same entry
		{"auth", "auth"},
		{"login", "login"}, // deprecated alias resolves to the auth login sub
		{"nope", ""},
	}
	for _, c := range cases {
		got := findCmd(c.name)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("findCmd(%q) = %q, want nil", c.name, got.name)
		case c.want != "" && (got == nil || got.name != c.want):
			t.Errorf("findCmd(%q) = %v, want %q", c.name, got, c.want)
		}
	}
}

func TestHelpCmdTopics(t *testing.T) {
	cases := []struct {
		name    string
		topic   []string
		wantErr bool
	}{
		{"top level", nil, false},
		{"command", []string{"pull"}, false},
		{"educator subcommand", []string{"educator", "push"}, false},
		{"auth subcommand", []string{"auth", "status"}, false},
		{"unknown command", []string{"bogus"}, true},
		{"unknown subcommand", []string{"educator", "bogus"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := helpCmd(c.topic)
			if c.wantErr && err == nil {
				t.Errorf("helpCmd(%v) = nil, want error", c.topic)
			}
			if !c.wantErr && err != nil {
				t.Errorf("helpCmd(%v) = %v, want nil", c.topic, err)
			}
		})
	}
}

func TestHasHelpFlag(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--help"}, true},
		{[]string{"foo", "-h"}, true},
		{[]string{"foo", "--base", "x"}, false},
		{[]string{"help"}, false}, // a bare "help" positional is not a flag
		{nil, false},
	}
	for _, c := range cases {
		if got := hasHelpFlag(c.args); got != c.want {
			t.Errorf("hasHelpFlag(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
