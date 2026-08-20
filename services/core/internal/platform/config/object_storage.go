package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ObjectStoreConfig struct {
	BackendID      string
	Driver         string
	FilesystemRoot string
	S3             S3ObjectStoreConfig
}

type S3ObjectStoreConfig struct {
	Region          string
	Bucket          string
	Prefix          string
	Endpoint        string
	UsePathStyle    bool
	CredentialsMode string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type TorrentStorageToolConfig struct {
	Environment string
	DatabaseURL string
	Source      ObjectStoreConfig
	Destination ObjectStoreConfig
}

// ObjectMigrationToolConfig is shared by the unified immutable-object
// migration command. TorrentStorageToolConfig remains as a compatibility alias
// for older operator scripts while both paths use the same validated loader.
type ObjectMigrationToolConfig = TorrentStorageToolConfig

type TorrentUploadStorageToolConfig struct {
	Environment string
	DatabaseURL string
	Store       ObjectStoreConfig
}

// ObjectStoreConfigSHA256 commits a cutover manifest to the stable physical
// storage scope without copying credentials or host/bucket details into that
// manifest. Rotating a secret therefore does not change object identity, while
// changing a root, bucket, prefix, endpoint, driver, or backend ID does.
func ObjectStoreConfigSHA256(settings ObjectStoreConfig) [sha256.Size]byte {
	parts := []string{"peergo:object-store-config:v1", settings.BackendID, settings.Driver}
	switch settings.Driver {
	case "filesystem":
		parts = append(parts, filepath.Clean(settings.FilesystemRoot))
	case "s3":
		parts = append(parts,
			settings.S3.Region,
			settings.S3.Bucket,
			settings.S3.Prefix,
			settings.S3.Endpoint,
			strconv.FormatBool(settings.S3.UsePathStyle),
			settings.S3.CredentialsMode,
		)
	}
	return sha256.Sum256([]byte(strings.Join(parts, "\x00")))
}

// LoadTorrentUploadStorageTool composes one backend at a time for orphan
// reconciliation. Operators can rerun it with a previous backend's runtime
// credentials after a storage cutover without placing secrets in PostgreSQL.
func LoadTorrentUploadStorageTool() (TorrentUploadStorageToolConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return TorrentUploadStorageToolConfig{}, err
	}
	if environment != "development" && environment != "production" {
		return TorrentUploadStorageToolConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return TorrentUploadStorageToolConfig{}, err
	}
	store, err := loadObjectStore("PEERGO_TORRENT_STORAGE", environment)
	if err != nil {
		return TorrentUploadStorageToolConfig{}, err
	}
	return TorrentUploadStorageToolConfig{Environment: environment, DatabaseURL: databaseURL, Store: store}, nil
}

// LoadTorrentStorageTool keeps migration credentials out of site settings and
// the Core API process. Operators inject exactly two named backends into the
// bounded migration command; database locations retain only those stable IDs.
func LoadTorrentStorageTool() (TorrentStorageToolConfig, error) {
	return LoadObjectMigrationTool()
}

func LoadObjectMigrationTool() (ObjectMigrationToolConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return TorrentStorageToolConfig{}, err
	}
	if environment != "development" && environment != "production" {
		return TorrentStorageToolConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	databaseURL, err := required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return TorrentStorageToolConfig{}, err
	}
	source, err := loadObjectStore("PEERGO_STORAGE_SOURCE", environment)
	if err != nil {
		return TorrentStorageToolConfig{}, err
	}
	destination, err := loadObjectStore("PEERGO_STORAGE_DESTINATION", environment)
	if err != nil {
		return TorrentStorageToolConfig{}, err
	}
	if source.BackendID == destination.BackendID {
		return TorrentStorageToolConfig{}, errors.New("storage source and destination backend IDs must differ")
	}
	return TorrentStorageToolConfig{
		Environment: environment, DatabaseURL: databaseURL, Source: source, Destination: destination,
	}, nil
}

