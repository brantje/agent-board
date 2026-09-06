package evidence

// blobSizeLimit exposes a storage backend's maximum object size to evidence
// producers that can split larger logical payloads into multiple durable blobs.
type blobSizeLimit interface {
	MaxBlobBytes() int64
}

func (s *FileBlobStore) MaxBlobBytes() int64 {
	if s == nil {
		return 0
	}
	return s.maxBytes
}

func (s *RedactingBlobStore) MaxBlobBytes() int64 {
	if s == nil || s.base == nil {
		return 0
	}
	limited, ok := s.base.(blobSizeLimit)
	if !ok {
		return 0
	}
	return limited.MaxBlobBytes()
}

func maxBlobBytes(store BlobStore) int64 {
	limited, ok := store.(blobSizeLimit)
	if !ok {
		return 0
	}
	return limited.MaxBlobBytes()
}
