package frontend

import (
	"fmt"
	"io"

	"github.com/schollz/progressbar/v3"
)

type uploadProgress struct {
	writer        io.Writer
	bar           *progressbar.ProgressBar
	detailVisible bool
}

func newUploadProgress(writer io.Writer) *uploadProgress {
	return &uploadProgress{writer: writer}
}

func (p *uploadProgress) Start(totalArtifacts int) error {
	if totalArtifacts <= 0 {
		return nil
	}
	p.bar = progressbar.NewOptions(
		totalArtifacts,
		progressbar.OptionSetWriter(p.writer),
		progressbar.OptionSetDescription("upload"),
		progressbar.OptionSetWidth(32),
		progressbar.OptionShowCount(),
		progressbar.OptionSetItsString("artifact"),
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

func (p *uploadProgress) ArtifactStarted(pdbID string, artifactID string) error {
	if p.bar == nil {
		return nil
	}
	if err := p.bar.Clear(); err != nil {
		return fmt.Errorf("clear upload progress: %w", err)
	}
	if p.detailVisible {
		if _, err := fmt.Fprint(p.writer, "\x1b[1A\r\x1b[2K"); err != nil {
			return fmt.Errorf("clear upload progress detail: %w", err)
		}
	}
	if _, err := fmt.Fprintf(p.writer, "uploading %s %s\n", pdbID, artifactID); err != nil {
		return fmt.Errorf("write upload progress detail: %w", err)
	}
	p.detailVisible = true
	if err := p.bar.RenderBlank(); err != nil {
		return fmt.Errorf("render upload progress: %w", err)
	}
	return nil
}

func (p *uploadProgress) ArtifactDone() error {
	if p.bar == nil {
		return nil
	}
	if err := p.bar.Add(1); err != nil {
		return fmt.Errorf("advance upload progress: %w", err)
	}
	return nil
}

func (p *uploadProgress) EntryDone(pdbID string, entryID string, models int, artifacts int) error {
	if p.bar == nil {
		return nil
	}
	if err := p.bar.Clear(); err != nil {
		return fmt.Errorf("clear upload progress: %w", err)
	}
	if p.detailVisible {
		if _, err := fmt.Fprint(p.writer, "\x1b[1A\r\x1b[2K"); err != nil {
			return fmt.Errorf("clear upload progress detail: %w", err)
		}
		p.detailVisible = false
	}
	if _, err := fmt.Fprintf(p.writer, "uploaded %s entry_id=%s models=%d artifacts=%d\n", pdbID, entryID, models, artifacts); err != nil {
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
