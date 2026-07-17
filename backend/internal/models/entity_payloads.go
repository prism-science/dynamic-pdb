package models

type ModelPayload struct {
	FileURL string `json:"file_url,omitempty"`
}

type MetricsPayload struct {
	RFree *float64 `json:"r_free,omitempty"`
	RWork *float64 `json:"r_work,omitempty"`
	RSCC  *float64 `json:"rscc,omitempty"`
	CC    *float64 `json:"cc,omitempty"`
}
