{{- define "dynamic-pdb.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.backendName" -}}
{{- printf "%s-backend" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.websiteName" -}}
{{- printf "%s-website" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.s3proxyName" -}}
{{- printf "%s-s3proxy" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.migrationsName" -}}
{{- printf "%s-migrations" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.mmseqsJobName" -}}
{{- printf "%s-mmseqs-job" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.mmseqsCacheName" -}}
{{- printf "%s-mmseqs-cache" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.stripApiPrefixName" -}}
{{- printf "%s-strip-api-prefix" (include "dynamic-pdb.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dynamic-pdb.dbSecretName" -}}
{{- default (include "dynamic-pdb.fullname" .) .Values.backend.dbSecretName -}}
{{- end -}}

{{- define "dynamic-pdb.appSecretName" -}}
{{- default (include "dynamic-pdb.backendName" .) .Values.backend.appSecretName -}}
{{- end -}}

{{- define "dynamic-pdb.s3SecretName" -}}
{{- default (printf "%s-s3-data" (include "dynamic-pdb.fullname" .)) .Values.backend.s3SecretName -}}
{{- end -}}

{{- define "dynamic-pdb.s3proxySecretName" -}}
{{- default (printf "%s-s3proxy-aws" (include "dynamic-pdb.fullname" .)) .Values.s3proxy.awsSecretName -}}
{{- end -}}

{{- define "dynamic-pdb.labels" -}}
app.kubernetes.io/part-of: dynamic-pdb
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "dynamic-pdb.backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dynamic-pdb.backendName" . }}
app.kubernetes.io/component: backend
{{- end -}}

{{- define "dynamic-pdb.website.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dynamic-pdb.websiteName" . }}
app.kubernetes.io/component: website
{{- end -}}

{{- define "dynamic-pdb.s3proxy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dynamic-pdb.s3proxyName" . }}
app.kubernetes.io/component: s3proxy
{{- end -}}

{{- define "dynamic-pdb.mmseqsJob.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dynamic-pdb.mmseqsJobName" . }}
app.kubernetes.io/component: mmseqs-job
{{- end -}}
