package migrations

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tempDatabase creates an empty database on the GATEWAY_TEST_POSTGRES_DSN
// server (CI sets it) and drops it after the test, so these tests never touch
// the schema the store integration tests use.
func tempDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GATEWAY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set GATEWAY_TEST_POSTGRES_DSN to run the migration tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "migtest_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	return pool
}

func version(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var v int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUp_EmptyDatabaseAndAgain(t *testing.T) {
	pool := tempDatabase(t)
	ctx := context.Background()
	if err := Up(ctx, pool); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if err := Up(ctx, pool); err != nil {
		t.Fatalf("second Up (nothing to do): %v", err)
	}
	if v := version(t, pool); v < 1 {
		t.Fatalf("version %d, want at least 1", v)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coaches (email, password_hash, display_name) VALUES ('a@b.c', 'x', 'A')`); err != nil {
		t.Fatalf("coaches table not usable: %v", err)
	}
}

// A database created by the old infra/postgres/init.sql (every local database
// made before Phase 5) keeps its rows and is recorded at version 1.
func TestUp_DatabaseFromOldInitSQL(t *testing.T) {
	pool := tempDatabase(t)
	ctx := context.Background()
	old, err := os.ReadFile("testdata/pre-phase5-init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(old)); err != nil {
		t.Fatalf("old init.sql: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO coaches (email, password_hash, display_name) VALUES ('old@b.c', 'x', 'Old')`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, pool); err != nil {
		t.Fatalf("Up on an init.sql database: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM coaches WHERE email = 'old@b.c'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("existing coach lost: n=%d err=%v", n, err)
	}
	if v := version(t, pool); v < 1 {
		t.Fatalf("version %d, want at least 1", v)
	}
}

// Two gateways starting together: the session lock makes one wait, and both succeed.
func TestUp_Concurrent(t *testing.T) {
	pool := tempDatabase(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Up(context.Background(), pool)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Up %d: %v", i, err)
		}
	}
}

func TestReset_ThenUp(t *testing.T) {
	pool := tempDatabase(t)
	ctx := context.Background()
	if err := Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := Reset(ctx, pool); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.coaches') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("coaches still there after Reset: %v %v", exists, err)
	}
	if err := Up(ctx, pool); err != nil {
		t.Fatalf("Up after Reset: %v", err)
	}
}
