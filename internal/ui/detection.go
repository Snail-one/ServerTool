package ui

import (
	"fmt"
	"io"
	"time"
)

// DetectionProgress reports each probe before it runs, including elapsed time
// while a slow probe is still waiting for a result.
type DetectionProgress struct {
	output io.Writer
}

func NewDetectionProgress(output io.Writer) *DetectionProgress {
	return &DetectionProgress{output: output}
}

func (progress *DetectionProgress) Step(label string, detect func()) {
	progress.step(label, detect, time.Second)
}

func (progress *DetectionProgress) step(label string, detect func(), interval time.Duration) {
	started := time.Now()
	interactive := progressTerminal(progress.output)
	render := func(waiting bool) {
		state := "正在检测"
		if !waiting {
			state = "检测结束"
		}
		line := fmt.Sprintf("[检测] %s：%s（耗时 %.1f 秒）", label, state, time.Since(started).Seconds())
		if interactive {
			fmt.Fprintf(progress.output, "\r\033[2K%s", line)
			if !waiting {
				fmt.Fprintln(progress.output)
			}
		} else {
			fmt.Fprintln(progress.output, line)
		}
	}
	render(true)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				render(true)
			case <-stop:
				return
			}
		}
	}()
	func() {
		defer func() {
			close(stop)
			<-done
		}()
		detect()
	}()
	render(false)
}
