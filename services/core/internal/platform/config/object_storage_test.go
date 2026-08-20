package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTorrentStorageToolSupportsFilesystemToS3(t *testing.T) {
	setValidStorageToolEnvironment(t)

	settings, err := LoadTorrentStorageTool()
	if err != nil {
		t.Fatalf("LoadTorrentStorageTool() error = %v", err)
	}
	if settings.Source.Driver != "filesystem" || settings.Destination.Driver != "s3" {
		t.Fatalf("drivers = %q -> %q", settings.Source.Driver, settings.Destination.Driver)
	}
	if settings.Destination.S3.Endpoint != "http://127.0.0.1:9000" || !settings.Destination.S3.UsePathStyle || settings.Destination.S3.CredentialsMode != "static" {
		t.Fatalf("S3 settings = %+v", settings.Destination.S3)
	}
}

func TestLoadTorrentUploadStorageToolUsesOnlyTheActiveBackend(t *testing.T) {
	setValidCoreEnvironment(t)

	settings, err := LoadTorrentUploadStorageTool()
	if err != nil {
		t.Fatalf("LoadTorrentUploadStorageTool() error = %v", err)
	}
	if settings.Store.BackendID != "local-primary" || settings.Store.Driver != "filesystem" || settings.Store.FilesystemRoot == "" {
		t.Fatalf("active upload store = %+v", settings.Store)
	}
}

func TestLoadTorrentStorageToolRejectsInsecureProductionS3Endpoint(t *testing.T) {
	setValidStorageToolEnvironment(t)
	t.Setenv("PEERGO_ENV", "production")

	_, err := LoadTorrentStorageTool()
	if err == nil || !strings.Contains(err.Error(), "must use https in production") {
		t.Fatalf("LoadTorrentStorageTool() error = %v", err)
	}
}

func TestLoadTorrentStorageToolRequiresDifferentBackendIDs(t *testing.T) {
	setValidStorageToolEnvironment(t)
	t.Setenv("PEERGO_STORAGE_DESTINATION_BACKEND_ID", "local-primary")

	_, err := LoadTorrentStorageTool()
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("LoadTorrentStorageTool() error = %v", err)
	}
}

func TestObjectStoreConfigSHA256BindsScopeButNotSecrets(t *testing.T) {
	settings := ObjectStoreConfig{
		BackendID: "s3-primary",
		Driver:    "s3",
		S3: S3ObjectStoreConfig{
			Region: "us-east-1", Bucket: "peergo-objects", Prefix: "production",
			Endpoint: "https://objects.example", UsePathStyle: true, CredentialsMode: "static",
			AccessKeyID: "first", SecretAccessKey: "first-secret",
		},
	}
	original := ObjectStoreConfigSHA256(settings)
	settings.S3.AccessKeyID = "rotated"
	settings.S3.SecretAccessKey = "rotated-secret"
	if rotated := ObjectStoreConfigSHA256(settings); rotated != original {
		t.Fatal("credential rotation changed the stable object-store scope digest")
	}
	settings.S3.Prefix = "different"
	if changed := ObjectStoreConfigSHA256(settings); changed == original {
		t.Fatal("object-store scope change did not change its digest")
	}
}

func setValidStorageToolEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_STORAGE_SOURCE_BACKEND_ID", "local-primary")
	t.Setenv("PEERGO_STORAGE_SOURCE_DRIVER", "filesystem")
	t.Setenv("PEERGO_STORAGE_SOURCE_FILESYSTEM_ROOT", filepath.Join(t.TempDir(), "objects"))
	t.Setenv("PEERGO_STORAGE_DESTINATION_BACKEND_ID", "s3-primary")
	t.Setenv("PEERGO_STORAGE_DESTINATION_DRIVER", "s3")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_REGION", "us-east-1")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_BUCKET", "peergo-objects")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_PREFIX", "dev")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_ENDPOINT", "http://127.0.0.1:9000")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_USE_PATH_STYLE", "true")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_CREDENTIALS_MODE", "static")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_ACCESS_KEY_ID", "peergo-local")
	t.Setenv("PEERGO_STORAGE_DESTINATION_S3_SECRET_ACCESS_KEY", "peergo-local-secret")
}
