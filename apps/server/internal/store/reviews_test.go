package store

import "testing"

type reviewStoreWithoutCapability struct{ ReviewStore }

type reviewStoreWithCapability struct {
	ReviewStore
	enabled bool
}

func (s reviewStoreWithCapability) SupportsReviewStore() bool { return s.enabled }

func TestSupportsReviewStoreHonorsCapability(t *testing.T) {
	if SupportsReviewStore(nil) {
		t.Fatal("nil value should not support ReviewStore")
	}
	if SupportsReviewStore(struct{}{}) {
		t.Fatal("unrelated value should not support ReviewStore")
	}
	if !SupportsReviewStore(reviewStoreWithoutCapability{}) {
		t.Fatal("ReviewStore without capability marker should be supported")
	}
	if SupportsReviewStore(reviewStoreWithCapability{enabled: false}) {
		t.Fatal("disabled ReviewStore capability should not be supported")
	}
	if !SupportsReviewStore(reviewStoreWithCapability{enabled: true}) {
		t.Fatal("enabled ReviewStore capability should be supported")
	}
}
