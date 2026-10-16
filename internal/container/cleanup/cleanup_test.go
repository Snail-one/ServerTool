package cleanup

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/shared"
)

func TestDockerCleanupPlanForChoice(t *testing.T) {
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

func TestVolumeCleanupRequiresConfirmation(t *testing.T) {
	for _, tt := range []struct {
		runtimeName string
		choice      string
	}{
		{runtimeName: "docker", choice: "10"},
		{runtimeName: "podman", choice: "10"},
		{runtimeName: "docker", choice: "9"},
	} {
		runtimeName := tt.runtimeName
		for _, confirmed := range []bool{false, true} {
			t.Run(runtimeName+"/"+tt.choice+"/"+map[bool]string{false: "cancel", true: "confirm"}[confirmed], func(t *testing.T) {
				plan, err := cleanupPlanForRuntime(tt.choice, runtimeName)
				if err != nil {
					t.Fatal(err)
				}
				runs := 0
				executed, err := executeDockerCleanupPlan(plan, func(prompt string) (bool, error) {
					if !strings.Contains(prompt, "数据卷") || !strings.Contains(prompt, "永久删除") {
						t.Fatalf("volume deletion needs explicit confirmation, got %q", prompt)
					}
					return confirmed, nil
				}, func(name string, args ...string) error {
					runs++
					if name != runtimeName || !reflect.DeepEqual(args, plan.args) {
						t.Fatalf("unexpected command: %s %v", name, args)
					}
					return nil
				}, runtimeName)
				if err != nil || executed != confirmed || runs != map[bool]int{false: 0, true: 1}[confirmed] {
					t.Fatalf("executed=%v runs=%d err=%v", executed, runs, err)
				}
			})
		}
	}
}

func TestCleanupConfirmationErrorRunsNoCommand(t *testing.T) {
	plan, err := dockerCleanupPlanForChoice("10")
	if err != nil {
		t.Fatal(err)
	}
	executed, err := executeDockerCleanupPlan(plan, func(string) (bool, error) {
		return true, io.EOF
	}, func(string, ...string) error {
		t.Fatal("command must not run after a confirmation error")
		return nil
	}, "docker")
	if executed || !errors.Is(err, io.EOF) {
		t.Fatalf("executed=%v err=%v", executed, err)
	}
}

func TestPodmanCleanupPlans(t *testing.T) {
	for _, choice := range []string{"1", "2", "9"} {
		if _, err := cleanupPlanForRuntime(choice, "podman"); err == nil {
			t.Fatalf("Docker-specific cleanup %s must not be offered for Podman", choice)
		}
	}
	for _, choice := range []string{"7", "8"} {
		plan, err := cleanupPlanForRuntime(choice, "podman")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.impact, "Pod") {
			t.Fatalf("missing Pod cleanup impact: %s", plan.impact)
		}
		if strings.Contains(strings.Join(plan.args, " "), "--volumes") {
			t.Fatal("system cleanup must preserve volumes")
		}
	}
}

func TestResolvePodmanVolumePlan(t *testing.T) {
	for _, tt := range []struct {
		name     string
		help     string
		probeErr error
		wantArgs []string
	}{
		{name: "legacy", help: "Remove all unused volumes\n  -f, --force", wantArgs: []string{"volume", "prune", "-f"}},
		{name: "modern", help: "  -a, --all Remove all unused volumes", wantArgs: []string{"volume", "prune", "-a", "-f"}},
		{name: "probe failed", probeErr: io.EOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := cleanupPlanForRuntime("10", "podman")
			if err != nil {
				t.Fatal(err)
			}
			got, err := resolvePodmanVolumePlan(plan, func(name string, args ...string) (string, error) {
				if name != "podman" || !reflect.DeepEqual(args, []string{"volume", "prune", "--help"}) {
					t.Fatalf("probe must only read help: %s %v", name, args)
				}
				return tt.help, tt.probeErr
			})
			if tt.probeErr != nil {
				if !errors.Is(err, tt.probeErr) || len(got.args) != 0 {
					t.Fatalf("failed probe returned executable plan: %#v, %v", got, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got.args, tt.wantArgs) {
				t.Fatalf("args=%v err=%v", got.args, err)
			}
		})
	}
}

type cleanupRuntimePrompter struct {
	answers []string
}

func (p *cleanupRuntimePrompter) Ask(string) (string, error) {
	if len(p.answers) == 0 {
		return "", io.EOF
	}
	answer := p.answers[0]
	p.answers = p.answers[1:]
	return answer, nil
}

func TestSelectCleanupRuntime(t *testing.T) {
	runtimes := []runtime.Runtime{{Name: "docker", Display: "Docker"}, {Name: "podman", Display: "Podman"}}
	for _, tt := range []struct {
		name     string
		answers  []string
		wantName string
		wantErr  error
	}{
		{name: "docker", answers: []string{"1"}, wantName: "docker"},
		{name: "podman after invalid input", answers: []string{"", "3", "bad", "2"}, wantName: "podman"},
		{name: "return", answers: []string{"q"}, wantErr: shared.ErrReturnToMenu},
		{name: "input error", wantErr: io.EOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectCleanupRuntime(&cleanupRuntimePrompter{answers: tt.answers}, runtimes)
			if got.Name != tt.wantName || !errors.Is(err, tt.wantErr) {
				t.Fatalf("runtime=%v err=%v", got, err)
			}
		})
	}
	got, err := selectCleanupRuntime(&cleanupRuntimePrompter{}, runtimes[1:])
	if err != nil || got.Name != "podman" {
		t.Fatalf("single runtime selection should need no input: %v, %v", got, err)
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
