package engine

import (
	"context"
	"testing"
)

type stubEngine string

func (s stubEngine) Name() string                                     { return string(s) }
func (s stubEngine) Execute(context.Context, Request) (Result, error) { return Result{}, nil }

type pointerStubEngine struct{}

func (*pointerStubEngine) Name() string                                     { return "pointer" }
func (*pointerStubEngine) Execute(context.Context, Request) (Result, error) { return Result{}, nil }

func TestRegistry(t *testing.T) {
	r, err := NewRegistry(stubEngine("scripted"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Get("scripted")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "scripted" {
		t.Fatalf("got %q", got.Name())
	}
	if _, err := r.Get("missing"); err == nil {
		t.Fatal("expected missing adapter error")
	}
	if !r.Has(" scripted ") || r.Has("missing") {
		t.Fatal("registry membership does not match Get")
	}
	var nilRegistry *Registry
	if nilRegistry.Has("scripted") {
		t.Fatal("nil registry reported an adapter")
	}
}

func TestRegistryRejectsDuplicateAndEmptyAdapters(t *testing.T) {
	if _, err := NewRegistry(stubEngine("")); err == nil {
		t.Fatal("expected empty name error")
	}
	if _, err := NewRegistry(stubEngine("scripted"), stubEngine("scripted")); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistryRejectsTypedNilAdapter(t *testing.T) {
	var adapter *pointerStubEngine
	if _, err := NewRegistry(adapter); err == nil {
		t.Fatal("expected typed-nil adapter error")
	}
}