func loadObjectStore(prefix, environment string) (ObjectStoreConfig, error) {
	backendID, err := required(prefix + "_BACKEND_ID")
	if err != nil {
		return ObjectStoreConfig{}, err
	}
	driver, err := required(prefix + "_DRIVER")
	if err != nil {
		return ObjectStoreConfig{}, err
	}
	settings := ObjectStoreConfig{BackendID: backendID, Driver: driver}
	switch driver {
	case "filesystem":
		root, err := required(prefix + "_FILESYSTEM_ROOT")
		if err != nil {
			return ObjectStoreConfig{}, err
		}
		if !filepath.IsAbs(root) {
			return ObjectStoreConfig{}, fmt.Errorf("%s_FILESYSTEM_ROOT must be absolute", prefix)
		}
		settings.FilesystemRoot = filepath.Clean(root)
	case "s3":
		settings.S3, err = loadS3ObjectStore(prefix, environment)
		if err != nil {
			return ObjectStoreConfig{}, err
		}
	default:
		return ObjectStoreConfig{}, fmt.Errorf("%s_DRIVER must be filesystem or s3", prefix)
	}
	return settings, nil
}

func loadS3ObjectStore(prefix, environment string) (S3ObjectStoreConfig, error) {
	region, err := required(prefix + "_S3_REGION")
	if err != nil {
		return S3ObjectStoreConfig{}, err
	}
	bucket, err := required(prefix + "_S3_BUCKET")
	if err != nil {
		return S3ObjectStoreConfig{}, err
	}
	credentialsMode, err := required(prefix + "_S3_CREDENTIALS_MODE")
	if err != nil {
		return S3ObjectStoreConfig{}, err
	}
	endpoint := strings.TrimSpace(os.Getenv(prefix + "_S3_ENDPOINT"))
	if endpoint != "" {
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return S3ObjectStoreConfig{}, fmt.Errorf("%s_S3_ENDPOINT must be an absolute origin without user info", prefix)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return S3ObjectStoreConfig{}, fmt.Errorf("%s_S3_ENDPOINT must use http or https", prefix)
		}
		if environment == "production" && parsed.Scheme != "https" {
			return S3ObjectStoreConfig{}, fmt.Errorf("%s_S3_ENDPOINT must use https in production", prefix)
		}
		endpoint = strings.TrimRight(endpoint, "/")
	}
	usePathStyle := false
	if raw := strings.TrimSpace(os.Getenv(prefix + "_S3_USE_PATH_STYLE")); raw != "" {
		usePathStyle, err = strconv.ParseBool(raw)
		if err != nil {
			return S3ObjectStoreConfig{}, fmt.Errorf("%s_S3_USE_PATH_STYLE must be true or false", prefix)
		}
	}
	settings := S3ObjectStoreConfig{
		Region: region, Bucket: bucket, Prefix: strings.Trim(strings.TrimSpace(os.Getenv(prefix+"_S3_PREFIX")), "/"),
		Endpoint: endpoint, UsePathStyle: usePathStyle, CredentialsMode: credentialsMode,
		AccessKeyID:     strings.TrimSpace(os.Getenv(prefix + "_S3_ACCESS_KEY_ID")),
		SecretAccessKey: strings.TrimSpace(os.Getenv(prefix + "_S3_SECRET_ACCESS_KEY")),
		SessionToken:    strings.TrimSpace(os.Getenv(prefix + "_S3_SESSION_TOKEN")),
	}
	switch credentialsMode {
	case "default":
		if settings.AccessKeyID != "" || settings.SecretAccessKey != "" || settings.SessionToken != "" {
			return S3ObjectStoreConfig{}, fmt.Errorf("%s default credential mode cannot include static credential fields", prefix)
		}
	case "static":
		if settings.AccessKeyID == "" || settings.SecretAccessKey == "" {
			return S3ObjectStoreConfig{}, fmt.Errorf("%s static credential mode requires access and secret keys", prefix)
		}
	default:
		return S3ObjectStoreConfig{}, fmt.Errorf("%s_S3_CREDENTIALS_MODE must be default or static", prefix)
	}
	return settings, nil
}
