package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/grader"
	"github.com/michael-duren/rubber-duck/internal/grader/cloudrungrader"
	"github.com/michael-duren/rubber-duck/internal/grader/dockergrader"
	"github.com/michael-duren/rubber-duck/internal/httpapi"
	"github.com/michael-duren/rubber-duck/internal/ingest"
	"github.com/michael-duren/rubber-duck/internal/store"
	"github.com/michael-duren/rubber-duck/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "duckserver:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: duckserver <serve|migrate|user|seed> [flags]")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "migrate":
		return migrateCmd(args[1:])
	case "user":
		return userCmd(args[1:])
	case "seed":
		return seedCmd(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func databaseURL() string {
	return envOr("DATABASE_URL", "postgres://duckserver:duckserver@localhost:5432/duckserver?sslmode=disable")
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	// Cloud Run's contract is the PORT env var; GC_ADDR/-addr still win.
	addr := fs.String("addr", envOr("GC_ADDR", ":"+envOr("PORT", "8080")), "listen address")
	dbURL := fs.String("db", databaseURL(), "postgres connection URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := store.Migrate(*dbURL, false); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	st, err := store.Open(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	g, gradeTimeout, err := newGrader(ctx, logger)
	if err != nil {
		return err
	}
	pool := grader.NewPool(g, st, logger, 2, gradeTimeout)
	if err := pool.Recover(ctx); err != nil {
		return fmt.Errorf("requeue pending submissions: %w", err)
	}

	// How many community approvals publish a proposal (an admin approval
	// always publishes immediately).
	threshold := 3
	if v := os.Getenv("GC_APPROVAL_THRESHOLD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return fmt.Errorf("GC_APPROVAL_THRESHOLD must be a positive integer, got %q", v)
		}
		threshold = n
	}

	mux := http.NewServeMux()
	web.Register(mux, logger, st, st, st, st, pool, threshold)
	httpapi.Register(mux, logger, st, st, st)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           web.CanonicalHost(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("listening", "addr", *addr, "grader", envOr("GC_GRADER", "docker"))

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// newGrader picks the grading backend from GC_GRADER. Cloud Run Jobs need a
// much larger budget than local docker: execution scheduling and container
// cold start happen before the task's own 90s limit even begins.
func newGrader(ctx context.Context, logger *slog.Logger) (grader.Grader, time.Duration, error) {
	switch backend := envOr("GC_GRADER", "docker"); backend {
	case "docker":
		return dockergrader.New(), 60 * time.Second, nil
	case "cloudrun":
		cfg := cloudrungrader.Config{
			Project: os.Getenv("GC_PROJECT"),
			Region:  os.Getenv("GC_REGION"),
			Bucket:  os.Getenv("GC_GRADING_BUCKET"),
		}
		if cfg.Project == "" || cfg.Region == "" || cfg.Bucket == "" {
			return nil, 0, fmt.Errorf("GC_GRADER=cloudrun requires GC_PROJECT, GC_REGION, and GC_GRADING_BUCKET")
		}
		g, err := cloudrungrader.New(ctx, cfg, logger)
		if err != nil {
			return nil, 0, err
		}
		return g, 180 * time.Second, nil
	default:
		return nil, 0, fmt.Errorf("unknown GC_GRADER %q (want docker or cloudrun)", backend)
	}
}

func migrateCmd(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dbURL := fs.String("db", databaseURL(), "postgres connection URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := fs.Arg(0)
	switch dir {
	case "up", "":
		return store.Migrate(*dbURL, false)
	case "down":
		return store.Migrate(*dbURL, true)
	default:
		return fmt.Errorf("usage: duckserver migrate [up|down]")
	}
}

// userCmd holds operator actions on accounts. Promotion is deliberately not
// a web flow: the first admin has to come from somewhere outside the app,
// and keeping it CLI-only means there is no privilege-escalation surface to
// defend in the web layer.
func userCmd(args []string) error {
	if len(args) < 1 || args[0] != "promote" {
		return fmt.Errorf("usage: duckserver user promote --username <username> [--role admin|user]")
	}
	fs := flag.NewFlagSet("user promote", flag.ContinueOnError)
	username := fs.String("username", "", "account to change")
	role := fs.String("role", "admin", "role to assign: admin or user")
	dbURL := fs.String("db", databaseURL(), "postgres connection URL")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *username == "" {
		return fmt.Errorf("usage: duckserver user promote --username <username> [--role admin|user]")
	}
	if *role != "admin" && *role != "user" {
		return fmt.Errorf("--role must be admin or user, got %q", *role)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.PromoteUser(ctx, *username, *role); err != nil {
		return err
	}
	fmt.Printf("user %q is now role %q\n", *username, *role)
	return nil
}

// seedCmd imports a course document straight into the database, bypassing
// the proposal workflow — the bootstrap path for a fresh local database
// (`make seed`) and the documented break-glass import for prod (run against
// a cloud-sql-proxy URL). Unattributed (editedBy nil) like agent publishes
// used to be, and idempotent: a document byte-identical to what's stored is
// skipped entirely, so re-running the full seed loop doesn't bump variant
// versions and spuriously trigger the "completed before the course was
// updated" notice on submissions.
func seedCmd(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	dbURL := fs.String("db", databaseURL(), "postgres connection URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: duckserver seed [--db <url>] <course.md>")
	}

	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	res, err := ingest.Parse(src)
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}
	course, variant, err := ingest.ToDomain(res, src)
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	stored, _, err := st.VariantSource(ctx, course.Slug, variant.Language)
	if err == nil && stored == variant.SourceMD {
		fmt.Printf("unchanged %s/%s\n", course.Slug, variant.Language)
		return nil
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	version, err := st.UpsertVariant(ctx, course, variant, nil, nil)
	if err != nil {
		return err
	}
	fmt.Printf("seeded %s/%s version %d\n", course.Slug, variant.Language, version)
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
