package ui

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type detectionRecorder struct {
	mu     sync.Mutex
	output bytes.Buffer
	lines  chan string
}

func (recorder *detectionRecorder) Write(data []byte) (int, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	n, err := recorder.output.Write(data)
	recorder.lines <- string(data)
	return n, err
}

func (recorder *detectionRecorder) text() string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.output.String()
}

func TestDetectionProgressReportsWhileProbeIsBlocked(t *testing.T) {
	recorder := &detectionRecorder{lines: make(chan string, 100)}
	progress := NewDetectionProgress(recorder)
	release := make(chan struct{})
	var releaseOnce sync.Once
	finishProbe := func() { releaseOnce.Do(func() { close(release) }) }
	defer finishProbe()
	entered := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		progress.step("Docker 服务（docker info）", func() {
			entered <- recorder.text()
			<-release
		}, 10*time.Millisecond)
	}()

	select {
	case output := <-entered:
		if !strings.Contains(output, "Docker 服务（docker info）：正在检测") {
			t.Fatalf("probe started before its label was visible: %q", output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start")
	}

	// Both the initial line and a timer update must appear before the probe returns.
	for i := 0; i < 2; i++ {
		select {
		case line := <-recorder.lines:
			if !strings.Contains(line, "正在检测") || !strings.Contains(line, "耗时") {
				t.Fatalf("missing live waiting status: %q", line)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("blocked probe did not update its elapsed time")
		}
	}
	finishProbe()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("progress did not stop after the probe returned")
	}
	output := recorder.text()
	if !strings.Contains(output, "Docker 服务（docker info）：检测结束") {
		t.Fatalf("missing final elapsed time: %q", output)
	}
	if strings.Contains(output, "\033[") || strings.Contains(output, "\r") {
		t.Fatalf("redirected output contains terminal escapes: %q", output)
	}
}
