package taskpayload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"google.golang.org/api/option"
)

type gcsObjectStore struct {
	client *storage.Client
	bucket *storage.BucketHandle
	name   string
}

// GCSTaskPayloadProvider persists large TaskRun JSON through Google Cloud
// Storage while retaining small payloads inline.
type GCSTaskPayloadProvider struct{ *objectTaskPayloadProvider }

func NewGCSTaskPayloadProvider(ctx context.Context, cfg config.ArtifactStorageConfig) (*GCSTaskPayloadProvider, error) {
	store, err := newGCSObjectStore(ctx, cfg.GCS)
	if err != nil {
		return nil, err
	}
	provider, err := newConfiguredObjectProvider("gcs", store, cfg)
	if err != nil {
		return nil, err
	}
	return &GCSTaskPayloadProvider{objectTaskPayloadProvider: provider}, nil
}

func newGCSObjectStore(ctx context.Context, cfg config.GCSArtifactConfig) (objectStore, error) {
	bucketName := strings.TrimSpace(cfg.Bucket)
	if bucketName == "" {
		return nil, errors.New("storage.artifacts.gcs.bucket is required")
	}
	credentialsFile := configuredSecret(cfg.CredentialsFile)
	if cfg.Anonymous && credentialsFile != "" {
		return nil, errors.New("storage.artifacts.gcs.anonymous and credentials_file are mutually exclusive")
	}
	if cfg.Anonymous && strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("storage.artifacts.gcs.anonymous requires an explicit emulator or compatible endpoint")
	}
	options := []option.ClientOption{}
	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		options = append(options, option.WithEndpoint(endpoint))
	}
	if credentialsFile != "" {
		options = append(options, option.WithCredentialsFile(credentialsFile))
	}
	if cfg.Anonymous {
		options = append(options, option.WithoutAuthentication())
	}
	client, err := storage.NewClient(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("initialize GCS task payload client: %w", err)
	}
	return &gcsObjectStore{client: client, bucket: client.Bucket(bucketName), name: bucketName}, nil
}

func (s *gcsObjectStore) Put(ctx context.Context, key string, body []byte, metadata objectMetadata) error {
	writer := s.bucket.Object(key).NewWriter(ctx)
	writer.ContentType = metadata.ContentType
	writer.ContentEncoding = metadata.ContentEncoding
	writer.Metadata = map[string]string{"flowgent-sha256": metadata.SHA256}
	if _, err := writer.Write(body); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func (s *gcsObjectStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.bucket.Object(key).NewReader(ctx)
}

func (s *gcsObjectStore) Delete(ctx context.Context, key string) error {
	return s.bucket.Object(key).Delete(ctx)
}

func (s *gcsObjectStore) URI(key string) string { return fmt.Sprintf("gs://%s/%s", s.name, key) }
func (s *gcsObjectStore) Close() error          { return s.client.Close() }
