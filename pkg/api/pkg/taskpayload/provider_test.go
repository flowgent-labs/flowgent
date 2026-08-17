package taskpayload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

type memoryObjectStore struct {
	objects  map[string][]byte
	metadata map[string]objectMetadata
	deleted  []string
	closed   bool
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: map[string][]byte{}, metadata: map[string]objectMetadata{}}
}

func (s *memoryObjectStore) Put(_ context.Context, key string, body []byte, metadata objectMetadata) error {
	s.objects[key] = bytes.Clone(body)
	s.metadata[key] = metadata
	return nil
}

func (s *memoryObjectStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *memoryObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	return nil
}

func (s *memoryObjectStore) URI(key string) string { return "memory://bucket/" + key }
func (s *memoryObjectStore) Close() error          { s.closed = true; return nil }

func TestDefaultTaskPayloadProviderKeepsCompleteJSONInline(t *testing.T) {
	t.Parallel()
	provider := NewDefaultTaskPayloadProvider(1024)
	payload := map[string]any{"prompt": "complete", "nested": map[string]any{"attempt": 2}}
	stored, err := provider.Persist(context.Background(), TaskPayloadKey{}, payload)
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if !reflect.DeepEqual(stored, payload) {
		t.Fatalf("stored = %#v, want %#v", stored, payload)
	}
	resolved, err := provider.Resolve(context.Background(), stored)
	if err != nil || !reflect.DeepEqual(resolved, payload) {
		t.Fatalf("Resolve = %#v, %v", resolved, err)
	}
}

func TestObjectTaskPayloadProviderExternalizesAndHydratesGzipJSON(t *testing.T) {
	t.Parallel()
	store := newMemoryObjectStore()
	provider, err := newObjectTaskPayloadProvider(objectProviderOptions{
		Name: "s3", Store: store, InlineMaxBytes: 8, MaxPayloadBytes: 4096,
		Compression: "gzip", Prefix: "tenant-artifacts", PutTimeout: time.Second,
		GetTimeout: time.Second, VerifyChecksum: true,
	})
	if err != nil {
		t.Fatalf("newObjectTaskPayloadProvider: %v", err)
	}
	payload := map[string]any{"prompt": "the complete retry input", "items": []any{"a", "b"}}
	stored, err := provider.Persist(context.Background(), TaskPayloadKey{
		Namespace: "team/a", RunID: "run/1", TaskID: "task/2", Kind: PayloadInput,
	}, payload)
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	ref, ok, err := decodeReference(stored)
	if err != nil || !ok {
		t.Fatalf("reference = %#v, %v, %v", ref, ok, err)
	}
	if ref.Provider != "s3" || ref.ContentEncoding != "gzip" || !strings.HasSuffix(ref.Key, ".json.gz") {
		t.Fatalf("reference = %#v", ref)
	}
	if strings.Contains(ref.Key, "team/a") || strings.Contains(ref.Key, "run/1") {
		t.Fatalf("object key contains unsafe path segment: %s", ref.Key)
	}
	resolved, err := provider.Resolve(context.Background(), stored)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !reflect.DeepEqual(resolved, payload) {
		t.Fatalf("resolved = %#v, want %#v", resolved, payload)
	}
	if err := provider.Delete(context.Background(), stored); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, exists := store.objects[ref.Key]; exists {
		t.Fatal("Delete left the external object behind")
	}
}

func TestObjectTaskPayloadProviderKeepsSmallPayloadInline(t *testing.T) {
	t.Parallel()
	store := newMemoryObjectStore()
	provider, err := newObjectTaskPayloadProvider(objectProviderOptions{
		Name: "gcs", Store: store, InlineMaxBytes: 1024, MaxPayloadBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"ok": true}
	stored, err := provider.Persist(context.Background(), TaskPayloadKey{}, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, payload) || len(store.objects) != 0 {
		t.Fatalf("stored = %#v, objects = %d", stored, len(store.objects))
	}
}

func TestObjectTaskPayloadProviderRejectsCorruption(t *testing.T) {
	t.Parallel()
	store := newMemoryObjectStore()
	provider, err := newObjectTaskPayloadProvider(objectProviderOptions{
		Name: "s3", Store: store, MaxPayloadBytes: 4096, Compression: "none", VerifyChecksum: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := provider.Persist(context.Background(), TaskPayloadKey{Kind: PayloadOutput}, map[string]any{"answer": strings.Repeat("x", 64)})
	if err != nil {
		t.Fatal(err)
	}
	ref, _, _ := decodeReference(stored)
	store.objects[ref.Key][0] ^= 1
	if _, err := provider.Resolve(context.Background(), stored); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Resolve corruption error = %v", err)
	}
}

func TestTaskPayloadProviderFactoryValidatesCloudConfiguration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := NewProvider(ctx, config.ArtifactStorageConfig{Provider: "unknown"}); err == nil {
		t.Fatal("unknown provider was accepted")
	}
	if _, err := NewProvider(ctx, config.ArtifactStorageConfig{Provider: "s3"}); err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("S3 validation error = %v", err)
	}
	if _, err := NewProvider(ctx, config.ArtifactStorageConfig{Provider: "gcs"}); err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("GCS validation error = %v", err)
	}
}

func TestDefaultTaskPayloadProviderEnforcesMaximum(t *testing.T) {
	t.Parallel()
	provider := NewDefaultTaskPayloadProvider(16)
	if _, err := provider.Persist(context.Background(), TaskPayloadKey{}, map[string]any{"value": strings.Repeat("x", 32)}); err == nil {
		t.Fatal("oversized payload was accepted")
	}
}
