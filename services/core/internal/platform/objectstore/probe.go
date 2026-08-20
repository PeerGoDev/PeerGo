package objectstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	platformconfig "github.com/peergo/peergo/services/core/internal/platform/config"
)

// ProbeConfiguredReadOnly verifies that a provisioned cutover destination is
// reachable without creating a canary object. The real importer still proves
// write/read integrity for every immutable object; preflight must not leave
// untracked bytes or delete anything merely to test credentials.
func ProbeConfiguredReadOnly(ctx context.Context, settings platformconfig.ObjectStoreConfig) error {
	if _, err := objectstorage.ParseBackendID(settings.BackendID); err != nil {
		return fmt.Errorf("parse object store backend ID: %w", err)
	}
	switch settings.Driver {
	case "filesystem":
		return probeFilesystemRoot(settings.FilesystemRoot)
	case "s3":
		awsSettings, err := loadConfiguredAWS(ctx, settings)
		if err != nil {
			return fmt.Errorf("load S3 probe configuration: %w", err)
		}
		client := newS3Client(awsSettings, settings.S3.Endpoint, settings.S3.UsePathStyle)
		if _, err := client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(settings.S3.Bucket)}); err != nil {
			return fmt.Errorf("read-only probe S3 bucket: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported object store driver %q", settings.Driver)
	}
}

func probeFilesystemRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return errors.New("filesystem object root must be absolute")
	}
	cleaned := filepath.Clean(root)
	info, err := os.Lstat(cleaned)
	if err != nil {
		return fmt.Errorf("inspect provisioned filesystem object root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("filesystem object root must be a provisioned non-symlink directory")
	}
	if info.Mode().Perm()&0o222 == 0 {
		return errors.New("filesystem object root has no write permission bits")
	}
	handle, err := os.Open(cleaned)
	if err != nil {
		return fmt.Errorf("open provisioned filesystem object root: %w", err)
	}
	return handle.Close()
}
