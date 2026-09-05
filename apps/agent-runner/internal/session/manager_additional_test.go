package session

import (
	"context"
	"reflect"
	"testing"
)

func TestManagerActiveIDsAreSortedAndCapacityIsConfigurable(t *testing.T) {
	manager := NewManagerWithWorkspace(2, t.TempDir())
	second, err := manager.Start("b", Request{Command: []string{"sh", "-c", "sleep 30"}})
	if err != nil { t.Fatal(err) }
	first, err := manager.Start("a", Request{Command: []string{"sh", "-c", "sleep 30"}})
	if err != nil { t.Fatal(err) }
	if got, want := manager.ActiveIDs(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active ids got %v want %v", got, want)
	}
	if err := first.Kill(); err != nil { t.Fatal(err) }
	if err := second.Kill(); err != nil { t.Fatal(err) }
	if _, err := first.Wait(context.Background()); err != nil { t.Fatal(err) }
	if _, err := second.Wait(context.Background()); err != nil { t.Fatal(err) }
}
