package wallet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shopspring/decimal"

	_ "modernc.org/sqlite"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/wallet/pkg"
	"github.com/flowgent-labs/flowgent/wallet/pkg/providers"
)

// RunWallet handles wallet start/stop/restart.
// master key is loaded from the flowgent.yaml config object (payments.wallet.secret_store).
func RunWallet(action, listen, db, cfgPath string) error {
	switch action {
	case "start":
		return startWallet(listen, db, cfgPath)
	case "stop":
		return fmt.Errorf("stop: send SIGTERM")
	case "restart":
		return fmt.Errorf("restart: not supported")
	default:
		return fmt.Errorf("unknown wallet action: %s", action)
	}
}

// RunWalletGenKey generates a new Ed25519 keypair.
func RunWalletGenKey(format, encoding string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("key generation failed: %w", err)
	}

	pubStr := encodeKey(pub, encoding)
	privStr := encodeKey(priv, encoding)

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"public_key":  pubStr,
			"private_key": privStr,
		})
	default:
		fmt.Printf("Public key:  %s\n", pubStr)
		fmt.Printf("Private key: %s\n", privStr)
		return nil
	}
}

func encodeKey(key []byte, enc string) string {
	switch enc {
	case "base64":
		return base64.StdEncoding.EncodeToString(key)
	default:
		return hex.EncodeToString(key)
	}
}

// startWallet starts the wallet key-management daemon.
// Master key file path is read from config (payments.wallet.secret_store.master_key_file).
func startWallet(listen, dbPath, cfgPath string) error {
	var masterKeyFile string
	if cfgPath != "" {
		if cfg, err := config.Load(cfgPath); err == nil && cfg.Payments != nil {
			masterKeyFile = cfg.Payments.Wallet.SecretStore.MasterKeyFile
		}
	}

	if dbPath == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home = "/tmp"
		}
		dbPath = home + "/.flowgent/wallet.db"
	}
	// Ensure parent directory exists (container may not have $HOME/.flowgent/)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return fmt.Errorf("create wallet data dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	secretStore, err := providers.NewDefaultSecretStoreProvider(db, masterKeyFile)
	if err != nil {
		return fmt.Errorf("create secret store: %w", err)
	}

	srv := &walletServer{
		secretStore: secretStore,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/api/v1/wallet/sign", srv.handleSign)
	mux.HandleFunc("/api/v1/wallet/address", srv.handleAddress)
	mux.HandleFunc("/api/v1/wallet/balance", srv.handleBalance)
	mux.HandleFunc("GET /api/v1/wallet/keys", srv.handleListKeys)
	mux.HandleFunc("GET /api/v1/wallet/keys/{name}", srv.handleGetKey)
	mux.HandleFunc("POST /api/v1/wallet/keys", srv.handleCreateKey)
	mux.HandleFunc("DELETE /api/v1/wallet/keys/{name}", srv.handleDeleteKey)

	httpServer := &http.Server{
		Addr:    listen,
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Flowgent Wallet daemon listening on %s", listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Wallet server: %v", err)
		}
	}()

	<-sigCh
	log.Println("Wallet daemon shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(ctx)
}

type walletServer struct {
	secretStore payments.SecretStoreProvider
}

func (s *walletServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *walletServer) handleSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Wallet  string `json:"wallet"`
		Payload string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	keyBytes, err := s.secretStore.GetSecret(r.Context(), "wallet:"+req.Wallet)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get key: " + err.Error()})
		return
	}

	privKey := ed25519.PrivateKey(keyBytes)
	sig := ed25519.Sign(privKey, []byte(req.Payload))

	writeJSON(w, http.StatusOK, map[string]string{
		"signature": hex.EncodeToString(sig),
		"wallet":    req.Wallet,
	})
}

func (s *walletServer) handleAddress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	wallet := r.URL.Query().Get("wallet")
	keyBytes, err := s.secretStore.GetSecret(r.Context(), "wallet:"+wallet)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet not found"})
		return
	}

	privKey := ed25519.PrivateKey(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)

	writeJSON(w, http.StatusOK, map[string]string{
		"address": "0x" + hex.EncodeToString(pubKey),
	})
}

func (s *walletServer) handleBalance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"balance": decimal.Zero.String(),
	})
}

func (s *walletServer) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.secretStore.ListSecrets(r.Context(), "wallet:")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type KeyInfo struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	var result []KeyInfo
	for _, k := range keys {
		keyBytes, err := s.secretStore.GetSecret(r.Context(), k)
		if err != nil {
			continue
		}
		privKey := ed25519.PrivateKey(keyBytes)
		pubKey := privKey.Public().(ed25519.PublicKey)
		result = append(result, KeyInfo{
			Name:    strings.TrimPrefix(k, "wallet:"),
			Address: "0x" + hex.EncodeToString(pubKey),
		})
	}
	if result == nil {
		result = []KeyInfo{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *walletServer) handleGetKey(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	keyBytes, err := s.secretStore.GetSecret(r.Context(), "wallet:"+name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet not found: " + name})
		return
	}
	privKey := ed25519.PrivateKey(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    name,
		"address": "0x" + hex.EncodeToString(pubKey),
	})
}

func (s *walletServer) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		PrivateKey string `json:"private_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	var privKey ed25519.PrivateKey
	if req.PrivateKey != "" {
		kb, err := hex.DecodeString(req.PrivateKey)
		if err != nil || len(kb) != ed25519.PrivateKeySize {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid private key hex"})
			return
		}
		privKey = ed25519.PrivateKey(kb)
	} else {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "key generation failed"})
			return
		}
		privKey = priv
		_ = pub
	}

	if err := s.secretStore.PutSecret(r.Context(), "wallet:"+req.Name, []byte(privKey)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	pubKey := privKey.Public().(ed25519.PublicKey)
	resp := map[string]string{
		"name":    req.Name,
		"address": "0x" + hex.EncodeToString(pubKey),
	}
	if req.PrivateKey == "" {
		resp["private_key"] = hex.EncodeToString(privKey)
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *walletServer) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.secretStore.DeleteSecret(r.Context(), "wallet:"+name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
