package upload

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	entryStatusUploading = "uploading"
	entryStatusCompleted = "completed"
)

const (
	uploadEventEntryUploading = "entry_uploading"
	uploadEventEntryCompleted = "entry_completed"
)

type State struct {
	Entries map[string]EntryState `json:"entries"`
}

type EntryState struct {
	Status              string   `json:"status"`
	EntryID             string   `json:"entry_id,omitempty"`
	UploadedModelIDs    []string `json:"uploaded_model_ids,omitempty"`
	UploadedArtifactIDs []string `json:"uploaded_artifact_ids,omitempty"`
	UploadedRunIDs      []string `json:"uploaded_run_ids,omitempty"`
	UploadedMetricIDs   []string `json:"uploaded_metric_ids,omitempty"`
	StartedAt           string   `json:"started_at,omitempty"`
	CompletedAt         string   `json:"completed_at,omitempty"`
}

type uploadStateEvent struct {
	Event       string   `json:"event"`
	PDBID       string   `json:"pdb_id"`
	EntryID     string   `json:"entry_id,omitempty"`
	ModelIDs    []string `json:"model_ids,omitempty"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
	RunIDs      []string `json:"run_ids,omitempty"`
	MetricIDs   []string `json:"metric_ids,omitempty"`
	At          string   `json:"at"`
}

type uploadStateRecorder struct {
	mutex sync.Mutex
	path  string
	state State
}

func newUploadStateRecorder(path string, state State) *uploadStateRecorder {
	state.ensureEntries()
	return &uploadStateRecorder{path: path, state: state}
}

func (r *uploadStateRecorder) startEntry(pdbID string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.state.startEntry(r.path, pdbID)
}

func (r *uploadStateRecorder) completeEntry(result entryUploadResult) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.state.completeEntry(r.path, result)
}

func uploadStatePath(manifestPath string) string {
	extension := filepath.Ext(manifestPath)
	if extension == "" {
		return manifestPath + ".upload.jsonl"
	}
	return strings.TrimSuffix(manifestPath, extension) + ".upload.jsonl"
}

func readUploadState(path string) (State, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newUploadState(), nil
		}
		return State{}, fmt.Errorf("read upload state: %w", err)
	}

	state := newUploadState()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event uploadStateEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return State{}, fmt.Errorf("decode upload state line %d: %w", lineNumber, err)
		}
		if err := state.apply(event); err != nil {
			return State{}, fmt.Errorf("apply upload state line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		closeErr := file.Close()
		return State{}, fmt.Errorf("scan upload state: %w", errors.Join(err, closeErr))
	}
	if err := file.Close(); err != nil {
		return State{}, fmt.Errorf("close upload state: %w", err)
	}
	return state, nil
}

func writeUploadState(path string, state State) error {
	state.ensureEntries()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace upload state: %w", err)
	}
	for _, pdbID := range state.pdbIDs() {
		entryState := state.Entries[pdbID]
		if entryState.Status == "" {
			continue
		}
		if entryState.StartedAt != "" || entryState.Status == entryStatusUploading {
			if err := appendUploadStateEvent(path, uploadStateEvent{
				Event: uploadEventEntryUploading,
				PDBID: pdbID,
				At:    entryState.StartedAt,
			}); err != nil {
				return err
			}
		}
		if entryState.Status == entryStatusCompleted {
			if err := appendUploadStateEvent(path, uploadStateEvent{
				Event:       uploadEventEntryCompleted,
				PDBID:       pdbID,
				EntryID:     entryState.EntryID,
				ModelIDs:    entryState.UploadedModelIDs,
				ArtifactIDs: entryState.UploadedArtifactIDs,
				RunIDs:      entryState.UploadedRunIDs,
				MetricIDs:   entryState.UploadedMetricIDs,
				At:          entryState.CompletedAt,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *State) startEntry(path string, pdbID string) error {
	s.ensureEntries()
	pdbID = canonicalPDBID(pdbID)
	startedAt := currentStateTime()
	s.Entries[pdbID] = EntryState{
		Status:    entryStatusUploading,
		StartedAt: startedAt,
	}
	if err := appendUploadStateEvent(path, uploadStateEvent{
		Event: uploadEventEntryUploading,
		PDBID: pdbID,
		At:    startedAt,
	}); err != nil {
		return err
	}
	return nil
}

func (s *State) completeEntry(path string, result entryUploadResult) error {
	s.ensureEntries()
	completedAt := currentStateTime()
	entryState := s.Entries[result.PDBID]
	entryState.Status = entryStatusCompleted
	entryState.EntryID = result.EntryID
	entryState.UploadedModelIDs = result.ModelIDs
	entryState.UploadedArtifactIDs = result.ArtifactIDs
	entryState.UploadedRunIDs = result.RunIDs
	entryState.UploadedMetricIDs = result.MetricIDs
	entryState.CompletedAt = completedAt
	s.Entries[result.PDBID] = entryState

	if err := appendUploadStateEvent(path, uploadStateEvent{
		Event:       uploadEventEntryCompleted,
		PDBID:       result.PDBID,
		EntryID:     result.EntryID,
		ModelIDs:    result.ModelIDs,
		ArtifactIDs: result.ArtifactIDs,
		RunIDs:      result.RunIDs,
		MetricIDs:   result.MetricIDs,
		At:          completedAt,
	}); err != nil {
		return err
	}
	return nil
}

func appendUploadStateEvent(path string, event uploadStateEvent) error {
	event.PDBID = canonicalPDBID(event.PDBID)
	if event.At == "" {
		event.At = currentStateTime()
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open upload state: %w", err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(event); err != nil {
		closeErr := file.Close()
		return fmt.Errorf("encode upload state event: %w", errors.Join(err, closeErr))
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close upload state: %w", err)
	}
	return nil
}

func (s *State) apply(event uploadStateEvent) error {
	s.ensureEntries()
	pdbID := canonicalPDBID(event.PDBID)
	if pdbID == "" {
		return fmt.Errorf("event %s has empty pdb_id", event.Event)
	}
	switch event.Event {
	case uploadEventEntryUploading:
		s.Entries[pdbID] = EntryState{
			Status:    entryStatusUploading,
			StartedAt: event.At,
		}
	case uploadEventEntryCompleted:
		entryState := s.Entries[pdbID]
		entryState.Status = entryStatusCompleted
		entryState.EntryID = event.EntryID
		entryState.UploadedModelIDs = event.ModelIDs
		entryState.UploadedArtifactIDs = event.ArtifactIDs
		entryState.UploadedRunIDs = event.RunIDs
		entryState.UploadedMetricIDs = event.MetricIDs
		entryState.CompletedAt = event.At
		s.Entries[pdbID] = entryState
	default:
		return fmt.Errorf("unknown event %q", event.Event)
	}
	return nil
}

func (s State) completedEntry(pdbID string) bool {
	entryState, ok := s.Entries[canonicalPDBID(pdbID)]
	return ok && entryState.Status == entryStatusCompleted
}

func (s State) uploadingEntries() []string {
	entries := make([]string, 0)
	for pdbID, entryState := range s.Entries {
		if entryState.Status == entryStatusUploading {
			entries = append(entries, pdbID)
		}
	}
	sort.Strings(entries)
	return entries
}

func (s *State) ensureEntries() {
	if s.Entries == nil {
		s.Entries = map[string]EntryState{}
	}
}

func newUploadState() State {
	return State{Entries: map[string]EntryState{}}
}

func (s State) pdbIDs() []string {
	pdbIDs := make([]string, 0, len(s.Entries))
	for pdbID := range s.Entries {
		pdbIDs = append(pdbIDs, pdbID)
	}
	sort.Strings(pdbIDs)
	return pdbIDs
}

func currentStateTime() string {
	return time.Now().UTC().Format(time.RFC3339)
}
