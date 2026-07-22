{{- define "dynamic-pdb.labels" -}}
app.kubernetes.io/part-of: dynamic-pdb
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "dynamic-pdb.backend.selectorLabels" -}}
app.kubernetes.io/name: dynamic-pdb-backend
app.kubernetes.io/component: backend
{{- end -}}

{{- define "dynamic-pdb.website.selectorLabels" -}}
app.kubernetes.io/name: dynamic-pdb-website
app.kubernetes.io/component: website
{{- end -}}
