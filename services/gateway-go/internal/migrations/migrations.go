// Package migrations holds the versioned Postgres schema (Phase 5 step 1) and
// applies it with goose. The SQL files are embedded in the gateway binary, so
// the gateway brings its database up to date when it starts: no separate tool
// or init script on the server.
//
// Add a change as the next numbered file (00002_<name>.sql) with a
// "-- +goose Up" and a "-- +goose Down" section. Never edit a file that has
// been merged: a database that already ran it would not run it again.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql
var files embed.FS

// Up applies every migration the database has not run yet. A Postgres session
// lock makes a second gateway starting at the same time wait instead of
// running the same migration twice.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := provider(db)
	if err != nil {
		return err
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	for _, r := range results {
		log.Printf("[gateway] migration %s applied in %s", r.Source.Path, r.Duration)
	}
	return nil
}

// Reset runs every Down section, newest first. Only the store integration
// tests use it, to start each test from an empty schema.
func Reset(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := provider(db)
	if err != nil {
		return err
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}

func provider(db *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return p, nil
}
