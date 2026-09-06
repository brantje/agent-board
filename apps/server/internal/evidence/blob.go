package evidence

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrBlobTooLarge   = errors.New("evidence: blob exceeds configured size limit")
	ErrInvalidBlobRef = errors.New("evidence: invalid blob reference")
)

type Blob struct {
	Ref       string
	SizeBytes int64
	Digest    string
}

type BlobStore interface {
	Put(context.Context, string, io.Reader) (Blob, error)
	Open(context.Context, string) (io.ReadCloser, error)
}

type ReaderRedactor interface {
	Reader(string, io.Reader) io.Reader
}

type RedactingBlobStore struct {
	base     BlobStore
	redactor ReaderRedactor
}

func NewRedactingBlobStore(base BlobStore, redactor ReaderRedactor) (*RedactingBlobStore, error) {
	if base == nil || redactor == nil {
		return nil, fmt.Errorf("evidence: blob store and redactor are required")
	}
	return &RedactingBlobStore{base: base, redactor: redactor}, nil
}

func (s *RedactingBlobStore) Put(ctx context.Context, runID string, source io.Reader) (Blob, error) {
	if source == nil {
		return Blob{}, fmt.Errorf("evidence: blob source is required")
	}
	return s.base.Put(ctx, runID, s.redactor.Reader(runID, source))
}

func (s *RedactingBlobStore) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	return s.base.Open(ctx, ref)
}

type FileBlobStore struct {
	root     string
	maxBytes int64
}

func NewFileBlobStore(root string, maxBytes int64) (*FileBlobStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("evidence: blob root is required")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("evidence: max blob size must be positive")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("evidence: resolve blob root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("evidence: create blob root: %w", err)
	}
	return &FileBlobStore{root: absolute, maxBytes: maxBytes}, nil
}

func (s *FileBlobStore) Put(ctx context.Context, _ string, source io.Reader) (Blob, error) {
	if source == nil {
		return Blob{}, fmt.Errorf("evidence: blob source is required")
	}
	if err := ctx.Err(); err != nil {
		return Blob{}, err
	}

	id, err := newBlobID()
	if err != nil {
		return Blob{}, err
	}
	finalPath, err := s.pathForID(id)
	if err != nil {
		return Blob{}, err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return Blob{}, fmt.Errorf("evidence: create blob directory: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(finalPath), ".blob-*")
	if err != nil {
		return Blob{}, fmt.Errorf("evidence: create temporary blob: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	hash := sha256.New()
	limited := io.LimitReader(source, s.maxBytes+1)
	written, err := copyContext(ctx, io.MultiWriter(temp, hash), limited)
	if err != nil {
		return Blob{}, fmt.Errorf("evidence: write blob: %w", err)
	}
	if written > s.maxBytes {
		return Blob{}, ErrBlobTooLarge
	}
	if err := temp.Sync(); err != nil {
		return Blob{}, fmt.Errorf("evidence: sync blob: %w", err)
	}
	if err := temp.Close(); err != nil {
		return Blob{}, fmt.Errorf("evidence: close blob: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return Blob{}, fmt.Errorf("evidence: secure blob permissions: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return Blob{}, fmt.Errorf("evidence: commit blob: %w", err)
	}
	committed = true
	return Blob{Ref: "blob:" + id, SizeBytes: written, Digest: "sha256:" + hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *FileBlobStore) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := parseBlobRef(ref)
	if err != nil {
		return nil, err
	}
	path, err := s.pathForID(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("evidence: open blob: %w", err)
	}
	return file, nil
}

func (s *FileBlobStore) pathForID(id string) (string, error) {
	if len(id) != 32 {
		return "", ErrInvalidBlobRef
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", ErrInvalidBlobRef
	}
	path := filepath.Join(s.root, id[:2], id[2:])
	cleanRoot := filepath.Clean(s.root) + string(os.PathSeparator)
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath+string(os.PathSeparator), cleanRoot) {
		return "", ErrInvalidBlobRef
	}
	return cleanPath, nil
}

func newBlobID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("evidence: generate blob id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func parseBlobRef(ref string) (string, error) {
	if !strings.HasPrefix(ref, "blob:") {
		return "", ErrInvalidBlobRef
	}
	id := strings.TrimPrefix(ref, "blob:")
	if len(id) != 32 {
		return "", ErrInvalidBlobRef
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", ErrInvalidBlobRef
	}
	return id, nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}
