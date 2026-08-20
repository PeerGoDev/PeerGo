package objectstore

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
)

func NewConfigured(ctx context.Context, settings platformconfig.ObjectStoreConfig) (objectstorage.Store, error) {
	backendID, err := objectstorage.ParseBackendID(settings.BackendID)
	if err != nil {
		return nil, fmt.Errorf("parse object store backend ID: %w", err)
	}
	switch settings.Driver {
	case "filesystem":
		return NewFilesystem(backendID, settings.FilesystemRoot)
	case "s3":
		awsSettings, err := loadConfiguredAWS(ctx, settings)
		if err != nil {
			return nil, fmt.Errorf("load S3 client configuration: %w", err)
		}
		return NewS3(
			backendID, settings.S3.Bucket, settings.S3.Prefix, awsSettings,
			settings.S3.Endpoint, settings.S3.UsePathStyle,
		)
	default:
		return nil, fmt.Errorf("unsupported object store driver %q", settings.Driver)
	}
}

func loadConfiguredAWS(ctx context.Context, settings platformconfig.ObjectStoreConfig) (aws.Config, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(settings.S3.Region)}
	if settings.S3.CredentialsMode == "static" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			settings.S3.AccessKeyID, settings.S3.SecretAccessKey, settings.S3.SessionToken,
		)))
	}
	return awsconfig.LoadDefaultConfig(ctx, loadOptions...)
}
