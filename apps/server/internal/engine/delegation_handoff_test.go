package engine

import (
	"errors"
	"testing"
)

func TestDelegationHandoffErrorCarriesDelegation(t *testing.T) {
	delegation := Delegation{ID: "delegation-1", RunID: "run-2"}
	err := NewDelegationHandoff(delegation)

	if err.Error() != ErrDelegationHandoff.Error() {
		t.Fatalf("error=%q want %q", err.Error(), ErrDelegationHandoff.Error())
	}
	if !errors.Is(err, ErrDelegationHandoff) {
		t.Fatalf("error=%v does not unwrap to delegation handoff sentinel", err)
	}
	got, ok := AsDelegationHandoff(err)
	if !ok || got != delegation {
		t.Fatalf("delegation=%+v ok=%v want %+v", got, ok, delegation)
	}
}

func TestAsDelegationHandoffRejectsOtherAndTypedNilErrors(t *testing.T) {
	if got, ok := AsDelegationHandoff(errors.New("other")); ok || got != (Delegation{}) {
		t.Fatalf("other error delegation=%+v ok=%v", got, ok)
	}

	var typedNil *DelegationHandoffError
	if got, ok := AsDelegationHandoff(typedNil); ok || got != (Delegation{}) {
		t.Fatalf("typed nil delegation=%+v ok=%v", got, ok)
	}
}
