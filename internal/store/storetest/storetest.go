// Package storetest gives tests a migrated store in a fresh Postgres schema.
// Set SIMPLECD_TEST_DATABASE_URL to run them; without it they are skipped.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	neturl "net/url"
	"os"
	"testing"

	"github.com/Mtze/simple-cd/internal/store"
	"github.com/jackc/pgx/v5"
)

func New(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("SIMPLECD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIMPLECD_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	b := make([]byte, 6)
	rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		conn.Close(context.Background())
	})

	u, err := neturl.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
