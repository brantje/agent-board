package runnerprotocol

const TransferChunkSize = 64 << 10

type TransferBegin struct {
	TransferID string `json:"transfer_id"`
	Direction  string `json:"direction"`
	TotalBytes int64  `json:"total_bytes"`
	Checksum   string `json:"checksum"`
}

type TransferChunk struct {
	TransferID string `json:"transfer_id"`
	Data       string `json:"data"`
}

type TransferEnd struct {
	TransferID string `json:"transfer_id"`
}

type TransferFailed struct {
	TransferID string `json:"transfer_id"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}
