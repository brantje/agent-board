package executioncontext

import "testing"

func TestSameJSONPreservesLargeIntegerPrecision(t *testing.T) {
	left := []byte(`{"value":9007199254740992}`)
	right := []byte(`{"value":9007199254740993}`)
	if sameJSON(left, right) {
		t.Fatal("distinct large JSON integers compared equal")
	}
	if !sameJSON([]byte(`{"value":1}`), []byte(`{"value":1.0}`)) {
		t.Fatal("numerically equivalent JSON values compared different")
	}
}
