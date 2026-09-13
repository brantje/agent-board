package postgres

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"
)

func TestAuthSessionRefreshHistoryStoresOnlyHashes(t *testing.T) {
	pool := testPool(t)
	s := New(pool)
	now := time.Now().UTC()
	service := newPostgresAuthTestService(t, s, &now)
	bootstrapPostgresAuthTestUser(t, service)

	initial, err := service.Login(t.Context(), "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	rotated, err := service.Refresh(t.Context(), initial.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(t.Context(), `SELECT token_hash FROM auth_session_refresh_tokens`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	stored := make([][]byte, 0, 2)
	for rows.Next() {
		var hash []byte
		if err := rows.Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if len(hash) != sha256.Size {
			t.Fatalf("stored refresh generation hash length=%d want %d", len(hash), sha256.Size)
		}
		stored = append(stored, append([]byte(nil), hash...))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored refresh generations=%d want 2", len(stored))
	}

	initialHash := sha256.Sum256([]byte(initial.RefreshToken))
	rotatedHash := sha256.Sum256([]byte(rotated.RefreshToken))
	for _, expected := range [][]byte{initialHash[:], rotatedHash[:]} {
		found := false
		for _, hash := range stored {
			if bytes.Equal(hash, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected refresh-token hash was not retained")
		}
	}
	for _, raw := range [][]byte{[]byte(initial.RefreshToken), []byte(rotated.RefreshToken)} {
		for _, hash := range stored {
			if bytes.Equal(hash, raw) {
				t.Fatal("raw refresh token was persisted in refresh-generation history")
			}
		}
	}
}
