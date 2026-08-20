package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func TestS3UsesConditionalChecksumWriteAndExactVersionRead(t *testing.T) {
	t.Parallel()

	contents := []byte("immutable torrent bytes")
	descriptor := torrents.StoredObjectDescriptor{
		SHA256: torrents.ObjectSHA256(sha256.Sum256(contents)), ByteLength: int64(len(contents)),
	}
	client := &fakeS3Client{getContents: contents, getVersionID: "version-7"}
	store, err := newS3WithClient("s3-primary", "peergo-objects", "production", client)
	if err != nil {
		t.Fatal(err)
	}
	key := torrents.TorrentObjectKey(descriptor.SHA256)
	result, err := store.PutIfAbsent(context.Background(), key, bytes.NewReader(contents), descriptor)
	if err != nil || !result.Created || result.VersionID != "version-7" {
		t.Fatalf("PutIfAbsent() = %+v, %v", result, err)
	}
	if aws.ToString(client.putInput.IfNoneMatch) != "*" || aws.ToString(client.putInput.Key) != "production/"+string(key) {
		t.Fatalf("PutObject input = %+v", client.putInput)
	}
	if aws.ToString(client.putInput.ChecksumSHA256) != base64.StdEncoding.EncodeToString(descriptor.SHA256[:]) || client.putInput.Metadata["peergo-sha256"] != descriptor.SHA256.Hex() {
		t.Fatalf("PutObject checksum metadata = %+v", client.putInput.Metadata)
	}

	object, err := store.Open(context.Background(), key, "version-7")
	if err != nil {
		t.Fatal(err)
	}
	read, _ := io.ReadAll(object.Body)
	_ = object.Body.Close()
	if !bytes.Equal(read, contents) || object.VersionID != "version-7" || aws.ToString(client.getInput.VersionId) != "version-7" {
		t.Fatalf("Open() bytes=%q version=%q input=%+v", read, object.VersionID, client.getInput)
	}
	if err := store.Delete(context.Background(), key, "version-7"); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(client.deleteInput.VersionId) != "version-7" {
		t.Fatalf("DeleteObject version = %q", aws.ToString(client.deleteInput.VersionId))
	}
}

func TestS3TreatsPreconditionFailureAsExistingWithoutOverwrite(t *testing.T) {
	t.Parallel()

	client := &fakeS3Client{putError: &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "already exists"}}
	store, err := newS3WithClient("s3-primary", "peergo-objects", "", client)
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("object")
	descriptor := torrents.StoredObjectDescriptor{
		SHA256: torrents.ObjectSHA256(sha256.Sum256(contents)), ByteLength: int64(len(contents)),
	}
	result, err := store.PutIfAbsent(context.Background(), torrents.TorrentObjectKey(descriptor.SHA256), bytes.NewReader(contents), descriptor)
	if err != nil || result.Created {
		t.Fatalf("PutIfAbsent(existing) = %+v, %v", result, err)
	}
}

type fakeS3Client struct {
	putInput     *awss3.PutObjectInput
	getInput     *awss3.GetObjectInput
	deleteInput  *awss3.DeleteObjectInput
	putError     error
	getContents  []byte
	getVersionID string
}

func (client *fakeS3Client) PutObject(_ context.Context, input *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	client.putInput = input
	if client.putError != nil {
		return nil, client.putError
	}
	return &awss3.PutObjectOutput{VersionId: aws.String(client.getVersionID)}, nil
}

func (client *fakeS3Client) GetObject(_ context.Context, input *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	client.getInput = input
	length := int64(len(client.getContents))
	return &awss3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(client.getContents)), ContentLength: &length,
		VersionId: aws.String(client.getVersionID),
	}, nil
}

func (client *fakeS3Client) DeleteObject(_ context.Context, input *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	client.deleteInput = input
	return &awss3.DeleteObjectOutput{}, nil
}

var _ s3API = (*fakeS3Client)(nil)
