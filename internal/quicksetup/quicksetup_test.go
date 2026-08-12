package quicksetup

import (
	"errors"
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
