package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestCopyWithProgressKnownSize(t *testing.T) {
	var destination bytes.Buffer
	var output bytes.Buffer

	written, err := CopyWithProgress(&destination, strings.NewReader("download"), &output, "下载测试文件", 8)
	if err != nil {
		t.Fatal(err)
	}
	if written != 8 || destination.String() != "download" {
		t.Fatalf("复制结果异常：written=%d data=%q", written, destination.String())
	}
	progress := output.String()
	if !strings.Contains(progress, "下载测试文件") || !strings.Contains(progress, "100%") || !strings.Contains(progress, "8 B/8 B") {
		t.Fatalf("已知大小的下载进度异常：%q", progress)
	}
}

func TestCopyWithProgressUnknownSize(t *testing.T) {
	var destination bytes.Buffer
	var output bytes.Buffer

	_, err := CopyWithProgress(&destination, strings.NewReader("chunked"), &output, "下载分块响应", -1)
	if err != nil {
		t.Fatal(err)
	}
	progress := output.String()
	if !strings.Contains(progress, "下载分块响应：下载完成 7 B") {
		t.Fatalf("未知大小的下载进度异常：%q", progress)
	}
}

func TestProgressLineCapsPercentage(t *testing.T) {
	line := progressLine("下载", 12, 10, time.Second, true)
	if !strings.Contains(line, "100%") || strings.Contains(line, "120%") {
		t.Fatalf("进度百分比未正确限制：%q", line)
	}
}

func TestFormatBytes(t *testing.T) {
	for _, item := range []struct {
		size int64
		want string
	}{
		{size: 999, want: "999 B"},
		{size: 1024, want: "1.0 KiB"},
		{size: 3 * 1024 * 1024, want: "3.0 MiB"},
	} {
		if got := formatBytes(item.size); got != item.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", item.size, got, item.want)
		}
	}
}
