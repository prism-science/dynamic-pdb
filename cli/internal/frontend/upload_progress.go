package frontend

import (
	"fmt"
	"io"

	"github.com/schollz/progressbar/v3"
)

type uploadProgress struct {
	writer io.Writer
	bar    *progressbar.ProgressBar
}

func newUploadProgress(writer io.Writer) *uploadProgress {
	return &uploadProgress{writer: writer}
}

func (p *uploadProgress) Start(totalEntries int) error {
	if totalEntries <= 0 {
		return nil
	}
	p.bar = progressbar.NewOptions(
		totalEntries,
		progressbar.OptionSetWriter(p.writer),
		progressbar.OptionSetDescription("upload"),
		progressbar.OptionSetWidth(32),
		progressbar.OptionShowCount(),
		progressbar.OptionSetItsString("entry"),
		progressbar.OptionShowIts(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
	return p.bar.RenderBlank()
}

func (p *uploadProgress) EntryDone(pdbID string, entryID string, models int, artifacts int) error {
	if p.bar == nil {
		return nil
	}
	if err := p.bar.Add(1); err != nil {
		return fmt.Errorf("advance upload progress: %w", err)
	}
	if _, err := fmt.Fprintf(p.writer, "\nuploaded %s entry_id=%s models=%d artifacts=%d\n", pdbID, entryID, models, artifacts); err != nil {
		return fmt.Errorf("write upload progress detail: %w", err)
	}
	return nil
}

func (p *uploadProgress) Finish() error {
	if p.bar == nil {
		return nil
	}
	if err := p.bar.Finish(); err != nil {
		return fmt.Errorf("finish upload progress: %w", err)
	}
	return nil
}
