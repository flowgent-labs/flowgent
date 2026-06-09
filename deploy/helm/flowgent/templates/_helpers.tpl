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

{{/*
extraSecretEnv iterates over .Values.secrets.extraSecrets and emits env: entries.
Each entry maps a K8s Secret key to an optional env var name.

Format:
  secrets:
    extraSecrets:
      - name: "my-k8s-secret"
        optional: true
        mappings:
          - key: apikey
            env: DEEPSEEK_APIKEY   # optional, defaults to key
*/}}
{{- define "flowgent.extraSecretEnv" -}}
{{- range .Values.secrets.extraSecrets }}
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
{{- if .Values.postgresql.enabled }}
            - name: FLOWGENT__STORAGE__POSTGRES__DSN
              value: {{ include "flowgent.databaseUrl" . | quote }}
{{- end }}
{{- if .Values.emqx.enabled }}
            - name: FLOWGENT__MESSAGER__MQTT__BROKER
              value: {{ include "flowgent.mqttBroker" . | quote }}
{{- end }}
{{- end }}

{{/* Database URL — only used when postgresql.enabled=true (internal) */}}
{{- define "flowgent.databaseUrl" -}}
{{- printf "postgres://%s:%s@%s-postgresql:%d/%s?sslmode=disable" "flowgent" "flowgent" (include "flowgent.fullname" .) 5432 "flowgent" }}
{{- end }}

{{/* MQTT broker URL — only used when emqx.enabled=true (internal) */}}
{{- define "flowgent.mqttBroker" -}}
{{- printf "tcp://%s-emqx:1883" (include "flowgent.fullname" .) }}
{{- end }}

{{/* Wallet master key generation (Ed25519) */}}
{{- define "flowgent.generateWalletKey" -}}
{{- $key := genPrivateKey "ed25519" }}
{{- $_ := set .Values.wallet "generatedMasterKey" ($key | b64enc) }}
{{- $key | b64enc }}
{{- end }}
