package upload

type Progress interface {
	Start(totalEntries int) error
	EntryDone(pdbID string, entryID string, models int, artifacts int) error
	Finish() error
}

type NoopProgress struct{}

func (NoopProgress) Start(_ int) error {
	return nil
}

func (NoopProgress) EntryDone(_ string, _ string, _ int, _ int) error {
	return nil
}

func (NoopProgress) Finish() error {
	return nil
}
