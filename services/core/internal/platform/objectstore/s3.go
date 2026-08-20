package objectstore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type s3API interface {
	PutObject(context.Context, *awss3.PutObjectInput, ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	DeleteObject(context.Context, *awss3.DeleteObjectInput, ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error)
}

type S3 struct {
	backendID objectstorage.BackendID
	bucket    string
	prefix    string
	client    s3API
}

// NewS3 composes the shared AWS SDK v2 client for AWS S3, MinIO, R2, and other
// compatible providers. BaseEndpoint uses the SDK's current endpoint resolver;
// path-style is an explicit deployment choice rather than a vendor-specific
// adapter fork.
func NewS3(backendID objectstorage.BackendID, bucket, prefix string, awsConfig aws.Config, baseEndpoint string, usePathStyle bool) (*S3, error) {
	return newS3WithClient(backendID, bucket, prefix, newS3Client(awsConfig, baseEndpoint, usePathStyle))
}

func newS3Client(awsConfig aws.Config, baseEndpoint string, usePathStyle bool) *awss3.Client {
	return awss3.NewFromConfig(awsConfig, func(options *awss3.Options) {
		options.UsePathStyle = usePathStyle
		if strings.TrimSpace(baseEndpoint) != "" {
			options.BaseEndpoint = aws.String(strings.TrimRight(baseEndpoint, "/"))
		}
	})
}

func newS3WithClient(backendID objectstorage.BackendID, bucket, prefix string, client s3API) (*S3, error) {
	bucket = strings.TrimSpace(bucket)
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if backendID == "" || bucket == "" || len(bucket) > 255 || strings.ContainsAny(bucket, "/\\\t\r\n ") || client == nil {
		return nil, errors.New("S3 object store configuration is invalid")
	}
	if prefix != "" {
		parsed, err := objectstorage.ParseKey(prefix)
		if err != nil || string(parsed) != prefix {
			return nil, errors.New("S3 object prefix is invalid")
		}
	}
	return &S3{backendID: backendID, bucket: bucket, prefix: prefix, client: client}, nil
}

func (store *S3) BackendID() objectstorage.BackendID {
	return store.backendID
}

func (store *S3) PutIfAbsent(ctx context.Context, key objectstorage.Key, source io.Reader, expected objectstorage.Descriptor) (objectstorage.WriteResult, error) {
	physicalKey, err := store.physicalKey(key)
	if err != nil || source == nil || expected.ByteLength <= 0 {
		return objectstorage.WriteResult{}, objectstorage.ErrInputInvalid
	}
	checksum := base64.StdEncoding.EncodeToString(expected.SHA256[:])
	result, err := store.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:            aws.String(store.bucket),
		Key:               aws.String(physicalKey),
		Body:              source,
		ContentLength:     aws.Int64(expected.ByteLength),
		ContentType:       aws.String("application/octet-stream"),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		ChecksumSHA256:    aws.String(checksum),
		IfNoneMatch:       aws.String("*"),
		Metadata: map[string]string{
			"peergo-sha256": expected.SHA256.Hex(),
		},
	})
	if isS3ErrorCode(err, "PreconditionFailed", "412") {
		return objectstorage.WriteResult{Created: false}, nil
	}
	if err != nil {
		return objectstorage.WriteResult{}, fmt.Errorf("put immutable S3 object: %w", err)
	}
	return objectstorage.WriteResult{Created: true, VersionID: aws.ToString(result.VersionId)}, nil
}

func (store *S3) Open(ctx context.Context, key objectstorage.Key, versionID string) (objectstorage.Reader, error) {
	physicalKey, err := store.physicalKey(key)
	if err != nil {
		return objectstorage.Reader{}, err
	}
	input := &awss3.GetObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(physicalKey), ChecksumMode: types.ChecksumModeEnabled,
	}
	if strings.TrimSpace(versionID) != "" {
		input.VersionId = aws.String(versionID)
	}
	result, err := store.client.GetObject(ctx, input)
	if isS3ErrorCode(err, "NoSuchKey", "NoSuchVersion", "NotFound", "404") {
		return objectstorage.Reader{}, objectstorage.ErrNotFound
	}
	if err != nil {
		return objectstorage.Reader{}, fmt.Errorf("get S3 object: %w", err)
	}
	if result.Body == nil || result.ContentLength == nil || *result.ContentLength <= 0 {
		if result.Body != nil {
			_ = result.Body.Close()
		}
		return objectstorage.Reader{}, objectstorage.ErrObjectConflict
	}
	return objectstorage.Reader{
		Body: result.Body, ByteLength: *result.ContentLength, VersionID: aws.ToString(result.VersionId),
	}, nil
}

func (store *S3) Delete(ctx context.Context, key objectstorage.Key, versionID string) error {
	physicalKey, err := store.physicalKey(key)
	if err != nil {
		return err
	}
	input := &awss3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(physicalKey)}
	if strings.TrimSpace(versionID) != "" {
		input.VersionId = aws.String(versionID)
	}
	_, err = store.client.DeleteObject(ctx, input)
	if isS3ErrorCode(err, "NoSuchKey", "NoSuchVersion", "NotFound", "404") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (store *S3) physicalKey(key objectstorage.Key) (string, error) {
	parsed, err := objectstorage.ParseKey(string(key))
	if err != nil || parsed != key {
		return "", objectstorage.ErrInputInvalid
	}
	if store.prefix == "" {
		return string(key), nil
	}
	return store.prefix + "/" + string(key), nil
}

func isS3ErrorCode(err error, codes ...string) bool {
	if err == nil {
		return false
	}
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	for _, code := range codes {
		if apiError.ErrorCode() == code {
			return true
		}
	}
	return false
}

var _ objectstorage.Store = (*S3)(nil)
