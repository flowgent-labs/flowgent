{{/*
Flowgent Helm Chart — Shared Helpers
*/}}

{{- define "flowgent.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "flowgent.fullname" -}}
{{- $name := .Chart.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "flowgent.labels" -}}
app.kubernetes.io/name: {{ include "flowgent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "flowgent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "flowgent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Official releases inherit the chart appVersion; local/E2E values may pin a tag. */}}
{{- define "flowgent.image" -}}
{{- printf "%s:%s" .Values.global.image.repository (.Values.global.image.tag | default .Chart.AppVersion) -}}
{{- end }}

{{- define "flowgent.webImage" -}}
{{- printf "%s:%s" .Values.web.image.repository (.Values.web.image.tag | default .Chart.AppVersion) -}}
{{- end }}

{{/* PG secret name: auto-generated when enabled, externalSecret when disabled */}}
{{- define "flowgent.pgSecretName" -}}
{{- if .Values.postgresql.externalSecret }}
{{- .Values.postgresql.externalSecret }}
{{- else }}
{{- printf "%s-pg" (include "flowgent.fullname" .) }}
{{- end }}
{{- end }}

{{/* MQTT secret name */}}
{{- define "flowgent.mqttSecretName" -}}
{{- if .Values.emqx.externalSecret }}
{{- .Values.emqx.externalSecret }}
{{- else }}
{{- printf "%s-mqtt" (include "flowgent.fullname" .) }}
{{- end }}
{{- end }}

{{/* Notification DB encryption key; mounted only into API server + Notifier. */}}
{{- define "flowgent.notificationSecretName" -}}
{{- if .Values.notifier.secretEncryption.existingSecret }}
{{- .Values.notifier.secretEncryption.existingSecret }}
{{- else }}
{{- printf "%s-notification-encryption" (include "flowgent.fullname" .) }}
{{- end }}
{{- end }}

{{- define "flowgent.notificationSecretEnv" -}}
- name: FLOWGENT_NOTIFICATION_KEY_V1
  valueFrom:
    secretKeyRef:
      name: {{ include "flowgent.notificationSecretName" . | quote }}
      key: {{ .Values.notifier.secretEncryption.keyName | quote }}
{{- end }}

{{/* Shared HMAC verifier for contexts produced by the AuthGuard edge. */}}
{{- define "flowgent.authguardAdapterEnv" -}}
{{- $middleware := index .Values "authguard-middleware" -}}
{{- if or $middleware.enabled $middleware.adapter.enabled }}
- name: AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY
  valueFrom:
    secretKeyRef:
      name: {{ required "authguard-middleware.adapter.existingSecret is required when AuthGuard integration is enabled" $middleware.adapter.existingSecret | quote }}
      key: {{ $middleware.adapter.hmacKey | quote }}
{{- end }}
{{- end }}

{{/*
extraSecretEnv iterates over runtime.secrets.kubernetes.env and emits env: entries.
Each entry maps a K8s Secret key to an optional env var name.

Format:
  runtime:
    secrets:
      kubernetes:
        env:
          - name: "my-k8s-secret"
            optional: true
            mappings:
              - key: apikey
                env: DEEPSEEK_APIKEY   # optional, defaults to key
*/}}
{{- define "flowgent.extraSecretEnv" -}}
{{- range .Values.runtime.secrets.kubernetes.env }}
{{- $secretName := .name }}
{{- $optional := .optional | default false }}
{{- range .mappings }}
- name: {{ .env | default .key | quote }}
  valueFrom:
    secretKeyRef:
      name: {{ $secretName | quote }}
      key: {{ .key | quote }}
      optional: {{ $optional }}
{{- end }}
{{- end }}
{{- end }}

{{/*
internalEnv emits FLOWGENT__ prefixed env vars for internal connections.
These use Spring Boot relaxed binding (__ → ., __N__ → [N]) parsed by config.go.

Components that get these: apiserver, jobmanager, taskmanager, sandbox, notifier, controller.
*/}}
{{- define "flowgent.internalEnv" -}}
{{- if .Values.postgresql.externalSecret }}
- name: FLOWGENT__STORAGE__POSTGRES__DSN
  valueFrom:
    secretKeyRef: {name: {{ .Values.postgresql.externalSecret | quote }}, key: url}
{{- else if or .Values.postgresql.enabled .Values.storage.postgres.dsn }}
- name: FLOWGENT__STORAGE__POSTGRES__DSN
  value: {{ include "flowgent.databaseUrl" . | quote }}
{{- end }}
{{- if .Values.emqx.externalSecret }}
- name: FLOWGENT__MESSAGER__MQTT__BROKER
  valueFrom:
    secretKeyRef: {name: {{ .Values.emqx.externalSecret | quote }}, key: broker}
{{- else if or .Values.emqx.enabled .Values.messaging.mqtt.broker }}
- name: FLOWGENT__MESSAGER__MQTT__BROKER
  value: {{ include "flowgent.mqttBroker" . | quote }}
{{- end }}
{{- if eq .Values.cache.provider "Redis" }}
{{- $redisSecret := .Values.redis.externalSecret | default (printf "%s-redis" (include "flowgent.fullname" .)) }}
- name: FLOWGENT_REDIS_PASSWORD
  valueFrom:
    secretKeyRef: {name: {{ $redisSecret | quote }}, key: password}
{{- end }}
{{- include "flowgent.extraSecretEnv" . }}
{{- end }}

{{/*
The Controller constructs execution resources and resolves persisted LLM/MCP
secret references while it starts. Mount the same existing Secret that is
propagated to TaskManager and Sandbox pods; keeping the name in one runtime
setting prevents a second credential configuration path.
*/}}
{{- define "flowgent.runtimeCredentialEnvFrom" -}}
{{- with .Values.runtime.credentialEnvSecret }}
- secretRef:
    name: {{ . | quote }}
{{- end }}
{{- end }}

{{/* Explicit external values take precedence over managed connection defaults. */}}
{{- define "flowgent.databaseUrl" -}}
{{- if .Values.storage.postgres.dsn -}}
{{- .Values.storage.postgres.dsn -}}
{{- else -}}
{{- $name := .Values.postgresql.name | default (printf "%s-postgresql" (include "flowgent.fullname" .)) -}}
{{- printf "postgres://%s:%s@%s.%s.svc.cluster.local:5432/%s?sslmode=disable" (.Values.postgresql.username | urlquery) (.Values.postgresql.password | urlquery) $name (.Values.postgresql.namespace | default .Release.Namespace) .Values.postgresql.database -}}
{{- end -}}
{{- end }}

{{- define "flowgent.mqttBroker" -}}
{{- if .Values.messaging.mqtt.broker -}}
{{- .Values.messaging.mqtt.broker -}}
{{- else -}}
{{- printf "tcp://%s-emqx.%s.svc.cluster.local:1883" (include "flowgent.fullname" .) .Release.Namespace -}}
{{- end -}}
{{- end }}

{{/* ── Runtime Secret Providers: CSI volumes (GCP / AWS) ── */}}
{{- define "flowgent.credentialVolumes" -}}
{{- if .Values.runtime.secrets.gcp.enabled }}
- name: gcp-secrets
  csi:
    driver: secrets-store.csi.k8s.io
    readOnly: true
    volumeAttributes:
      secretProviderClass: {{ include "flowgent.fullname" . }}-gcp-secrets
{{- end }}
{{- if .Values.runtime.secrets.aws.enabled }}
- name: aws-secrets
  csi:
    driver: secrets-store.csi.k8s.io
    readOnly: true
    volumeAttributes:
      secretProviderClass: {{ include "flowgent.fullname" . }}-aws-secrets
{{- end }}
{{- end }}

{{/* ── Runtime Secret Providers: CSI volumeMounts (GCP / AWS) ── */}}
{{- define "flowgent.credentialVolumeMounts" -}}
{{- if .Values.runtime.secrets.gcp.enabled }}
- name: gcp-secrets
  mountPath: {{ .Values.runtime.secrets.gcp.mountPath }}
  readOnly: true
{{- end }}
{{- if .Values.runtime.secrets.aws.enabled }}
- name: aws-secrets
  mountPath: {{ .Values.runtime.secrets.aws.mountPath }}
  readOnly: true
{{- end }}
{{- end }}

{{/* ── Runtime Secret Providers: Vault Agent annotations ── */}}
{{- define "flowgent.vaultAnnotations" -}}
vault.hashicorp.com/agent-inject: "true"
vault.hashicorp.com/role: {{ .Values.runtime.secrets.vault.vaultRole | quote }}
vault.hashicorp.com/agent-init-first: "true"
{{- range .Values.runtime.secrets.vault.secrets }}
vault.hashicorp.com/agent-inject-secret-{{ .fileName }}: {{ .secretPath | quote }}
vault.hashicorp.com/agent-inject-template-{{ .fileName }}: |
  {{ print "{{-" }} with secret {{ .secretPath | quote }} {{ print "-}}" }}
  {{ .fileName | upper }}={{ print "{{" }} .Data.data.{{ .secretKey }} {{ print "}}" }}
  {{ print "{{-" }} end {{ print "-}}" }}
{{- end }}
{{- end }}

{{/* ── Runtime Secret Providers: ServiceAccount annotations ── */}}
{{- define "flowgent.credentialServiceAccountAnnotations" -}}
{{- range $k, $v := .Values.runtime.secrets.serviceAccount.annotations }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end }}
