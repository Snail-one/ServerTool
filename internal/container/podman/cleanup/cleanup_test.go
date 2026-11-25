package cleanup

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestUnsupportedChoices(t *testing.T) {
	for _, choice := range []string{"1", "2", "9"} {
		if _, err := planForChoice(choice); err == nil {
			t.Fatalf("Docker-specific cleanup %s must not be offered for Podman", choice)
		}
	}
}

func TestSystemCleanupMentionsPods(t *testing.T) {
	for _, choice := range []string{"7", "8"} {
		item, err := planForChoice(choice)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(item.impact, "Pod") {
			t.Fatalf("missing Pod cleanup impact: %s", item.impact)
		}
		if strings.Contains(strings.Join(item.args, " "), "--volumes") {
			t.Fatal("system cleanup must preserve volumes")
		}
	}
}

func TestVolumeCleanupRequiresConfirmation(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "confirm"}[confirmed], func(t *testing.T) {
			item, err := planForChoice("10")
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
				if name != "podman" || !reflect.DeepEqual(args, item.args) {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				return nil
			}, "podman")
			if err != nil || executed != confirmed || runs != map[bool]int{false: 0, true: 1}[confirmed] {
				t.Fatalf("executed=%v runs=%d err=%v", executed, runs, err)
			}
		})
	}
}

func TestResolveVolumePlan(t *testing.T) {
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
			item, err := planForChoice("10")
			if err != nil {
				t.Fatal(err)
			}
			got, err := resolveVolumePlan(item, func(name string, args ...string) (string, error) {
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
