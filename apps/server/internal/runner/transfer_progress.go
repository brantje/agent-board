package runner

import "time"

const DefaultTransferProgressInterval = time.Second

type TransferProgress struct {
	BytesTransferred int64
	TotalBytes       int64
}

type TransferProgressFunc func(TransferProgress)

type transferProgressState struct {
	lastEmitted time.Time
	lastBytes   int64
}

func shouldEmitTransferProgress(transferred, total int64, state *transferProgressState, now time.Time, minInterval time.Duration) bool {
	if total <= 0 || transferred <= 0 {
		return false
	}
	if transferred >= total {
		return true
	}
	if state == nil || state.lastEmitted.IsZero() {
		return true
	}
	if now.Sub(state.lastEmitted) >= minInterval {
		return true
	}
	lastPercent := state.lastBytes * 100 / total
	currentPercent := transferred * 100 / total
	return currentPercent >= lastPercent+5
}

func emitTransferProgress(onProgress TransferProgressFunc, transferred, total int64, state *transferProgressState, now time.Time, minInterval time.Duration) {
	if onProgress == nil {
		return
	}
	if !shouldEmitTransferProgress(transferred, total, state, now, minInterval) {
		return
	}
	onProgress(TransferProgress{BytesTransferred: transferred, TotalBytes: total})
	if state != nil {
		state.lastEmitted = now
		state.lastBytes = transferred
	}
}
