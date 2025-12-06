// flowgent-wallet is a standalone daemon for secure key management and payment signing.
// It exposes a local API that the Flowgent runtime calls to sign payment authorizations.
// The wallet daemon is the ONLY process that has access to raw private keys.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

const usage = `Flowgent Wallet — Secure Key Management Daemon

Usage:
  flowgent-wallet [options]

Options:
  --listen ADDR         Listen address (default: "127.0.0.1:9901")
  --db PATH            SQLite database path (default: "~/.flowgent/wallet.db")
  --master-key KEY     Master encryption key
  --master-key-file F  Path to master key file
  --generate-key       Generate a new wallet keypair and exit
`

func main() {
	listen := "127.0.0.1:9901"
	dbPath := os.ExpandEnv("$HOME/.flowgent/wallet.db")
	masterKey := ""
	masterKeyFile := ""
	generateKey := false

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen":
			i++
			if i < len(args) {
				listen = args[i]
			}
		case "--db":
			i++
			if i < len(args) {
				dbPath = args[i]
			}
		case "--master-key":
			i++
			if i < len(args) {
				masterKey = args[i]
			}
		case "--master-key-file":
			i++
			if i < len(args) {
				masterKeyFile = args[i]
			}
		case "--generate-key":
			generateKey = true
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(0)
		}
	}

	if generateKey {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "key generation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Public key:  %s\n", hex.EncodeToString(pub))
		fmt.Printf("Private key: %s\n", hex.EncodeToString(priv))
		os.Exit(0)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	secretStore, err := providers.NewDefaultSecretStoreProvider(db, masterKey, masterKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create secret store: %v\n", err)
		os.Exit(1)
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
		Addr:    listen,
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("Flowgent Wallet daemon listening on %s\n", listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-sigCh
	fmt.Println("\nShutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx)
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

	// Retrieve the private key from the secret store
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
	// Balance is checked via the facilitator or chain RPC.
	// This endpoint returns a stub for the interface contract.
	writeJSON(w, http.StatusOK, map[string]string{
		"balance": decimal.Zero.String(),
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
