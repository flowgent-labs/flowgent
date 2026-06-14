package providers

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testMasterKeyFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "master.key")
	if err := os.WriteFile(p, []byte("test-master-key-32bytes!!"), 0600); err != nil {
		t.Fatalf("write test key file: %v", err)
	}
	return p
}

func TestDefaultSecretStore_EncryptDecrypt(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	keyFile := testMasterKeyFile(t)
	store, err := NewDefaultSecretStoreProvider(db, keyFile)
	if err != nil {
		t.Fatalf("NewDefaultSecretStoreProvider: %v", err)
	}

	ctx := context.Background()

	// Put
	secret := []byte("my-wallet-private-key")
	if err := store.PutSecret(ctx, "wallet:test", secret); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}

	// Get
	got, err := store.GetSecret(ctx, "wallet:test")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if string(got) != string(secret) {
		t.Errorf("expected %s, got %s", secret, got)
	}
}

func TestDefaultSecretStore_NotFound(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	keyFile := testMasterKeyFile(t)
	store, _ := NewDefaultSecretStoreProvider(db, keyFile)
	_, err = store.GetSecret(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestDefaultSecretStore_Delete(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	keyFile := testMasterKeyFile(t)
	store, _ := NewDefaultSecretStoreProvider(db, keyFile)
	ctx := context.Background()

	store.PutSecret(ctx, "wallet:del", []byte("secret"))
	if err := store.DeleteSecret(ctx, "wallet:del"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	_, err = store.GetSecret(ctx, "wallet:del")
	if err == nil {
		t.Fatal("should not find deleted secret")
	}
}

func TestDefaultSecretStore_List(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	keyFile := testMasterKeyFile(t)
	store, _ := NewDefaultSecretStoreProvider(db, keyFile)
	ctx := context.Background()

	store.PutSecret(ctx, "wallet:a", []byte("1"))
	store.PutSecret(ctx, "wallet:b", []byte("2"))
	store.PutSecret(ctx, "other:x", []byte("3"))

	keys, err := store.ListSecrets(ctx, "wallet:")
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 wallet keys, got %d: %v", len(keys), keys)
	}
}

func TestResolveMasterKey_File(t *testing.T) {
	keyFile := testMasterKeyFile(t)
	key, err := resolveMasterKey(keyFile)
	if err != nil {
		t.Fatalf("resolveMasterKey: %v", err)
	}
	if string(key) != "test-master-key-32bytes!!" {
		t.Errorf("expected test-master-key-32bytes!!, got %s", key)
	}
}

func TestResolveMasterKey_NoConfig(t *testing.T) {
	_, err := resolveMasterKey("")
	if err == nil {
		t.Fatal("expected error when no master key configured")
	}
}
