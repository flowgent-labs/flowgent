package providers

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestDefaultSecretStore_EncryptDecrypt(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// Set a master key for this test
	os.Setenv("FLOWGENT_MASTER_KEY", "test-master-key-32bytes!!")
	defer os.Unsetenv("FLOWGENT_MASTER_KEY")

	store, err := NewDefaultSecretStoreProvider(db, "", "")
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
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	os.Setenv("FLOWGENT_MASTER_KEY", "test-master-key-32bytes!!")
	defer os.Unsetenv("FLOWGENT_MASTER_KEY")

	store, _ := NewDefaultSecretStoreProvider(db, "", "")
	_, err = store.GetSecret(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestDefaultSecretStore_Delete(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	os.Setenv("FLOWGENT_MASTER_KEY", "test-master-key-32bytes!!")
	defer os.Unsetenv("FLOWGENT_MASTER_KEY")

	store, _ := NewDefaultSecretStoreProvider(db, "", "")
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
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	os.Setenv("FLOWGENT_MASTER_KEY", "test-master-key-32bytes!!")
	defer os.Unsetenv("FLOWGENT_MASTER_KEY")

	store, _ := NewDefaultSecretStoreProvider(db, "", "")
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

func TestResolveMasterKey_EnvVar(t *testing.T) {
	os.Setenv("FLOWGENT_MASTER_KEY", "my-env-key")
	defer os.Unsetenv("FLOWGENT_MASTER_KEY")

	key, err := resolveMasterKey("", "")
	if err != nil {
		t.Fatalf("resolveMasterKey: %v", err)
	}
	if string(key) != "my-env-key" {
		t.Errorf("expected my-env-key, got %s", key)
	}
}

func TestResolveMasterKey_NoConfig(t *testing.T) {
	os.Unsetenv("FLOWGENT_MASTER_KEY")
	os.Unsetenv("FLOWGENT_MASTER_KEY_FILE")
	_, err := resolveMasterKey("", "")
	if err == nil {
		t.Fatal("expected error when no master key configured")
	}
}
