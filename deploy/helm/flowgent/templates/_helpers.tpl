{{/*
Flowgent Helm Chart — Shared Helpers
*/}}

{{- define "flowgent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "flowgent.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "flowgent.labels" -}}
app.kubernetes.io/name: {{ include "flowgent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "flowgent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "flowgent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Database URL construction */}}
{{- define "flowgent.databaseUrl" -}}
{{- if .Values.global.databaseUrl }}
{{- .Values.global.databaseUrl }}
{{- else if .Values.postgresql.enabled }}
{{- printf "postgres://%s:%s@%s:%d/%s?sslmode=disable" .Values.postgresql.username .Values.postgresql.password .Values.postgresql.host (int .Values.postgresql.port) .Values.postgresql.database }}
{{- end }}
{{- end }}

{{/* MQTT broker URL */}}
{{- define "flowgent.mqttBroker" -}}
{{- if .Values.global.mqttBroker }}
{{- .Values.global.mqttBroker }}
{{- else if .Values.emqx.enabled }}
{{- .Values.emqx.broker }}
{{- end }}
{{- end }}

{{/* Wallet master key generation (Ed25519) */}}
{{- define "flowgent.generateWalletKey" -}}
{{- $key := genPrivateKey "ed25519" }}
{{- $_ := set .Values.wallet "generatedMasterKey" ($key | b64enc) }}
{{- $key | b64enc }}
{{- end }}
