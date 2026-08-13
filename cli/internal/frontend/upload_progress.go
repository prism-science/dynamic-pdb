package frontend

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

type uploadProgress struct {
	mutex     sync.Mutex
	writer    io.Writer
	bar       *progressbar.ProgressBar
	events    chan struct{}
	done      chan struct{}
	started   time.Time
	total     int
	doneCount int
	firstErr  error
	closed    bool
}

func newUploadProgress(writer io.Writer) *uploadProgress {
	return &uploadProgress{writer: writer}
}

func (p *uploadProgress) Start(totalEntries int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if totalEntries <= 0 {
		return nil
	}
	p.started = time.Now()
	p.total = totalEntries
	p.doneCount = 0
	p.events = make(chan struct{}, totalEntries)
	p.done = make(chan struct{})
	p.bar = progressbar.NewOptions(
		totalEntries,
		progressbar.OptionSetWriter(p.writer),
		progressbar.OptionSetDescription(p.description()),
		progressbar.OptionSetWidth(32),
		progressbar.OptionShowCount(),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetElapsedTime(false),
		progressbar.OptionThrottle(250*time.Millisecond),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
	if err := p.bar.RenderBlank(); err != nil {
		return err
	}
	go p.run()
	return nil
}

func (p *uploadProgress) EntryDone(_ string, _ string, _ int, _ int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.events == nil || p.closed {
		return nil
	}
	select {
	case p.events <- struct{}{}:
	default:
	}
	return nil
}

func (p *uploadProgress) run() {
	defer close(p.done)
	for range p.events {
		if err := p.writeEntryDone(); err != nil {
			p.rememberError(err)
			return
		}
	}
	if p.bar != nil {
		if err := p.bar.Finish(); err != nil {
			p.rememberError(fmt.Errorf("finish upload progress: %w", err))
		}
	}
}

func (p *uploadProgress) writeEntryDone() error {
	p.doneCount++
	if err := p.bar.Add(1); err != nil {
		return fmt.Errorf("advance upload progress: %w", err)
	}
	p.bar.Describe(p.description())
	return nil
}

func (p *uploadProgress) description() string {
	if p.total <= 0 {
		return "upload"
	}
	elapsed := time.Since(p.started)
	if p.doneCount <= 0 {
		return "upload elapsed=" + formatProgressDuration(elapsed) + " eta=?"
	}
	average := elapsed / time.Duration(p.doneCount)
	remaining := average * time.Duration(p.total-p.doneCount)
	return "upload elapsed=" + formatProgressDuration(elapsed) +
		" avg=" + formatProgressDuration(average) + "/entry" +
		" eta=" + formatProgressDuration(remaining)
}

func formatProgressDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	duration = duration.Round(time.Second)
	hours := int(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	seconds := int(duration / time.Second)
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (p *uploadProgress) rememberError(err error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.firstErr == nil {
		p.firstErr = err
	}
}

func (p *uploadProgress) Finish() error {
	p.mutex.Lock()
	if p.events == nil || p.closed {
		err := p.firstErr
		p.mutex.Unlock()
		return err
	}
	events := p.events
	done := p.done
	p.closed = true
	p.mutex.Unlock()

	close(events)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		return nil
	}

	p.mutex.Lock()
	err := p.firstErr
	p.mutex.Unlock()
	return err
}
