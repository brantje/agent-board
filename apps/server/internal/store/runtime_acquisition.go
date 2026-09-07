package store

import "context"

// RuntimeAcquisitionLock serializes Runtime reuse/provisioning for one durable
// Workspace and Runtime pair across server processes. Release must be safe to
// call more than once.
type RuntimeAcquisitionLock interface {
	Release() error
}

// RuntimeAcquisitionStore provides the distributed ownership boundary used by
// the application Runtime acquisition flow.
type RuntimeAcquisitionStore interface {
	AcquireRuntimeAcquisitionLock(context.Context, string, string) (RuntimeAcquisitionLock, error)
}
