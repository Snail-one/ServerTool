package quicksetup

import (
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRunSetupStepsRunsInOrder(t *testing.T) {
	var got []string
	steps := []setupStep{
		{name: "SSH 公钥", run: func() error { got = append(got, "keys"); return nil }},
		{name: "SSH 安全策略", run: func() error { got = append(got, "security"); return nil }},
		{name: "Vim 配置", run: func() error { got = append(got, "vim"); return nil }},
		{name: "Bash 配置", run: func() error { got = append(got, "bash"); return nil }},
	}

	if err := runSetupSteps(steps); err != nil {
		t.Fatal(err)
	}
	want := []string{"keys", "security", "vim", "bash"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("setup order = %v, want %v", got, want)
	}
}

func TestRunSetupStepsStopsAfterFailure(t *testing.T) {
	wantErr := errors.New("write failed")
	lastRan := false
	steps := []setupStep{
		{name: "SSH 公钥", run: func() error { return nil }},
		{name: "SSH 安全策略", run: func() error { return wantErr }},
		{name: "Vim 配置", run: func() error { lastRan = true; return nil }},
	}

	err := runSetupSteps(steps)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runSetupSteps() error = %v, want wrapped error", err)
	}
	if !strings.Contains(err.Error(), "SSH 安全策略失败") {
		t.Fatalf("runSetupSteps() error lacks step name: %v", err)
	}
	if lastRan {
		t.Fatal("setup continued after a failed step")
	}
}

func TestPrintSetupSummaryCardIncludesEffectiveSSHDetails(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	summary := setupSummary{
		user:              "admin",
		sshKeys:           true,
		sshKeysPath:       "/home/admin/.ssh/authorized_keys",
		sshSecurity:       true,
		sshConfigSource:   "/usr/sbin/sshd -T",
		sshPort:           "30948",
		sshCommand:        "ssh -p 30948 admin@服务器IP",
		vimConfigured:     true,
		vimConfigPath:     "/home/admin/.vimrc",
		bashConfigured:    true,
		bashConfigPath:    "/home/admin/.bashrc",
		bashSourceCommand: "source /home/admin/.bashrc",
	}

	output := captureQuickSetupStdout(t, func() {
		printSetupSummaryCard(summary)
	})
	for _, want := range []string{
		"╭─ ServerTool",
		"│ 一键配置完成",
		"│ 用户：admin",
		"│ SSH 公钥：[已配置] /home/admin/.ssh/authorized_keys",
		"│ SSH 安全策略：[已配置]",
		"│ SSH 配置：/usr/sbin/sshd -T",
		"│ SSH 端口：30948",
		"│ SSH 登录：ssh -p 30948 admin@服务器IP",
		"│ Vim 配置：[已配置] /home/admin/.vimrc",
		"│ Bash 配置：[已配置] /home/admin/.bashrc",
		"│ Bash 生效：source /home/admin/.bashrc",
		"╰──────────────────────────────────────────────",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary card missing %q:\n%s", want, output)
		}
	}
}

func captureQuickSetupStdout(t *testing.T, run func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = old })

	run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output)
}
