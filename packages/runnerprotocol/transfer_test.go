package runnerprotocol

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestTransferChunkFitsMaxMessageSize(t *testing.T) {
	data := make([]byte, TransferChunkSize)
	msg, err := NewMessage(Version2, TypeTransferChunk, "11111111-1111-4111-8111-111111111111", TransferChunk{
		TransferID: "transfer-1",
		Data:       base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxMessageSize {
		t.Fatalf("chunk envelope %d exceeds MaxMessageSize %d", len(encoded), MaxMessageSize)
	}
}

func TestTransferIntegrityRules(t *testing.T) {
	for _, begin := range []TransferBegin{
		{TotalBytes: 12},
		{TransferID: "t", TotalBytes: -1},
		{TransferID: "t", TotalBytes: MaxTransferBytes + 1},
	} {
		if err := ValidateTransferBegin(begin); err == nil {
			t.Fatalf("invalid begin accepted: %+v", begin)
		}
	}

	payload := []byte("data")
	chunk := TransferChunk{TransferID: "t", Data: base64.StdEncoding.EncodeToString(payload)}
	buffer, err := AppendTransferChunk(nil, int64(len(payload)), chunk)
	if err != nil || string(buffer) != "data" {
		t.Fatalf("append=%q err=%v", buffer, err)
	}
	if err := ValidateTransferPayload(buffer, int64(len(payload)), TransferChecksum(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendTransferChunk(nil, 1, chunk); err == nil {
		t.Fatal("oversized payload accepted")
	}
	if err := ValidateTransferPayload(buffer, int64(len(payload)), "wrong"); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
}
