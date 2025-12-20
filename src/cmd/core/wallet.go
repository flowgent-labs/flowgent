package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shopspring/decimal"

	_ "github.com/mattn/go-sqlite3"

	"github.com/flowgent-labs/flowgent/src/payments"
	"github.com/flowgent-labs/flowgent/src/payments/providers"
)

// runWallet handles wallet start/stop/restart.
func runWallet(action, pidFile string) error {
	switch action {
	case "start":
		return startWallet(pidFile)
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		return startWallet(pidFile)
	default:
		return fmt.Errorf("unknown wallet action: %s", action)
	}
}

// runWalletGenKey generates a new Ed25519 wallet keypair.
// Format: "text" (human-readable) or "json" (machine-parseable).
func runWalletGenKey(format string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("key generation failed: %w", err)
	}
	pubHex := hex.EncodeToString(pub)
	privHex := hex.EncodeToString(priv)

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"public_key":  pubHex,
			"private_key": privHex,
		})
	default:
		fmt.Printf("Public key:  %s\n", pubHex)
		fmt.Printf("Private key: %s\n", privHex)
		return nil
	}
}

// startWallet starts the wallet key-management daemon.
func startWallet(pidFile string) error {
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		return fmt.Errorf("write PID file %s: %w", pidFile, err)
	}
	defer os.Remove(pidFile)

	dbPath := walletDB
	if dbPath == "" {
		dbPath = os.ExpandEnv("$HOME/.flowgent/wallet.db")
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	secretStore, err := providers.NewDefaultSecretStoreProvider(db, masterKey, masterKeyFile)
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

	httpServer := &http.Server{
		Addr:    walletListen,
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Flowgent Wallet daemon listening on %s", walletListen)
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
