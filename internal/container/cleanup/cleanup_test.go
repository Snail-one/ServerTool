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
		wantSkip    bool
		wantErrPart string
	}{
		{choice: "", wantArgs: []string{"system", "prune", "-f"}},
		{choice: "1", wantArgs: []string{"system", "prune", "-f"}},
		{choice: "2", wantArgs: []string{"container", "prune", "-f"}},
		{choice: "3", wantArgs: []string{"network", "prune", "-f"}},
		{choice: "4", wantArgs: []string{"image", "prune", "-f"}},
		{choice: "5", wantArgs: []string{"image", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "6", wantArgs: []string{"builder", "prune", "-f"}},
		{choice: "7", wantArgs: []string{"system", "prune", "-a", "-f"}, wantConfirm: true},
		{choice: "q", wantSkip: true},
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
			if got.skip != tt.wantSkip {
				t.Fatalf("skip = %v, want %v", got.skip, tt.wantSkip)
			}
		})
	}
}
