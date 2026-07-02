// Package payments implements wallet key management and x402 payment signing.
//
// FlowgentWalletManager is the main wallet service. It subscribes to unsigned x402
// payment signing requests via MQTT, signs them using the EOA private key from the
// configured secret store, and publishes the signed result back. It also exposes an
// HTTP API for key management.
package payments

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
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
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/wallet/pkg/providers"
)

// FlowgentWalletManager is the main wallet service. It listens for unsigned payment
// signing requests via MQTT, signs them with the EOA private key, and publishes
// the signed result. It also serves an HTTP API for key CRUD operations.
type FlowgentWalletManager struct {
	cfg          *config.FlowgentConfig
	listenAddr   string
	dbPath       string

	secretStore  SecretStoreProvider
	messager     messager.IMessager
	httpServer   *http.Server
	providerName string
	db           *sql.DB
}

// NewFlowgentWalletManager creates a new FlowgentWalletManager. It initializes the secret store provider
// based on config (CSI, Vault, or AES-256-GCM encrypted SQLite).
func NewFlowgentWalletManager(cfgPath, listenAddr, dbPath string) (*FlowgentWalletManager, error) {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return nil, err
	}

	sc := resolveStoreConfig(cfg)
	secretStore, db, err := createSecretStore(sc, dbPath)
	if err != nil {
		return nil, fmt.Errorf("create secret store: %w", err)
	}

	wm := &FlowgentWalletManager{
		cfg:          cfg,
		listenAddr:   listenAddr,
		dbPath:       dbPath,
		secretStore:  secretStore,
		providerName: sc.provider,
		db:           db,
	}

	return wm, nil
}

// Start starts the WalletManager. It subscribes to unsigned payment signing
// requests via MQTT, starts the HTTP key management API, and blocks until
// a shutdown signal is received.
func (wm *FlowgentWalletManager) Start(ctx context.Context) error {
	if wm.db != nil {
		defer wm.db.Close()
	}

	// ── HTTP API ──────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/health", wm.handleHealth)
	mux.HandleFunc("/api/v1/wallet/sign", wm.handleSign)
	mux.HandleFunc("/api/v1/wallet/address", wm.handleAddress)
	mux.HandleFunc("/api/v1/wallet/balance", wm.handleBalance)
	mux.HandleFunc("GET /api/v1/wallet/keys", wm.handleListKeys)
	mux.HandleFunc("GET /api/v1/wallet/keys/{name}", wm.handleGetKey)
	mux.HandleFunc("POST /api/v1/wallet/keys", wm.handleCreateKey)
	mux.HandleFunc("DELETE /api/v1/wallet/keys/{name}", wm.handleDeleteKey)

	wm.httpServer = &http.Server{
		Addr:    wm.listenAddr,
		Handler: mux,
	}

	// ── MQTT subscribe → sign → publish loop ─────────────────
	if wm.cfg != nil && wm.cfg.Messager.MQTT.Broker != "" {
		clientID := "wallet-" + wm.listenAddr
		mq := messager.NewMessagerManager(wm.cfg, clientID)
		wm.messager = mq

		// Subscribe to shared sign request topic. TaskManager pods publish
		// unsigned payloads here; wallet pods consume with $share for
		// load-balanced delivery.
		if err := mq.Subscribe(ctx, messager.SharedSignRequest(), wm.handleSignRequest); err != nil {
			return fmt.Errorf("wallet MQTT sign subscription failed: %w", err)
		}
		slog.Info("WalletManager subscribed to sign requests via MQTT", "broker", wm.cfg.Messager.MQTT.Broker)

		defer mq.Close()
	}

	// ── Signal handling ──────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("WalletManager started", "http", wm.listenAddr, "provider", wm.providerName, "mqtt", mqttBroker(wm.cfg))
		if err := wm.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("WalletManager HTTP server: %v", err)
		}
	}()

	<-sigCh
	log.Println("WalletManager shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return wm.httpServer.Shutdown(shutdownCtx)
}

// ── MQTT sign request handler ──────────────────────────────────

// handleSignRequest is the MQTT callback for sign/request topics.
// It receives unsigned payment payloads from TaskManager pods, signs them
// using the wallet's EOA private key, and publishes the result back.
func (wm *FlowgentWalletManager) handleSignRequest(topic string, payload []byte) {
	var req messager.SignRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		slog.Warn("WalletManager invalid sign request", "err", err)
		return
	}

	// Retrieve the EOA private key from the secret store
	keyBytes, err := wm.secretStore.GetSecret(context.Background(), "wallet:"+req.Wallet)
	if err != nil {
		wm.publishSignError(req, "wallet not found: "+req.Wallet)
		return
	}

	// Sign with Ed25519 EOA private key
	privKey := ed25519.PrivateKey(keyBytes)
	sig := ed25519.Sign(privKey, []byte(req.Payload))

	slog.Info("WalletManager signed payment request", "requestID", req.RequestID, "wallet", req.Wallet)

	resp := messager.SignResponse{
		RequestID: req.RequestID,
		Wallet:    req.Wallet,
		Signature: hex.EncodeToString(sig),
	}
	respBody, _ := json.Marshal(resp)
	respTopic := messager.SignResponseTopic(req.TenantID, req.FlowID, req.RunID)

	if err := wm.messager.Publish(context.Background(), respTopic, &messager.InterMessage{
		ID:      req.RequestID,
		Payload: respBody,
	}); err != nil {
		slog.Error("WalletManager publish sign response failed", "err", err)
	}
}

