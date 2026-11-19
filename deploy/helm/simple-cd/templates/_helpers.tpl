{{- define "simple-cd.name" -}}{{ .Release.Name | trunc 63 | trimSuffix "-" }}{{- end -}}
{{- define "simple-cd.labels" -}}
app.kubernetes.io/name: simple-cd
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}
{{- define "simple-cd.selector" -}}
app.kubernetes.io/name: simple-cd
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "simple-cd.tag" -}}{{ .tag | default .root.Chart.AppVersion }}{{- end -}}
