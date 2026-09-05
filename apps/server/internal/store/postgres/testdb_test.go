package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testDatabaseURLEnv   = "AGENT_BOARD_TEST_DATABASE_URL"
	testDatabaseResetEnv = "AGENT_BOARD_TEST_DATABASE_RESET"
	testDatabaseName     = "agent_board_test"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv(testDatabaseURLEnv)
	if databaseURL == "" {
		t.Skipf("%s is not set; PostgreSQL integration test skipped", testDatabaseURLEnv)
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open test PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping test PostgreSQL: %v", err)
	}
	resetSchema(t, pool)
	return pool
}

func resetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("read test database identity: %v", err)
	}
	if err := validateTestDatabaseReset(databaseName, os.Getenv(testDatabaseResetEnv)); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}

	schemaPath := filepath.Join("..", "..", "..", "..", "..", "packages", "database", "schema.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read canonical schema %s: %v", schemaPath, err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply canonical schema: %v", err)
	}
}

func validateTestDatabaseReset(databaseName, optIn string) error {
	if optIn != "1" {
		return fmt.Errorf("refusing destructive PostgreSQL test reset: set %s=1 explicitly", testDatabaseResetEnv)
	}
	if databaseName != testDatabaseName {
		return fmt.Errorf("refusing destructive PostgreSQL test reset for database %q; expected %q", databaseName, testDatabaseName)
	}
	return nil
}
