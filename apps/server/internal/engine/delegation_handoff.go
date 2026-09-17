package engine

import "errors"

var ErrDelegationHandoff = errors.New("engine: delegation workspace handoff")

type DelegationHandoffError struct {
	Delegation Delegation
}

func (e *DelegationHandoffError) Error() string {
	return ErrDelegationHandoff.Error()
}

func (e *DelegationHandoffError) Unwrap() error {
	return ErrDelegationHandoff
}

func NewDelegationHandoff(delegation Delegation) error {
	return &DelegationHandoffError{Delegation: delegation}
}

func AsDelegationHandoff(err error) (Delegation, bool) {
	var handoff *DelegationHandoffError
	if !errors.As(err, &handoff) || handoff == nil {
		return Delegation{}, false
	}
	return handoff.Delegation, true
}
