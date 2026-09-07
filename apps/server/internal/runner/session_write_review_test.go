package runner

import (
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func TestLockWriteMutexUntilFailsClosedBeforeAcquisition(t *testing.T) {
	tests := []struct {
		name string
		setup func() (time.Time, <-chan struct{}, <-chan struct{}, <-chan struct{})
		want error
	}{
		{
			name: "expired deadline",
			setup: func() (time.Time, <-chan struct{}, <-chan struct{}, <-chan struct{}) {
				return time.Now().Add(-time.Millisecond), make(chan struct{}), make(chan struct{}), make(chan struct{})
			},
			want: os.ErrDeadlineExceeded,
		},
		{
			name: "deadline changed",
			setup: func() (time.Time, <-chan struct{}, <-chan struct{}, <-chan struct{}) {
				wake := make(chan struct{})
				close(wake)
				return time.Now().Add(time.Second), wake, make(chan struct{}), make(chan struct{})
			},
			want: errSessionWriteDeadlineChanged,
		},
		{
			name: "session closed",
			setup: func() (time.Time, <-chan struct{}, <-chan struct{}, <-chan struct{}) {
				done := make(chan struct{})
				close(done)
				return time.Now().Add(time.Second), make(chan struct{}), done, make(chan struct{})
			},
			want: net.ErrClosed,
		},
		{
			name: "connection closed",
			setup: func() (time.Time, <-chan struct{}, <-chan struct{}, <-chan struct{}) {
				done := make(chan struct{})
				close(done)
				return time.Now().Add(time.Second), make(chan struct{}), make(chan struct{}), done
			},
			want: ErrDisconnected,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var mu sync.Mutex
			mu.Lock()
			defer mu.Unlock()
			deadline, wake, sessionDone, connectionDone := testCase.setup()
			unlock, err := lockWriteMutexUntil(&mu, deadline, wake, sessionDone, connectionDone)
			if unlock != nil {
				unlock()
				t.Fatal("lockWriteMutexUntil unexpectedly acquired the mutex")
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("lockWriteMutexUntil() error=%v want %v", err, testCase.want)
			}
		})
	}
}
