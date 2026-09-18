package store

import "context"

// DelegationTarget is the bounded server-owned identity exposed to an
// authoritative Agent as a possible target. RequestDelegation remains
// authoritative and revalidates the target when the tool is invoked.
type DelegationTarget struct {
	ID   string
	Name string
}

// DelegationTargetStore returns the current Project-visible Agents whose
// execution configuration satisfies the same runnable predicate used by
// canonical delegation acceptance. Transient capacity is deliberately not part
// of this discovery snapshot.
type DelegationTargetStore interface {
	ListDelegationTargets(context.Context, string, string) ([]DelegationTarget, error)
}
