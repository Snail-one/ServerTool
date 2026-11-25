package cleanup

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestPlanForChoice(t *testing.T) {
	tests := []struct {
		choice      string
		wantArgs    []string
		wantConfirm bool
		wantErrPart string
	}{
		{choice: "", wantErrPart: "无效容器清理选项"},
		{choice: "1", wantArgs: []string{"builder", "prune", "-f"}, wantConfirm: true},
		{choice: "2", wantArgs: []string{"builder", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "3", wantArgs: []string{"image", "prune", "-f"}, wantConfirm: true},
		{choice: "4", wantArgs: []string{"network", "prune", "-f"}, wantConfirm: true},
		{choice: "5", wantArgs: []string{"image", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "6", wantArgs: []string{"container", "prune", "-f"}, wantConfirm: true},
		{choice: "7", wantArgs: []string{"system", "prune", "-f"}, wantConfirm: true},
		{choice: "8", wantArgs: []string{"system", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "9", wantArgs: []string{"system", "prune", "-a", "--volumes", "-f"}, wantConfirm: true},
		{choice: "10", wantArgs: []string{"volume", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "bad", wantErrPart: "无效容器清理选项"},
	}

	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			got, err := planForChoice(tt.choice)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.args, tt.wantArgs) {
				t.Fatalf("args mismatch: got %#v, want %#v", got.args, tt.wantArgs)
			}
			if got.needsConfirm != tt.wantConfirm {
				t.Fatalf("needsConfirm = %v, want %v", got.needsConfirm, tt.wantConfirm)
			}
			if strings.TrimSpace(got.impact) == "" {
				t.Fatal("impact summary is empty")
			}
		})
	}
}

func TestVolumeCleanupRequiresConfirmation(t *testing.T) {
	for _, choice := range []string{"9", "10"} {
		for _, confirmed := range []bool{false, true} {
			t.Run(choice+"/"+map[bool]string{false: "cancel", true: "confirm"}[confirmed], func(t *testing.T) {
				item, err := planForChoice(choice)
				if err != nil {
					t.Fatal(err)
				}
				runs := 0
				executed, err := execute(item, func(prompt string) (bool, error) {
					if !strings.Contains(prompt, "数据卷") || !strings.Contains(prompt, "永久删除") {
						t.Fatalf("volume deletion needs explicit confirmation, got %q", prompt)
					}
					return confirmed, nil
				}, func(name string, args ...string) error {
					runs++
					if name != "docker" || !reflect.DeepEqual(args, item.args) {
						t.Fatalf("unexpected command: %s %v", name, args)
					}
					return nil
				}, "docker")
				if err != nil || executed != confirmed || runs != map[bool]int{false: 0, true: 1}[confirmed] {
					t.Fatalf("executed=%v runs=%d err=%v", executed, runs, err)
				}
			})
		}
	}
}

func TestCleanupConfirmationErrorRunsNoCommand(t *testing.T) {
	item, err := planForChoice("10")
	if err != nil {
		t.Fatal(err)
	}
	executed, err := execute(item, func(string) (bool, error) {
		return true, io.EOF
	}, func(string, ...string) error {
		t.Fatal("command must not run after a confirmation error")
		return nil
	}, "docker")
	if executed || !errors.Is(err, io.EOF) {
		t.Fatalf("executed=%v err=%v", executed, err)
	}
}

func TestCleanupCancelRunsNoCommand(t *testing.T) {
	item, err := planForChoice("7")
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	executed, err := execute(
		item,
		func(string) (bool, error) { return false, nil },
		func(string, ...string) error { runs++; return nil },
		"docker",
	)
	if err != nil || executed || runs != 0 {
		t.Fatalf("executed=%v runs=%d err=%v", executed, runs, err)
	}
}
