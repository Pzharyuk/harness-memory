package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Pzharyuk/harness-memory/db"
)

// Migrations are embedded in package db; go:embed cannot use ".." paths.

// Store is the Postgres-backed memory store.
type Store struct {
	Pool *pgxpool.Pool
}

// Open connects to Postgres, applies embedded migrations in filename order,
// and upserts schema_meta.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is empty")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	st := &Store{Pool: pool}
	if err := st.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return st, nil
}

type migration struct {
	version int
	name    string
	sql     string
}

func (s *Store) migrate(ctx context.Context) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	cur, err := s.schemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("schema version: %w", err)
	}

	var pending []migration
	for _, m := range ms {
		if m.version > cur {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migrate: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, m := range pending {
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO schema_meta (id, version, instance_id)
			VALUES (1, $1, gen_random_uuid())
			ON CONFLICT (id) DO UPDATE SET version = EXCLUDED.version
		`, m.version); err != nil {
			return fmt.Errorf("record %s: %w", m.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrate: %w", err)
	}
	return nil
}

func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.Pool.QueryRow(ctx, `select version from schema_meta`).Scan(&v)
	if err == nil {
		return v, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return 0, nil
	}
	return 0, err
}

func loadMigrations() ([]migration, error) {
	var ms []migration
	err := fs.WalkDir(db.Migrations, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".sql") {
			return nil
		}
		ver, err := versionFromFilename(path.Base(p))
		if err != nil {
			return err
		}
		body, err := fs.ReadFile(db.Migrations, p)
		if err != nil {
			return err
		}
		ms = append(ms, migration{version: ver, name: path.Base(p), sql: string(body)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	if len(ms) == 0 {
		return nil, fmt.Errorf("no migrations embedded")
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].name != ms[j].name {
			return ms[i].name < ms[j].name
		}
		return ms[i].version < ms[j].version
	})
	seen := make(map[int]string, len(ms))
	for _, m := range ms {
		if prev, ok := seen[m.version]; ok {
			return nil, fmt.Errorf("duplicate migration version %d (%s and %s)", m.version, prev, m.name)
		}
		seen[m.version] = m.name
	}
	return ms, nil
}

func versionFromFilename(name string) (int, error) {
	num, rest, ok := strings.Cut(name, "_")
	if !ok || rest == "" {
		return 0, fmt.Errorf("migration %q: want NNN_name.sql", name)
	}
	v, err := strconv.Atoi(num)
	if err != nil || v < 1 {
		return 0, fmt.Errorf("migration %q: invalid version", name)
	}
	return v, nil
}
