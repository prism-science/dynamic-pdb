package upload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Report struct {
	UploadedEntryIDs    []string `json:"uploaded_entry_ids"`
	UploadedModelIDs    []string `json:"uploaded_model_ids"`
	UploadedArtifactIDs []string `json:"uploaded_artifact_ids"`
	UploadedRunIDs      []string `json:"uploaded_run_ids"`
	UploadedMetricIDs   []string `json:"uploaded_metric_ids"`
}

func (r *Report) add(result entryUploadResult) {
	if result.CreatedEntryID != "" {
		r.UploadedEntryIDs = append(r.UploadedEntryIDs, result.CreatedEntryID)
	}
	r.UploadedModelIDs = append(r.UploadedModelIDs, result.ModelIDs...)
	r.UploadedArtifactIDs = append(r.UploadedArtifactIDs, result.ArtifactIDs...)
	r.UploadedRunIDs = append(r.UploadedRunIDs, result.RunIDs...)
	r.UploadedMetricIDs = append(r.UploadedMetricIDs, result.MetricIDs...)
}

func uploadReportPath(manifestPath string) string {
	extension := filepath.Ext(manifestPath)
	if extension == "" {
		return manifestPath + ".upload-log.json"
	}
	return strings.TrimSuffix(manifestPath, extension) + ".upload-log.json"
}

func writeUploadReport(path string, report Report) error {
	contents, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode upload report: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write upload report: %w", err)
	}
	return nil
}
