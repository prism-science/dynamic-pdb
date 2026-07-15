package internal

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test_should_join_reader_goroutines_before_returning is the deterministic
// regression guard for F8. A child emits one line and exits; the output sink
// parks the reader goroutine on that line. With the fix, streamCommand blocks in
// wg.Wait until the reader is released, so it must NOT return while a reader is
// still live (and therefore never closes the pipes under a live reader). The
// old cmd.Run code returned as soon as the child exited, abandoning the still-
// running reader — this test fails against that version.
func Test_should_join_reader_goroutines_before_returning(t *testing.T) {
	// given a child that emits a barrier line then exits, and a sink that blocks
	// the reader goroutine on that line until the test releases it
	cmd := exec.Command("sh", "-c", `echo barrier; exit 0`)

	readerParked := make(chan struct{})
	releaseReader := make(chan struct{})
	d := &dbTunnelCmd{
		onOutputLine: func(line string) {
			if line == "barrier" {
				close(readerParked)
				<-releaseReader
			}
		},
	}

	// when streamCommand runs while the reader is parked
	returned := make(chan error, 1)
	go func() { returned <- d.streamCommand(cmd) }()

	<-readerParked // the child has now exited, but the reader is still inside the sink

	// then streamCommand must not have returned yet: it has to join the reader first
	select {
	case <-returned:
		t.Fatal("streamCommand returned while a reader goroutine was still live (pipes closed under a live reader)")
	case <-time.After(200 * time.Millisecond):
		// good: still blocked in wg.Wait
	}

	// and once the reader is released it returns cleanly
	close(releaseReader)
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("streamCommand returned error after reader released: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamCommand did not return after its reader was released")
	}
}

// Test_should_capture_every_output_line_when_command_exits_under_concurrent_readers
// is the regression guard for F8: cmd.Run (= Start + Wait) closed the
// stdout/stderr pipe read ends while the asyncOutPipe scanners were still
// reading, so trailing lines were dropped and the reader goroutines were never
// joined. streamCommand joins the readers before cmd.Wait, so every line the
// child emits must be observed. Run under -race to also catch the read-vs-close
// data race the old code exhibited.
func Test_should_capture_every_output_line_when_command_exits_under_concurrent_readers(t *testing.T) {
	// given a command that writes many interleaved stdout/stderr lines then exits
	const linesPerStream = 300
	script := fmt.Sprintf(
		`for i in $(seq 1 %d); do echo "out $i"; echo "err $i" 1>&2; done`,
		linesPerStream,
	)
	cmd := exec.Command("sh", "-c", script)

	var mu sync.Mutex
	captured := make(map[string]int)
	d := &dbTunnelCmd{
		onOutputLine: func(line string) {
			mu.Lock()
			captured[line]++
			mu.Unlock()
		},
	}

	// when the command is streamed to completion
	if err := d.streamCommand(cmd); err != nil {
		t.Fatalf("streamCommand returned error: %v", err)
	}

	// then not a single line emitted by the child is lost
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != linesPerStream*2 {
		t.Fatalf("captured %d distinct lines, want %d (lines lost to early pipe close)",
			len(captured), linesPerStream*2)
	}
	for i := 1; i <= linesPerStream; i++ {
		if got := captured[fmt.Sprintf("out %d", i)]; got != 1 {
			t.Errorf("stdout line %d captured %d times, want 1", i, got)
		}
		if got := captured[fmt.Sprintf("err %d", i)]; got != 1 {
			t.Errorf("stderr line %d captured %d times, want 1", i, got)
		}
	}
}

// Test_should_return_error_when_command_exits_nonzero verifies the failure path
// still surfaces a wrapped error after the readers are joined.
func Test_should_return_error_when_command_exits_nonzero(t *testing.T) {
	// given a command that emits output and then exits non-zero
	cmd := exec.Command("sh", "-c", `echo hello; echo oops 1>&2; exit 3`)

	var mu sync.Mutex
	var lines []string
	d := &dbTunnelCmd{
		onOutputLine: func(line string) {
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		},
	}

	// when
	err := d.streamCommand(cmd)

	// then the non-zero exit is reported and the output read before exit is intact
	if err == nil {
		t.Fatal("expected error from non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "run SSM session") {
		t.Errorf("error %q does not wrap with %q", err.Error(), "run SSM session")
	}

	mu.Lock()
	defer mu.Unlock()
	if !contains(lines, "hello") || !contains(lines, "oops") {
		t.Errorf("expected both 'hello' and 'oops' in captured output, got %v", lines)
	}
}

func contains(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
