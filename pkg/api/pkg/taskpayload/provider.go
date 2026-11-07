// Package taskpayload persists and resolves complete TaskRun input/output JSON.
// It deliberately stays outside OpenTelemetry: traces carry bounded metadata,
// while this API-owned boundary preserves the auditable business payload.
package taskpayload

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

const (
	referenceEnvelopeKey = "$flowgent_task_payload"
	referenceVersion     = 1
	defaultMaxBytes      = int64(64 << 20)
)

// PayloadKind identifies which side of a TaskRun is being persisted.
type PayloadKind string

const (
	PayloadInput  PayloadKind = "input"
	PayloadOutput PayloadKind = "output"
)

// TaskPayloadKey is the stable logical identity of one TaskRun payload.
type TaskPayloadKey struct {
	Namespace string
	RunID     string
	TaskID    string
	Kind      PayloadKind
}

// ITaskPayloadProvider is the API Server boundary for TaskRun payload storage.
// Persist returns the representation stored in the DB; Resolve always returns
// the complete JSON object expected by REST clients and execution components.
type ITaskPayloadProvider interface {
	Name() string
	Persist(ctx context.Context, key TaskPayloadKey, payload map[string]any) (map[string]any, error)
	Resolve(ctx context.Context, stored map[string]any) (map[string]any, error)
	Delete(ctx context.Context, stored map[string]any) error
	Close() error
}

// ArtifactReference is stored inside a reserved, versioned JSON envelope in
// TaskRun.input/output. It is provider-neutral so migrations can move objects
// without changing the TaskRun schema.
type ArtifactReference struct {
	Version         int    `json:"version"`
	Provider        string `json:"provider"`
	Key             string `json:"key"`
	URI             string `json:"uri"`
	ContentType     string `json:"content_type"`
	ContentEncoding string `json:"content_encoding,omitempty"`
	SizeBytes       int64  `json:"size_bytes"`
	StoredSizeBytes int64  `json:"stored_size_bytes"`
	SHA256          string `json:"sha256"`
}

// DefaultTaskPayloadProvider keeps the complete JSON value inline in TaskRun
// database columns and requires no external service.
type DefaultTaskPayloadProvider struct {
	maxPayloadBytes int64
}

func NewDefaultTaskPayloadProvider(maxPayloadBytes int64) *DefaultTaskPayloadProvider {
	return &DefaultTaskPayloadProvider{maxPayloadBytes: normalizedMaxBytes(maxPayloadBytes)}
}

func (p *DefaultTaskPayloadProvider) Name() string { return "default" }

func (p *DefaultTaskPayloadProvider) Persist(_ context.Context, _ TaskPayloadKey, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		return nil, nil
	}
	if _, ok, err := decodeReference(payload); err != nil {
		return nil, err
	} else if ok {
		return nil, errors.New("default task payload provider cannot persist an external reference")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode task payload: %w", err)
	}
	if int64(len(raw)) > p.maxPayloadBytes {
		return nil, payloadTooLarge(len(raw), p.maxPayloadBytes)
	}
	return payload, nil
}

func (p *DefaultTaskPayloadProvider) Resolve(_ context.Context, stored map[string]any) (map[string]any, error) {
	if stored == nil {
		return nil, nil
	}
	if ref, ok, err := decodeReference(stored); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("task payload uses %q provider but active provider is default", ref.Provider)
	}
	return stored, nil
}

func (p *DefaultTaskPayloadProvider) Delete(context.Context, map[string]any) error { return nil }
func (p *DefaultTaskPayloadProvider) Close() error                                 { return nil }

type objectMetadata struct {
	ContentType     string
	ContentEncoding string
	SHA256          string
}

// objectStore isolates cloud SDK details from payload encoding, validation,
// threshold, compression, and checksum semantics.
type objectStore interface {
	Put(ctx context.Context, key string, body []byte, metadata objectMetadata) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	URI(key string) string
	Close() error
}

type objectTaskPayloadProvider struct {
	name            string
	store           objectStore
	inlineMaxBytes  int64
	maxPayloadBytes int64
	compression     string
	prefix          string
	putTimeout      time.Duration
	getTimeout      time.Duration
	verifyChecksum  bool
}

