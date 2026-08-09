package upload

type Progress interface {
	Start(totalArtifacts int) error
	ArtifactStarted(pdbID string, artifactID string) error
	ArtifactDone() error
	EntryDone(pdbID string, entryID string, models int, artifacts int) error
	Finish() error
}

type NoopProgress struct{}

func (NoopProgress) Start(_ int) error {
	return nil
}

func (NoopProgress) ArtifactStarted(_ string, _ string) error {
	return nil
}

func (NoopProgress) ArtifactDone() error {
	return nil
}

func (NoopProgress) EntryDone(_ string, _ string, _ int, _ int) error {
	return nil
}

func (NoopProgress) Finish() error {
	return nil
}
