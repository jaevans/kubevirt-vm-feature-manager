{{/*
Expand the name of the chart.
*/}}
{{- define "vm-feature-manager.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "vm-feature-manager.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "vm-feature-manager.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "vm-feature-manager.labels" -}}
helm.sh/chart: {{ include "vm-feature-manager.chart" . }}
{{ include "vm-feature-manager.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "vm-feature-manager.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vm-feature-manager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "vm-feature-manager.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "vm-feature-manager.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Webhook service name
*/}}
{{- define "vm-feature-manager.webhookServiceName" -}}
{{- include "vm-feature-manager.fullname" . }}-webhook
{{- end }}

{{/*
Certificate secret name
*/}}
{{- define "vm-feature-manager.certificateSecretName" -}}
{{- include "vm-feature-manager.fullname" . }}-tls
{{- end }}

{{/*
Certificate name
*/}}
{{- define "vm-feature-manager.certificateName" -}}
{{- include "vm-feature-manager.fullname" . }}-cert
{{- end }}

{{/*
Issuer name
*/}}
{{- define "vm-feature-manager.issuerName" -}}
{{- include "vm-feature-manager.fullname" . }}-issuer
{{- end }}
