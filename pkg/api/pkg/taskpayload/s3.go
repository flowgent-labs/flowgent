package taskpayload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

type s3Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3ObjectStore struct {
	client s3Client
	bucket string
}

// S3TaskPayloadProvider persists large TaskRun JSON through AWS S3 or an
// S3-compatible endpoint while retaining small payloads inline.
type S3TaskPayloadProvider struct{ *objectTaskPayloadProvider }

func NewS3TaskPayloadProvider(ctx context.Context, cfg config.ArtifactStorageConfig) (*S3TaskPayloadProvider, error) {
	store, err := newS3ObjectStore(ctx, cfg.S3)
	if err != nil {
		return nil, err
	}
	provider, err := newConfiguredObjectProvider("s3", store, cfg)
	if err != nil {
		return nil, err
	}
	return &S3TaskPayloadProvider{objectTaskPayloadProvider: provider}, nil
}

func newS3ObjectStore(ctx context.Context, cfg config.S3ArtifactConfig) (objectStore, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("storage.artifacts.s3.bucket is required")
	}
	accessKeyID := configuredSecret(cfg.AccessKeyID)
	secretAccessKey := configuredSecret(cfg.SecretAccessKey)
	if (accessKeyID == "") != (secretAccessKey == "") {
		return nil, errors.New("storage.artifacts.s3 access_key_id and secret_access_key must be configured together")
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{}
	if region := strings.TrimSpace(cfg.Region); region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(region))
	}
	if accessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, configuredSecret(cfg.SessionToken)),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("initialize S3 task payload client: %w", err)
	}
	if strings.TrimSpace(awsCfg.Region) == "" {
		return nil, errors.New("storage.artifacts.s3.region or AWS_REGION is required")
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
		options.UsePathStyle = cfg.ForcePathStyle
	})
	return &s3ObjectStore{client: client, bucket: bucket}, nil
}

func (s *s3ObjectStore) Put(ctx context.Context, key string, body []byte, metadata objectMetadata) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(body),
		ContentType: aws.String(metadata.ContentType), Metadata: map[string]string{"flowgent-sha256": metadata.SHA256},
	}
	if metadata.ContentEncoding != "" {
		input.ContentEncoding = aws.String(metadata.ContentEncoding)
	}
	_, err := s.client.PutObject(ctx, input)
	return err
}

func (s *s3ObjectStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (s *s3ObjectStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *s3ObjectStore) URI(key string) string { return fmt.Sprintf("s3://%s/%s", s.bucket, key) }
func (s *s3ObjectStore) Close() error          { return nil }
