package store

import (
	"context"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	st := openTest(t)
	var v int
	if err := st.Pool.QueryRow(context.Background(), `select version from schema_meta`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v < 1 {
		t.Fatalf("version=%d", v)
	}
	for _, table := range []string{"sources", "memories", "wiki_pages", "wiki_links", "revisions", "proposals", "tokens", "audit_log", "schema_meta"} {
		var n int
		q := `select count(*) from information_schema.tables where table_name=$1`
		if err := st.Pool.QueryRow(context.Background(), q, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing table %s: %v n=%d", table, err, n)
		}
	}
}
