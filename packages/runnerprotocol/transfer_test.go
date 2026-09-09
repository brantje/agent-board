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

func TestValidateTransferBeginRejectsOversizeAndMissingID(t *testing.T) {
	if err := ValidateTransferBegin(TransferBegin{TotalBytes: 12}); err == nil {
		t.Fatal("missing transfer id accepted")
	}
	if err := ValidateTransferBegin(TransferBegin{TransferID: "t", TotalBytes: -1}); err == nil {
		t.Fatal("negative size accepted")
	}
	if err := ValidateTransferBegin(TransferBegin{TransferID: "t", TotalBytes: MaxTransferBytes + 1}); err == nil {
		t.Fatal("oversize transfer accepted")
	}
	if err := ValidateTransferBegin(TransferBegin{TransferID: "t", TotalBytes: 0}); err != nil {
		t.Fatal(err)
	}
}
