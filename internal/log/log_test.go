package log

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestLogUsesChineseLabelsAndHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	stdout := captureOutput(t, &os.Stdout, func() {
		Info("完成")
		Warn("注意")
	})
	stderr := captureOutput(t, &os.Stderr, func() {
		Error("失败")
	})
	if stdout != "[信息] 完成\n[警告] 注意\n" || stderr != "[错误] 失败\n" {
		t.Fatalf("日志格式不一致：stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "\033[") {
		t.Fatalf("NO_COLOR 模式包含 ANSI 转义：%q", stdout+stderr)
	}
}

func captureOutput(t *testing.T, target **os.File, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := *target
	*target = writer
	t.Cleanup(func() { *target = previous })
	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}
