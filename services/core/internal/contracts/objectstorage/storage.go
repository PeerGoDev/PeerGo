// Package objectstorage defines the provider-neutral immutable object port
// shared by Core domains. It deliberately contains no database location or
// migration policy: those remain owned by the domain that references bytes.
package objectstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	maxBackendIDBytes = 63
	maxObjectKeyBytes = 1024
)

var (
	ErrInputInvalid   = errors.New("object storage input is invalid")
	ErrNotFound       = errors.New("stored object was not found")
	ErrObjectConflict = errors.New("stored object conflicts with immutable identity")
)

// SHA256 is the immutable identity of the complete stored byte stream.
type SHA256 [sha256.Size]byte

func (digest SHA256) Hex() string { return hex.EncodeToString(digest[:]) }

// BackendID is a stable deployment name. Credentials, endpoints and host
// paths remain runtime configuration rather than entering domain records.
type BackendID string

func ParseBackendID(value string) (BackendID, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > maxBackendIDBytes || !validIdentifier(value) {
		return "", ErrInputInvalid
	}
	return BackendID(value), nil
}

func validIdentifier(value string) bool {
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '-' || character == '_' || character == '.') {
			continue
		}
		return false
	}
	return true
}

// Key is a provider-neutral relative key, never a host path or public URL.
type Key string

func ParseKey(value string) (Key, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxObjectKeyBytes || !utf8.ValidString(value) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", ErrInputInvalid
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInputInvalid
	}
	for _, component := range strings.Split(cleaned, "/") {
		if component == "" || component == "." || component == ".." {
			return "", ErrInputInvalid
		}
	}
	return Key(cleaned), nil
}

type Descriptor struct {
	SHA256     SHA256
	ByteLength int64
}

func (descriptor Descriptor) Valid() bool {
	return descriptor.SHA256 != (SHA256{}) && descriptor.ByteLength > 0
}

type WriteResult struct {
	Created   bool
	VersionID string
}

type Reader struct {
	Body       io.ReadCloser
	ByteLength int64
	VersionID  string
}

// Store writes immutable objects without replacement. Callers choose keys and
// persist physical locations; adapters only implement provider mechanics.
type Store interface {
	BackendID() BackendID
	PutIfAbsent(context.Context, Key, io.Reader, Descriptor) (WriteResult, error)
	Open(context.Context, Key, string) (Reader, error)
	Delete(context.Context, Key, string) error
}

// Registry resolves stable backend identifiers to runtime adapters. Keeping
// this provider-neutral lets every domain share local/S3 cutover mechanics
// without sharing database policy or leaking storage locations to clients.
type Registry struct {
	stores map[BackendID]Store
}

func NewRegistry(stores ...Store) (*Registry, error) {
	registry := &Registry{stores: make(map[BackendID]Store, len(stores))}
	for _, store := range stores {
		if store == nil || store.BackendID() == "" {
			return nil, errors.New("object store and backend ID are required")
		}
		if _, exists := registry.stores[store.BackendID()]; exists {
			return nil, fmt.Errorf("duplicate object store backend %q", store.BackendID())
		}
		registry.stores[store.BackendID()] = store
	}
	if len(registry.stores) == 0 {
		return nil, errors.New("at least one object store is required")
	}
	return registry, nil
}

func (registry *Registry) Get(backendID BackendID) (Store, bool) {
	if registry == nil {
		return nil, false
	}
	store, exists := registry.stores[backendID]
	return store, exists
}

// Verify performs the full read-back required after every immutable write.
// Provider checksums and ETags are useful signals but never replace this test.
func Verify(object Reader, expected Descriptor) (Descriptor, error) {
	if object.Body == nil || !expected.Valid() || object.ByteLength != expected.ByteLength {
		return Descriptor{}, ErrObjectConflict
	}
	hasher := sha256.New()
	read, err := io.Copy(hasher, io.LimitReader(object.Body, expected.ByteLength+1))
	if err != nil {
		return Descriptor{}, fmt.Errorf("hash stored object: %w", err)
	}
	if read != expected.ByteLength {
		return Descriptor{}, ErrObjectConflict
	}
	var digest SHA256
	copy(digest[:], hasher.Sum(nil))
	if digest != expected.SHA256 {
		return Descriptor{}, ErrObjectConflict
	}
	return Descriptor{SHA256: digest, ByteLength: read}, nil
}

// ReadAllVerified is intended for bounded media that must be returned to an
// HTTP client after verification. Large domain objects should stream through
// Verify instead of retaining a second copy in memory.
func ReadAllVerified(object Reader, expected Descriptor) ([]byte, error) {
	if object.Body == nil || !expected.Valid() || object.ByteLength != expected.ByteLength {
		return nil, ErrObjectConflict
	}
	contents, err := io.ReadAll(io.LimitReader(object.Body, expected.ByteLength+1))
	if err != nil {
		return nil, fmt.Errorf("read stored object: %w", err)
	}
	if int64(len(contents)) != expected.ByteLength {
		return nil, ErrObjectConflict
	}
	digest := SHA256(sha256.Sum256(contents))
	if digest != expected.SHA256 {
		return nil, ErrObjectConflict
	}
	return contents, nil
}
