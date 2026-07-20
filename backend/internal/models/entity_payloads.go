package models

type ModelPayload struct {
	FileURL  string         `json:"file_url,omitempty"`
	Size     *int64         `json:"size,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type DataPayload struct {
	FileURL  string         `json:"file_url,omitempty"`
	Type     string         `json:"type,omitempty"`
	Size     *int64         `json:"size,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ProgramPayload struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

type MetricsPayload struct {
	RFree *float64 `json:"r_free,omitempty"`
	RWork *float64 `json:"r_work,omitempty"`
	RSCC  *float64 `json:"rscc,omitempty"`
	CC    *float64 `json:"cc,omitempty"`
}
