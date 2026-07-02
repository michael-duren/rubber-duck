package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mduren/getcracked/internal/auth"
	"github.com/mduren/getcracked/internal/grader"
	"github.com/mduren/getcracked/internal/grader/dockergrader"
	"github.com/mduren/getcracked/internal/httpapi"
	"github.com/mduren/getcracked/internal/ingest"
	"github.com/mduren/getcracked/internal/store"
	"github.com/mduren/getcracked/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "getcracked:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: getcracked <serve|migrate|apikey> [flags]")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "migrate":
		return migrateCmd(args[1:])
	case "apikey":
		return apikeyCmd(args[1:])
	case "seed":
		return seedCmd(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func databaseURL() string {
	return envOr("DATABASE_URL", "postgres://getcracked:getcracked@localhost:5432/getcracked?sslmode=disable")
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", envOr("GC_ADDR", ":8080"), "listen address")
	dbURL := fs.String("db", databaseURL(), "postgres connection URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := store.Migrate(*dbURL, false); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	st, err := store.Open(context.Background(), *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	pool := grader.NewPool(dockergrader.New(), st, logger, 2, 60*time.Second)
	if err := pool.Recover(context.Background()); err != nil {
		return fmt.Errorf("requeue pending submissions: %w", err)
	}

	mux := http.NewServeMux()
	web.Register(mux, logger, st, st, st, pool)
	httpapi.Register(mux, logger, st, st)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("listening", "addr", *addr)
	return srv.ListenAndServe()
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
		return fmt.Errorf("usage: getcracked migrate [up|down]")
	}
}

func apikeyCmd(args []string) error {
	if len(args) < 1 || args[0] != "create" {
		return fmt.Errorf("usage: getcracked apikey create --name <name>")
	}
	fs := flag.NewFlagSet("apikey create", flag.ContinueOnError)
	name := fs.String("name", "", "key name, e.g. writer-1")
	dbURL := fs.String("db", databaseURL(), "postgres connection URL")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("usage: getcracked apikey create --name <name>")
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	key, hash := auth.NewAPIKey()
	if _, err := st.CreateAPIKey(ctx, *name, hash); err != nil {
		return err
	}
	fmt.Printf("api key %q (shown once, store it now):\n%s\n", *name, key)
	return nil
}

// seedCmd publishes a course document through the agent API, exercising the
// same path agents use. It mints a throwaway key unless GC_API_KEY is set.
func seedCmd(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	baseURL := fs.String("url", envOr("GC_URL", "http://localhost:8080"), "server base URL")
	dbURL := fs.String("db", databaseURL(), "postgres connection URL (for key minting)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: getcracked seed <course.md>")
	}

	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	res, err := ingest.Parse(src)
	if err != nil {
		return fmt.Errorf("%s: %w", fs.Arg(0), err)
	}

	key := os.Getenv("GC_API_KEY")
	if key == "" {
		ctx := context.Background()
		st, err := store.Open(ctx, *dbURL)
		if err != nil {
			return err
		}
		defer st.Close()
		var hash []byte
		key, hash = auth.NewAPIKey()
		if _, err := st.CreateAPIKey(ctx, "seed", hash); err != nil {
			return err
		}
	}

	body, err := json.Marshal(map[string]string{"markdown": string(src)})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/v1/courses/%s/variants/%s", *baseURL, res.Course.Course, res.Course.Language)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("%s %s\n%s", resp.Status, url, out)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("seed failed")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
