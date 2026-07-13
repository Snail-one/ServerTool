package cleanup

import (
	"reflect"
	"strings"
	"testing"
)

func TestDockerCleanupPlanForChoice(t *testing.T) {
	tests := []struct {
		choice      string
		wantArgs    []string
		wantConfirm bool
		wantErrPart string
	}{
		{choice: "", wantErrPart: "无效容器清理选项"},
		{choice: "1", wantArgs: []string{"container", "prune", "-f"}, wantConfirm: true},
		{choice: "2", wantArgs: []string{"network", "prune", "-f"}, wantConfirm: true},
		{choice: "3", wantArgs: []string{"image", "prune", "-f"}, wantConfirm: true},
		{choice: "4", wantArgs: []string{"builder", "prune", "-f"}, wantConfirm: true},
		{choice: "5", wantArgs: []string{"system", "prune", "-f"}, wantConfirm: true},
		{choice: "6", wantArgs: []string{"image", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "7", wantArgs: []string{"system", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "bad", wantErrPart: "无效容器清理选项"},
	}

	for _, tt := range tests {
		t.Run(tt.choice, func(t *testing.T) {
			got, err := dockerCleanupPlanForChoice(tt.choice)
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

func TestDockerCleanupCancelRunsNoCommand(t *testing.T) {
	plan, err := dockerCleanupPlanForChoice("7")
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	executed, err := executeDockerCleanupPlan(
		plan,
		func(string) (bool, error) { return false, nil },
		func(string, ...string) error { runs++; return nil },
		"docker",
	)
	if err != nil || executed || runs != 0 {
		t.Fatalf("executed=%v runs=%d err=%v", executed, runs, err)
	}
}
