package notification

import _ "embed"

// These templates are expanded by Alertmanager, using only presentation annotations.
// Routing labels remain private to the engine and machine-readable webhooks.
const notificationTitle = `Arveld · {{ if eq .Status "resolved" }}Recovered{{ else if eq (len .Alerts.Firing) 1 }}Alert active{{ else }}{{ len .Alerts.Firing }} alerts active{{ end }}{{ with .CommonAnnotations.resource }} · {{ reReplaceAll "[\\r\\n]+" " " . }}{{ end }}{{ if eq (len .Alerts) 1 }}{{ with .CommonAnnotations.summary }} · {{ . }}{{ end }}{{ end }}`

const notificationText = notificationTitle + "\n" + notificationBody

const notificationBody = `{{ range .Alerts }}
{{ if eq .Status "resolved" }}RECOVERED{{ else }}ACTIVE{{ end }} · {{ if .Annotations.resource }}{{ .Annotations.resource }}{{ else }}Resource{{ end }}
{{ if .Annotations.summary }}{{ .Annotations.summary }}{{ else }}Alert condition changed{{ end }}
{{ if eq .Status "resolved" }}This condition is no longer active.{{ else }}{{ .Annotations.description }}{{ end }}
Severity: {{ .Labels.severity }}
Started: {{ .StartsAt.UTC.Format "02 Jan 2006, 15:04:05 UTC" }}{{ if eq .Status "resolved" }}
Recovered: {{ .EndsAt.UTC.Format "02 Jan 2006, 15:04:05 UTC" }}{{ end }}
{{ end }}
Open Arveld → Alerts to investigate.
`

//go:embed templates/email.html
var emailHTML string
