package torrents

import (
	"context"
	"crypto/sha256"
	"io"

	"github.com/google/uuid"
)

// readVerifiedStoredObject applies the same storage-migration read contract to
// every public binary surface: ordered locations may fall back only when a
// backend is absent or unavailable, while any returned length/hash conflict
// fails closed instead of being hidden by another copy.
func readVerifiedStoredObject(
	ctx context.Context,
	stores *StoreRegistry,
	descriptor StoredObjectDescriptor,
	locations []ReadableObjectLocation,
	maxByteLength int64,
) ([]byte, error) {
	if stores == nil || !descriptor.Valid() || maxByteLength < 1 || descriptor.ByteLength > maxByteLength {
		return nil, ErrReadableObjectConflict
	}
	for _, location := range locations {
		if location.ID == uuid.Nil || location.BackendID == "" || location.ObjectKey == "" ||
			location.Descriptor != descriptor || location.VerifiedAt.IsZero() {
			return nil, ErrReadableObjectConflict
		}
		store, configured := stores.Get(location.BackendID)
		if !configured {
			continue
		}
		object, err := store.Open(ctx, location.ObjectKey, location.VersionID)
		if err != nil {
			continue
		}
		if object.Body == nil || object.ByteLength != descriptor.ByteLength ||
			(location.VersionID != "" && object.VersionID != "" && object.VersionID != location.VersionID) {
			if object.Body != nil {
				_ = object.Body.Close()
			}
			return nil, ErrReadableObjectConflict
		}
		data, readErr := io.ReadAll(io.LimitReader(object.Body, descriptor.ByteLength+1))
		closeErr := object.Body.Close()
		if readErr != nil || closeErr != nil {
			continue
		}
		if int64(len(data)) != descriptor.ByteLength || ObjectSHA256(sha256.Sum256(data)) != descriptor.SHA256 {
			return nil, ErrReadableObjectConflict
		}
		return data, nil
	}
	return nil, ErrReadableObjectUnavailable
}
