package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	downloadProgressWidth   = 24
	downloadRefreshInterval = 100 * time.Millisecond
)

// CopyWithProgress copies a download stream while reporting its progress.
// Known-size responses get a progress bar and percentage; chunked or otherwise
// unknown-size responses show the received byte count and transfer rate.
func CopyWithProgress(destination io.Writer, source io.Reader, output io.Writer, label string, total int64) (int64, error) {
	progress := newDownloadProgress(output, label, total)
	progress.start()

	written, err := io.Copy(&progressWriter{destination: destination, progress: progress}, source)
	progress.finish(err == nil)
	return written, err
}

type progressWriter struct {
	destination io.Writer
	progress    *downloadProgress
}

func (writer *progressWriter) Write(data []byte) (int, error) {
	written, err := writer.destination.Write(data)
	writer.progress.add(int64(written))
	return written, err
}

type downloadProgress struct {
	output      io.Writer
	label       string
	total       int64
	written     int64
	startedAt   time.Time
	lastUpdate  time.Time
	interactive bool
}

func newDownloadProgress(output io.Writer, label string, total int64) *downloadProgress {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "下载"
	}
	return &downloadProgress{
		output:      output,
		label:       label,
		total:       total,
		interactive: progressTerminal(output),
	}
}

func (progress *downloadProgress) start() {
	progress.startedAt = time.Now()
	progress.lastUpdate = progress.startedAt
	if progress.interactive {
		progress.render(false, true)
	}
}

func (progress *downloadProgress) add(written int64) {
	progress.written += written
	if !progress.interactive {
		return
	}
	now := time.Now()
	if now.Sub(progress.lastUpdate) < downloadRefreshInterval && (progress.total <= 0 || progress.written < progress.total) {
		return
	}
	progress.lastUpdate = now
	progress.render(false, true)
}

func (progress *downloadProgress) finish(success bool) {
	progress.render(success, progress.interactive)
	if progress.interactive && !success {
		fmt.Fprintln(progress.output)
	}
}

func (progress *downloadProgress) render(success, carriageReturn bool) {
	line := progressLine(progress.label, progress.written, progress.total, time.Since(progress.startedAt), success)
	if carriageReturn {
		fmt.Fprintf(progress.output, "\r\033[2K%s", line)
	} else {
		fmt.Fprint(progress.output, line)
	}
	if success || !carriageReturn {
		fmt.Fprintln(progress.output)
	}
}

func progressLine(label string, written, total int64, elapsed time.Duration, complete bool) string {
	rate := float64(0)
	if elapsed > 0 {
		rate = float64(written) / elapsed.Seconds()
	}
	if total > 0 {
		percent := int(float64(written) * 100 / float64(total))
		if percent > 100 {
			percent = 100
		}
		filled := percent * downloadProgressWidth / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", downloadProgressWidth-filled)
		return fmt.Sprintf("%s：[%s] %3d%%  %s/%s  %s/s", label, bar, percent, formatBytes(written), formatBytes(total), formatBytes(int64(rate)))
	}
	state := "已下载"
	if complete {
		state = "下载完成"
	}
	return fmt.Sprintf("%s：%s %s  %s/s", label, state, formatBytes(written), formatBytes(int64(rate)))
}

func formatBytes(size int64) string {
	if size < 0 {
		size = 0
	}
	const unit = int64(1024)
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, suffix := range units {
		value /= float64(unit)
		if value < float64(unit) || suffix == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%d B", size)
}

func progressTerminal(output io.Writer) bool {
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