type objectProviderOptions struct {
	Name            string
	Store           objectStore
	InlineMaxBytes  int64
	MaxPayloadBytes int64
	Compression     string
	Prefix          string
	PutTimeout      time.Duration
	GetTimeout      time.Duration
	VerifyChecksum  bool
}

func newObjectTaskPayloadProvider(opts objectProviderOptions) (*objectTaskPayloadProvider, error) {
	if opts.Store == nil {
		return nil, errors.New("task payload object store is required")
	}
	if opts.InlineMaxBytes < 0 {
		return nil, errors.New("storage.artifacts.inline_max_bytes must be non-negative")
	}
	compression := strings.ToLower(strings.TrimSpace(opts.Compression))
	if compression == "" {
		compression = "gzip"
	}
	if compression != "none" && compression != "gzip" {
		return nil, fmt.Errorf("unsupported task payload compression %q", opts.Compression)
	}
	if opts.PutTimeout <= 0 {
		opts.PutTimeout = 30 * time.Second
	}
	if opts.GetTimeout <= 0 {
		opts.GetTimeout = 30 * time.Second
	}
	prefix := strings.Trim(strings.TrimSpace(opts.Prefix), "/")
	if prefix == "" {
		prefix = "flowgent/task-runs"
	}
	return &objectTaskPayloadProvider{
		name: opts.Name, store: opts.Store, inlineMaxBytes: opts.InlineMaxBytes,
		maxPayloadBytes: normalizedMaxBytes(opts.MaxPayloadBytes), compression: compression,
		prefix: prefix, putTimeout: opts.PutTimeout, getTimeout: opts.GetTimeout,
		verifyChecksum: opts.VerifyChecksum,
	}, nil
}

func (p *objectTaskPayloadProvider) Name() string { return p.name }

func (p *objectTaskPayloadProvider) Persist(ctx context.Context, key TaskPayloadKey, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		return nil, nil
	}
	if ref, ok, err := decodeReference(payload); err != nil {
		return nil, err
	} else if ok {
		if ref.Provider != p.name {
			return nil, fmt.Errorf("task payload reference provider %q does not match active provider %q", ref.Provider, p.name)
		}
		return payload, nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode task payload: %w", err)
	}
	if int64(len(raw)) > p.maxPayloadBytes {
		return nil, payloadTooLarge(len(raw), p.maxPayloadBytes)
	}
	if int64(len(raw)) <= p.inlineMaxBytes {
		return payload, nil
	}

	digest := sha256.Sum256(raw)
	checksum := hex.EncodeToString(digest[:])
	body := raw
	encoding := ""
	extension := ".json"
	if p.compression == "gzip" {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(raw); err != nil {
			return nil, fmt.Errorf("compress task payload: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("finish task payload compression: %w", err)
		}
		body = compressed.Bytes()
		encoding = "gzip"
		extension += ".gz"
	}

	objectKey := path.Join(
		p.prefix,
		safeKeySegment(key.Namespace),
		safeKeySegment(key.RunID),
		safeKeySegment(key.TaskID),
		string(key.Kind)+"-"+checksum+extension,
	)
	putCtx, cancel := context.WithTimeout(ctx, p.putTimeout)
	defer cancel()
	if err := p.store.Put(putCtx, objectKey, body, objectMetadata{
		ContentType: "application/json", ContentEncoding: encoding, SHA256: checksum,
	}); err != nil {
		return nil, fmt.Errorf("persist %s task payload: %w", p.name, err)
	}

	return encodeReference(ArtifactReference{
		Version: referenceVersion, Provider: p.name, Key: objectKey,
		URI: p.store.URI(objectKey), ContentType: "application/json",
		ContentEncoding: encoding, SizeBytes: int64(len(raw)),
		StoredSizeBytes: int64(len(body)), SHA256: checksum,
	}), nil
}

