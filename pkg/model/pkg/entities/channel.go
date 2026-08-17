package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
)

// NotifyChannelType enumerates supported notification providers.
type NotifyChannelType string

const (
	NotifTelegram NotifyChannelType = "telegram"
	NotifDingTalk NotifyChannelType = "dingtalk"
	NotifSlack    NotifyChannelType = "slack"
	NotifEmail    NotifyChannelType = "email"
	NotifWebhook  NotifyChannelType = "webhook"
)

// NotifyChannelInfo is a persisted notification provider configuration.
type NotifyChannelInfo struct {
	BaseEntity

	Name        string            `json:"name"`
	ChannelType NotifyChannelType `json:"provider" db:"channel_type"`
	Config      map[string]any    `json:"config"`
	Enabled     bool              `json:"enabled"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// ConfiguredSecretFields lets management clients distinguish an unchanged
	// write-only value from an unset value. SealedSecrets is opaque runtime data:
	// it can be decrypted only by services holding the configured master key.
	ConfiguredSecretFields []string            `json:"configured_secret_fields,omitempty" db:"-"`
	ClearSecretFields      []string            `json:"clear_secret_fields,omitempty" db:"-"`
	SealedSecrets          *secretbox.Envelope `json:"sealed_secrets,omitempty" db:"-"`
}

const notifySealedSecretsKey = "__flowgent_sealed_secrets_v1"

type notifySecretRecord struct {
	Envelope *secretbox.Envelope `json:"envelope"`
	Fields   []string            `json:"fields"`
}

var notifySecretFields = map[NotifyChannelType][]string{
	NotifTelegram: {"bot_token"},
	NotifDingTalk: {"webhook_url", "secret"},
	NotifSlack:    {"webhook_url"},
	NotifEmail:    {"password"},
	NotifWebhook:  {"url", "headers"},
}

// NotifyChannelSecretFields returns a copy of the schema-defined write-only
// fields for a provider. Field classification is never inferred from names.
func NotifyChannelSecretFields(provider NotifyChannelType) []string {
	fields := notifySecretFields[provider]
	return append([]string(nil), fields...)
}

// ProtectSecrets extracts schema-defined secrets from Config and replaces them
// with one authenticated envelope suitable for DB persistence.
func (n *NotifyChannelInfo) ProtectSecrets(ctx context.Context, cipher secretbox.ISecretCipher) error {
	if cipher == nil {
		return fmt.Errorf("notification secret encryption is not configured")
	}
	if n.Config == nil {
		n.Config = make(map[string]any)
	}
	secrets := make(map[string]any)
	for _, field := range NotifyChannelSecretFields(n.ChannelType) {
		if value, ok := n.Config[field]; ok && hasSecretValue(value) {
			secrets[field] = value
		}
		delete(n.Config, field)
	}
	delete(n.Config, notifySealedSecretsKey)
	if len(secrets) == 0 {
		n.SealedSecrets = nil
		n.ConfiguredSecretFields = nil
		return nil
	}
	envelope, err := secretbox.SealJSON(ctx, cipher, secrets, n.secretAAD())
	if err != nil {
		return fmt.Errorf("protect notification secrets: %w", err)
	}
	n.SealedSecrets = envelope
	n.ConfiguredSecretFields = sortedKeys(secrets)
	n.Config[notifySealedSecretsKey] = notifySecretRecord{
		Envelope: envelope, Fields: append([]string(nil), n.ConfiguredSecretFields...),
	}
	return nil
}

// ResolveSecrets returns a copy with plaintext fields restored for the
// notification runtime. It never mutates the persisted/public value.
func (n *NotifyChannelInfo) ResolveSecrets(ctx context.Context, cipher secretbox.ISecretCipher) (*NotifyChannelInfo, error) {
	resolved := n.clone()
	envelope, fields, err := resolved.envelope()
	if err != nil {
		return nil, err
	}
	delete(resolved.Config, notifySealedSecretsKey)
	resolved.SealedSecrets = envelope
	if envelope == nil {
		return resolved, nil
	}
	if cipher == nil {
		return nil, fmt.Errorf("notification secret encryption is not configured")
	}
	var secrets map[string]any
	if err := secretbox.OpenJSON(ctx, cipher, envelope, resolved.secretAAD(), &secrets); err != nil {
		return nil, fmt.Errorf("resolve notification secrets: %w", err)
	}
	for key, value := range secrets {
		resolved.Config[key] = value
	}
	resolved.ConfiguredSecretFields = fields
	if len(resolved.ConfiguredSecretFields) == 0 {
		resolved.ConfiguredSecretFields = sortedKeys(secrets)
	}
	return resolved, nil
}

// Redacted returns the management/API representation: secret values and the
// storage-reserved envelope key are absent, while configured field names and
// the opaque runtime envelope remain available.
func (n *NotifyChannelInfo) Redacted() (*NotifyChannelInfo, error) {
	redacted := n.clone()
	envelope, fields, err := redacted.envelope()
	if err != nil {
		return nil, err
	}
	delete(redacted.Config, notifySealedSecretsKey)
	for _, field := range NotifyChannelSecretFields(redacted.ChannelType) {
		delete(redacted.Config, field)
	}
	redacted.SealedSecrets = envelope
	redacted.ClearSecretFields = nil
	redacted.ConfiguredSecretFields = fields
	sort.Strings(redacted.ConfiguredSecretFields)
	return redacted, nil
}

func (n *NotifyChannelInfo) clone() *NotifyChannelInfo {
	copy := *n
	copy.Config = make(map[string]any, len(n.Config))
	for key, value := range n.Config {
		copy.Config[key] = value
	}
	copy.ConfiguredSecretFields = append([]string(nil), n.ConfiguredSecretFields...)
	copy.ClearSecretFields = append([]string(nil), n.ClearSecretFields...)
	return &copy
}

func (n *NotifyChannelInfo) envelope() (*secretbox.Envelope, []string, error) {
	if n.SealedSecrets != nil {
		return n.SealedSecrets, append([]string(nil), n.ConfiguredSecretFields...), nil
	}
	raw, ok := n.Config[notifySealedSecretsKey]
	if !ok || raw == nil {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("decode notification secret envelope: %w", err)
	}
	var record notifySecretRecord
	if err := json.Unmarshal(encoded, &record); err == nil && record.Envelope != nil {
		return record.Envelope, append([]string(nil), record.Fields...), nil
	}
	// Accept the initial envelope-only form for forward migration.
	var envelope secretbox.Envelope
	if err := json.Unmarshal(encoded, &envelope); err != nil || envelope.Ciphertext == "" {
		return nil, nil, fmt.Errorf("decode notification secret envelope")
	}
	return &envelope, NotifyChannelSecretFields(n.ChannelType), nil
}

func (n *NotifyChannelInfo) secretAAD() []byte {
	return []byte(n.Namespace + "\x00" + n.ID + "\x00" + string(n.ChannelType))
}

func hasSecretValue(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return true
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
