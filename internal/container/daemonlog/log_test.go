package daemonlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/container/update"
)

func TestWriteDockerDaemonLogConfigCreatesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker", "daemon.json")

	result, err := writeDockerDaemonLogConfig(path, defaultLogRotationConfig(), failOverwritePrompt(t))
	if err != nil {
		t.Fatal(err)
	}
	if !result.written || result.canceled || result.overwrote || result.backupPath != "" {
		t.Fatalf("unexpected result: %#v", result)
	}

	config := readDaemonJSON(t, path)
	assertLogRotationConfig(t, config, "100m", "3")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("daemon.json mode = %v, want 0644", got)
	}
	assertNoBackups(t, path)
}

func TestWriteDockerDaemonLogConfigPreservesExistingFields(t *testing.T) {
	path := writeDaemonJSONFile(t, `{"debug":true,"registry-mirrors":["https://mirror.example"]}`)

	result, err := writeDockerDaemonLogConfig(path, logRotationConfig{maxSize: "50m", maxFile: "5"}, failOverwritePrompt(t))
	if err != nil {
		t.Fatal(err)
	}
	if !result.written || result.canceled || result.overwrote || result.backupPath == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(result.backupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	config := readDaemonJSON(t, path)
	assertLogRotationConfig(t, config, "50m", "5")
	if got, ok := config["debug"].(bool); !ok || !got {
		t.Fatalf("debug field not preserved: %#v", config["debug"])
	}
	mirrors, ok := config["registry-mirrors"].([]any)
	if !ok || len(mirrors) != 1 || mirrors[0] != "https://mirror.example" {
		t.Fatalf("registry-mirrors not preserved: %#v", config["registry-mirrors"])
	}
}

func TestWriteDockerDaemonLogConfigRejectsOverwrite(t *testing.T) {
	original := `{"debug":true,"log-driver":"local"}`
	path := writeDaemonJSONFile(t, original)
	called := false

	result, err := writeDockerDaemonLogConfig(path, defaultLogRotationConfig(), func(status dockerLogStatus) (bool, error) {
		called = true
		if !status.hasLogDriver || status.logDriver != "local" {
			t.Fatalf("unexpected status: %#v", status)
		}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected overwrite prompt")
	}
	if !result.canceled || result.written || !result.overwrote || result.backupPath != "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("file changed after rejected overwrite:\n%s", string(data))
	}
	assertNoBackups(t, path)
}

func TestWriteDockerDaemonLogConfigConfirmsOverwrite(t *testing.T) {
	path := writeDaemonJSONFile(t, `{
  "debug": true,
  "log-driver": "local",
  "log-opts": {
    "max-size": "1m",
    "max-file": "1",
    "mode": "non-blocking"
  }
}`)
	called := false

	result, err := writeDockerDaemonLogConfig(path, logRotationConfig{maxSize: "10m", maxFile: "3"}, func(status dockerLogStatus) (bool, error) {
		called = true
		if !status.hasLogDriver || !status.hasLogOpts {
			t.Fatalf("unexpected status: %#v", status)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected overwrite prompt")
	}
	if !result.written || result.canceled || !result.overwrote || result.backupPath == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(result.backupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	config := readDaemonJSON(t, path)
	assertLogRotationConfig(t, config, "10m", "3")
	if got, ok := config["debug"].(bool); !ok || !got {
		t.Fatalf("debug field not preserved: %#v", config["debug"])
	}
	opts := logOptsMap(t, config)
	if len(opts) != 2 {
		t.Fatalf("log-opts should only contain max-size/max-file, got %#v", opts)
	}
}

func TestWriteDockerDaemonLogConfigRejectsInvalidJSON(t *testing.T) {
	original := `{not-json`
	path := writeDaemonJSONFile(t, original)

	result, err := writeDockerDaemonLogConfig(path, defaultLogRotationConfig(), failOverwritePrompt(t))
	if err == nil || !strings.Contains(err.Error(), "无法解析") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
	if result.written || result.canceled || result.backupPath != "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("invalid JSON file changed:\n%s", string(data))
	}
	assertNoBackups(t, path)
}

func TestDockerLogConfigStatusDetectsDriverOrOpts(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{name: "none", in: map[string]any{"debug": true}, want: false},
		{name: "driver", in: map[string]any{"log-driver": "local"}, want: true},
		{name: "opts", in: map[string]any{"log-opts": map[string]any{"max-size": "1m"}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := dockerLogConfigStatus(tt.in)
			if got != tt.want {
				t.Fatalf("dockerLogConfigStatus() conflict = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogRotationPresets(t *testing.T) {
	tests := []struct {
		choice string
		want   logRotationConfig
		wantOK bool
	}{
		{choice: "1", want: logRotationConfig{maxSize: "100m", maxFile: "3"}, wantOK: true},
		{choice: "2", want: logRotationConfig{maxSize: "50m", maxFile: "5"}, wantOK: true},
		{choice: "3", want: logRotationConfig{maxSize: "10m", maxFile: "3"}, wantOK: true},
		{choice: "4", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			got, ok := logRotationPreset(tt.choice)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("logRotationPreset() = %#v, %v; want %#v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestNormalizeMaxSize(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "100m", want: "100m"},
		{raw: "1K", want: "1k"},
		{raw: "2G", want: "2g"},
		{raw: " 50m ", want: "50m"},
		{raw: "0m", wantErr: true},
		{raw: "100mb", wantErr: true},
		{raw: "m", wantErr: true},
		{raw: "-1m", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := normalizeMaxSize(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalizeMaxSize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeMaxFile(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "1", want: "1"},
		{raw: "99", want: "99"},
		{raw: " 003 ", want: "3"},
		{raw: "0", wantErr: true},
		{raw: "-1", wantErr: true},
		{raw: "+1", wantErr: true},
		{raw: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := normalizeMaxFile(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalizeMaxFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptLogRotationConfigCustom(t *testing.T) {
	view := &fakePromptReader{answers: []string{"4", "250M", "7"}}
	got, skip, err := promptLogRotationConfig(view)
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Fatal("unexpected skip")
	}
	want := logRotationConfig{maxSize: "250m", maxFile: "7"}
	if got != want {
		t.Fatalf("promptLogRotationConfig() = %#v, want %#v", got, want)
	}
}

func TestPromptLogRotationConfigRejectsInvalidCustomInput(t *testing.T) {
	tests := []struct {
		name    string
		answers []string
		errPart string
	}{
		{name: "size", answers: []string{"4", "250mb"}, errPart: "max-size"},
		{name: "file", answers: []string{"4", "250m", "0"}, errPart: "max-file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := promptLogRotationConfig(&fakePromptReader{answers: tt.answers})
			if err == nil || !strings.Contains(err.Error(), tt.errPart) {
				t.Fatalf("expected error containing %q, got %v", tt.errPart, err)
			}
		})
	}
}

func TestConfigureDockerLogRotationCanSkipComposeRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	view := &fakePromptReader{answers: []string{"1"}, confirms: []bool{false}}
	var runCalls []systemRunCall
	batchRan := false

	err := configureDockerLogRotation(
		view,
		path,
		filepath.Dir(path),
		func() bool { return true },
		func(unit string) bool { return unit == "docker.service" },
		func(name string, args ...string) error {
			runCalls = append(runCalls, systemRunCall{name: name, args: append([]string{}, args...)})
			return nil
		},
		func(confirmer update.ComposeRebuildConfirmer, prompt string) error {
			confirmed, err := confirmer.Confirm(prompt)
			if err != nil {
				return err
			}
			batchRan = confirmed
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if batchRan {
		t.Fatal("batch rebuild should be skipped when user rejects confirmation")
	}
	if len(view.confirmPrompts) != 1 || !strings.Contains(view.confirmPrompts[0], "是否立即重建运行中的 Compose 项目") {
		t.Fatalf("unexpected confirm prompts: %#v", view.confirmPrompts)
	}
	wantRuns := []systemRunCall{{name: "systemctl", args: []string{"restart", "docker"}}}
	if !reflect.DeepEqual(runCalls, wantRuns) {
		t.Fatalf("system runs mismatch\ngot:  %#v\nwant: %#v", runCalls, wantRuns)
	}
	assertLogRotationConfig(t, readDaemonJSON(t, path), "100m", "3")
}

func TestConfigureDockerLogRotationConfirmCallsComposeRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	view := &fakePromptReader{answers: []string{"2"}, confirms: []bool{true}}
	rebuildCalled := false

	err := configureDockerLogRotation(
		view,
		path,
		filepath.Dir(path),
		func() bool { return true },
		func(unit string) bool { return unit == "docker.service" },
		func(string, ...string) error { return nil },
		func(confirmer update.ComposeRebuildConfirmer, prompt string) error {
			confirmed, err := confirmer.Confirm(prompt)
			if err != nil {
				return err
			}
			rebuildCalled = confirmed
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rebuildCalled {
		t.Fatal("expected confirmed compose rebuild")
	}
	assertLogRotationConfig(t, readDaemonJSON(t, path), "50m", "5")
}

type fakePromptReader struct {
	answers        []string
	confirms       []bool
	confirmPrompts []string
}

func (f *fakePromptReader) Ask(string) (string, error) {
	if len(f.answers) == 0 {
		return "", nil
	}
	answer := f.answers[0]
	f.answers = f.answers[1:]
	return answer, nil
}

func (f *fakePromptReader) Confirm(prompt string) (bool, error) {
	f.confirmPrompts = append(f.confirmPrompts, prompt)
	if len(f.confirms) == 0 {
		return false, nil
	}
	confirmed := f.confirms[0]
	f.confirms = f.confirms[1:]
	return confirmed, nil
}

type systemRunCall struct {
	name string
	args []string
}

func writeDaemonJSONFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daemon.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readDaemonJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func assertLogRotationConfig(t *testing.T, config map[string]any, maxSize, maxFile string) {
	t.Helper()
	if got := config["log-driver"]; got != dockerLogDriver {
		t.Fatalf("log-driver = %#v, want %q", got, dockerLogDriver)
	}
	opts := logOptsMap(t, config)
	if got := opts["max-size"]; got != maxSize {
		t.Fatalf("max-size = %#v, want %q", got, maxSize)
	}
	if got := opts["max-file"]; got != maxFile {
		t.Fatalf("max-file = %#v, want %q", got, maxFile)
	}
}

func logOptsMap(t *testing.T, config map[string]any) map[string]any {
	t.Helper()
	opts, ok := config["log-opts"].(map[string]any)
	if !ok {
		t.Fatalf("log-opts is not an object: %#v", config["log-opts"])
	}
	return opts
}

func failOverwritePrompt(t *testing.T) func(dockerLogStatus) (bool, error) {
	t.Helper()
	return func(dockerLogStatus) (bool, error) {
		t.Fatal("overwrite prompt should not be called")
		return false, nil
	}
}

func assertNoBackups(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("unexpected backups: %#v", matches)
	}
}