func (p *objectTaskPayloadProvider) Resolve(ctx context.Context, stored map[string]any) (map[string]any, error) {
	if stored == nil {
		return nil, nil
	}
	ref, ok, err := decodeReference(stored)
	if err != nil || !ok {
		return stored, err
	}
	if ref.Provider != p.name {
		return nil, fmt.Errorf("task payload uses %q provider but active provider is %q", ref.Provider, p.name)
	}
	if ref.SizeBytes < 0 || ref.SizeBytes > p.maxPayloadBytes {
		return nil, fmt.Errorf("task payload reference size %d exceeds configured maximum %d", ref.SizeBytes, p.maxPayloadBytes)
	}

	getCtx, cancel := context.WithTimeout(ctx, p.getTimeout)
	defer cancel()
	reader, err := p.store.Get(getCtx, ref.Key)
	if err != nil {
		return nil, fmt.Errorf("read %s task payload: %w", p.name, err)
	}
	defer reader.Close()
	storedLimit := p.maxPayloadBytes + max(p.maxPayloadBytes/100, 1<<20)
	body, err := io.ReadAll(io.LimitReader(reader, storedLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read task payload body: %w", err)
	}
	if int64(len(body)) > storedLimit {
		return nil, errors.New("stored task payload exceeds configured read limit")
	}

	raw := body
	if ref.ContentEncoding == "gzip" {
		gzipReader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("open task payload gzip stream: %w", err)
		}
		raw, err = io.ReadAll(io.LimitReader(gzipReader, p.maxPayloadBytes+1))
		closeErr := gzipReader.Close()
		if err != nil {
			return nil, fmt.Errorf("decompress task payload: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close task payload gzip stream: %w", closeErr)
		}
	} else if ref.ContentEncoding != "" {
		return nil, fmt.Errorf("unsupported stored task payload encoding %q", ref.ContentEncoding)
	}
	if int64(len(raw)) > p.maxPayloadBytes {
		return nil, payloadTooLarge(len(raw), p.maxPayloadBytes)
	}
	if ref.SizeBytes != int64(len(raw)) {
		return nil, fmt.Errorf("task payload size mismatch: got %d, want %d", len(raw), ref.SizeBytes)
	}
	if p.verifyChecksum {
		digest := sha256.Sum256(raw)
		if actual := hex.EncodeToString(digest[:]); actual != ref.SHA256 {
			return nil, fmt.Errorf("task payload checksum mismatch: got %s, want %s", actual, ref.SHA256)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode task payload JSON: %w", err)
	}
	return payload, nil
}

func (p *objectTaskPayloadProvider) Delete(ctx context.Context, stored map[string]any) error {
	ref, ok, err := decodeReference(stored)
	if err != nil || !ok {
		return err
	}
	if ref.Provider != p.name {
		return fmt.Errorf("task payload uses %q provider but active provider is %q", ref.Provider, p.name)
	}
	deleteCtx, cancel := context.WithTimeout(ctx, p.putTimeout)
	defer cancel()
	if err := p.store.Delete(deleteCtx, ref.Key); err != nil {
		return fmt.Errorf("delete %s task payload: %w", p.name, err)
	}
	return nil
}

func (p *objectTaskPayloadProvider) Close() error { return p.store.Close() }

func encodeReference(ref ArtifactReference) map[string]any {
	return map[string]any{referenceEnvelopeKey: ref}
}

func decodeReference(value map[string]any) (ArtifactReference, bool, error) {
	if len(value) != 1 {
		return ArtifactReference{}, false, nil
	}
	raw, ok := value[referenceEnvelopeKey]
	if !ok {
		return ArtifactReference{}, false, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ArtifactReference{}, true, fmt.Errorf("encode task payload reference: %w", err)
	}
	var ref ArtifactReference
	if err := json.Unmarshal(encoded, &ref); err != nil {
		return ArtifactReference{}, true, fmt.Errorf("decode task payload reference: %w", err)
	}
	if ref.Version != referenceVersion || ref.Provider == "" || ref.Key == "" || ref.URI == "" || ref.ContentType != "application/json" || ref.SizeBytes < 0 || ref.StoredSizeBytes < 0 || len(ref.SHA256) != sha256.Size*2 {
		return ArtifactReference{}, true, errors.New("invalid task payload reference")
	}
	return ref, true, nil
}

func safeKeySegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(value)
}

func normalizedMaxBytes(value int64) int64 {
	if value <= 0 {
		return defaultMaxBytes
	}
	return value
}

func payloadTooLarge(size int, maximum int64) error {
	return fmt.Errorf("task payload is %d bytes; configured maximum is %d bytes", size, maximum)
}