func (wm *FlowgentWalletManager) publishSignError(req messager.SignRequest, errMsg string) {
	slog.Warn("WalletManager sign request failed", "requestID", req.RequestID, "error", errMsg)
	resp := messager.SignResponse{
		RequestID: req.RequestID,
		Wallet:    req.Wallet,
		Error:     errMsg,
	}
	respBody, _ := json.Marshal(resp)
	respTopic := messager.SignResponseTopic(req.TenantID, req.FlowID, req.RunID)
	if err := wm.messager.Publish(context.Background(), respTopic, &messager.InterMessage{
		ID:      req.RequestID,
		Payload: respBody,
	}); err != nil {
		slog.Error("WalletManager publish sign error failed", "err", err)
	}
}

// ── HTTP handlers ─────────────────────────────────────────────

func (wm *FlowgentWalletManager) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (wm *FlowgentWalletManager) handleSign(w http.ResponseWriter, r *http.Request) {
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
	keyBytes, err := wm.secretStore.GetSecret(r.Context(), "wallet:"+req.Wallet)
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

func (wm *FlowgentWalletManager) handleAddress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	wallet := r.URL.Query().Get("wallet")
	keyBytes, err := wm.secretStore.GetSecret(r.Context(), "wallet:"+wallet)
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

func (wm *FlowgentWalletManager) handleBalance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"balance": decimal.Zero.String(),
	})
}

func (wm *FlowgentWalletManager) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := wm.secretStore.ListSecrets(r.Context(), "wallet:")
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
		keyBytes, err := wm.secretStore.GetSecret(r.Context(), k)
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

func (wm *FlowgentWalletManager) handleGetKey(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	keyBytes, err := wm.secretStore.GetSecret(r.Context(), "wallet:"+name)
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

func (wm *FlowgentWalletManager) handleCreateKey(w http.ResponseWriter, r *http.Request) {
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

	if wm.providerName == "csi" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "key creation not supported with CSI provider — keys are managed externally via Vault/GCP/AWS Secret Manager and injected via CSI driver",
		})
		return
	}

	if req.PrivateKey != "" && wm.providerName == "vault" {
		slog.Info("importing private key for wallet into Vault", "wallet", req.Name)
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
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "key generation failed"})
			return
		}
		privKey = priv
	}

	if err := wm.secretStore.PutSecret(r.Context(), "wallet:"+req.Name, []byte(privKey)); err != nil {
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

func (wm *FlowgentWalletManager) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := wm.secretStore.DeleteSecret(r.Context(), "wallet:"+name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

// ── Helpers ────────────────────────────────────────────────────

type storeConfig struct {
	provider      string
	masterKeyFile string
	vault         config.VaultCfg
	csiBasePath   string
}

func loadConfig(cfgPath string) (*config.FlowgentConfig, error) {
	if cfgPath == "" {
		return nil, nil
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func resolveStoreConfig(cfg *config.FlowgentConfig) storeConfig {
	c := storeConfig{}
	if cfg == nil || cfg.Wallet == nil {
		return c
	}
	sc := cfg.Wallet.SecretStore
	c.provider = sc.Provider
	c.masterKeyFile = sc.MasterKeyFile
	c.vault = sc.Vault
	if cfg.CredentialPaths.BasePath != "" {
		c.csiBasePath = cfg.CredentialPaths.BasePath
	} else {
		c.csiBasePath = "/var/flowgent"
	}
	return c
}

func createSecretStore(sc storeConfig, dbPath string) (SecretStoreProvider, *sql.DB, error) {
	switch sc.provider {
	case "csi":
		slog.Info("Wallet secret store: CSI", "base", sc.csiBasePath)
		p, err := providers.NewCSISecretStoreProvider(sc.csiBasePath)
		return p, nil, err

	case "vault":
		slog.Info("Wallet secret store: Vault", "addr", sc.vault.Address)
		p, err := providers.NewVaultSecretStoreProvider(
			sc.vault.Address, sc.vault.Token, sc.vault.TokenFile,
			sc.vault.MountPath, sc.vault.SecretPath, sc.vault.Role,
		)
		return p, nil, err

	default:
		slog.Info("Wallet secret store: default (AES-256-GCM encrypted DB)")
		if dbPath == "" {
			home := os.Getenv("HOME")
			if home == "" {
				home = "/tmp"
			}
			dbPath = home + "/.flowgent/wallet.db"
		}
		if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
			return nil, nil, fmt.Errorf("create wallet data dir: %w", err)
		}
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return nil, nil, fmt.Errorf("open database: %w", err)
		}
		p, err := providers.NewDefaultSecretStoreProvider(db, sc.masterKeyFile)
		return p, db, err
	}
}

func mqttBroker(cfg *config.FlowgentConfig) string {
	if cfg == nil {
		return "disabled"
	}
	if cfg.Messager.MQTT.Broker != "" {
		return cfg.Messager.MQTT.Broker
	}
	return "disabled"
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
