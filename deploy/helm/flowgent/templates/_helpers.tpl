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

{{/* API authorization credentials and runtime-only workload credentials. */}}
{{- define "flowgent.authorizationSecretName" -}}
{{- if .Values.authorization.existingSecret }}
{{- .Values.authorization.existingSecret }}
{{- else }}
{{- printf "%s-authorization" (include "flowgent.fullname" .) }}
{{- end }}
{{- end }}

{{- define "flowgent.runtimeAuthorizationSecretName" -}}
{{- if .Values.authorization.runtimeExistingSecret }}
{{- .Values.authorization.runtimeExistingSecret }}
{{- else }}
{{- printf "%s-runtime-auth" (include "flowgent.fullname" .) }}
{{- end }}
{{- end }}

{{- define "flowgent.workloadTokenEnv" -}}
{{- $root := .root -}}
{{- $component := .component -}}
- name: FLOWGENT_INTERNAL_TOKEN
  valueFrom:
    secretKeyRef:
      {{- if or (eq $component "jobmanager") (eq $component "taskmanager") }}
      name: {{ include "flowgent.runtimeAuthorizationSecretName" $root | quote }}
      key: {{ index $root.Values.authorization (printf "%sKey" $component) | quote }}
      {{- else }}
      name: {{ include "flowgent.authorizationSecretName" $root | quote }}
      key: {{ index $root.Values.authorization (printf "%sKey" $component) | quote }}
      {{- end }}
{{- end }}

{{- define "flowgent.authorizationServerEnv" -}}
- name: FLOWGENT_AUTH_BOOTSTRAP_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "flowgent.authorizationSecretName" . | quote }}
      key: {{ .Values.authorization.bootstrapKey | quote }}
{{- range $component := list "controller" "notifier" "a2a" }}
- name: {{ printf "FLOWGENT_AUTH_%s_TOKEN" (upper $component) }}
  valueFrom:
    secretKeyRef:
      name: {{ include "flowgent.authorizationSecretName" $ | quote }}
      key: {{ index $.Values.authorization (printf "%sKey" $component) | quote }}
{{- end }}
{{- range $component := list "jobmanager" "taskmanager" }}
- name: {{ printf "FLOWGENT_AUTH_%s_TOKEN" (upper $component) }}
  valueFrom:
    secretKeyRef:
      name: {{ include "flowgent.runtimeAuthorizationSecretName" $ | quote }}
      key: {{ index $.Values.authorization (printf "%sKey" $component) | quote }}
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
{{- include "flowgent.extraSecretEnv" . }}
{{- end }}

{{/* Database URL — only used when postgresql.enabled=true (internal) */}}
{{- define "flowgent.databaseUrl" -}}
{{- printf "postgres://%s:%s@%s-postgresql:%d/%s?sslmode=disable" "flowgent" "flowgent" (include "flowgent.fullname" .) 5432 "flowgent" }}
{{- end }}

{{/* MQTT broker URL — only used when emqx.enabled=true (internal) */}}
{{- define "flowgent.mqttBroker" -}}
{{- printf "tcp://%s-emqx:1883" (include "flowgent.fullname" .) }}
{{- end }}

{{/* ── Credential Provider: CSI volumes (GCP / AWS) ── */}}
{{- define "flowgent.credentialVolumes" -}}
{{- if .Values.credentialProviders.gcp.enabled }}
- name: gcp-secrets
  csi:
    driver: secrets-store.csi.k8s.io
    readOnly: true
    volumeAttributes:
      secretProviderClass: {{ include "flowgent.fullname" . }}-gcp-secrets
{{- end }}
{{- if .Values.credentialProviders.aws.enabled }}
- name: aws-secrets
  csi:
    driver: secrets-store.csi.k8s.io
    readOnly: true
    volumeAttributes:
      secretProviderClass: {{ include "flowgent.fullname" . }}-aws-secrets
{{- end }}
{{- end }}

{{/* ── Credential Provider: CSI volumeMounts (GCP / AWS) ── */}}
{{- define "flowgent.credentialVolumeMounts" -}}
{{- if .Values.credentialProviders.gcp.enabled }}
- name: gcp-secrets
  mountPath: {{ .Values.credentialProviders.gcp.mountPath }}
  readOnly: true
{{- end }}
{{- if .Values.credentialProviders.aws.enabled }}
- name: aws-secrets
  mountPath: {{ .Values.credentialProviders.aws.mountPath }}
  readOnly: true
{{- end }}
{{- end }}

{{/* ── Credential Provider: Vault Agent annotations ── */}}
{{- define "flowgent.vaultAnnotations" -}}
vault.hashicorp.com/agent-inject: "true"
vault.hashicorp.com/role: {{ .Values.credentialProviders.vault.vaultRole | quote }}
vault.hashicorp.com/agent-init-first: "true"
{{- range .Values.credentialProviders.vault.secrets }}
vault.hashicorp.com/agent-inject-secret-{{ .fileName }}: {{ .secretPath | quote }}
vault.hashicorp.com/agent-inject-template-{{ .fileName }}: |
  {{ print "{{-" }} with secret {{ .secretPath | quote }} {{ print "-}}" }}
  {{ .fileName | upper }}={{ print "{{" }} .Data.data.{{ .secretKey }} {{ print "}}" }}
  {{ print "{{-" }} end {{ print "-}}" }}
{{- end }}
{{- end }}

{{/* ── Credential Provider: ServiceAccount annotations ── */}}
{{- define "flowgent.credentialServiceAccountAnnotations" -}}
{{- range $k, $v := .Values.credentialProviders.serviceAccount.annotations }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end }}
