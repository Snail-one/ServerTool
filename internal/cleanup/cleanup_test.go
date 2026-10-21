package cleanup

import (
	"errors"
	"reflect"
	"testing"

	"snail_tool/internal/system"
)

func TestAllConfigurationCleanupDeletesBackupsAfterSuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed configuration cleanup"}[fail], func(t *testing.T) {
			var calls []string
			steps := []cleanupStep{{name: "test", run: func(*system.Account) error {
				calls = append(calls, "configuration")
				if fail {
					return errors.New("test failure")
				}
				return nil
			}}}
			err := runAllCleanupSteps(&system.Account{}, steps, func() error {
				calls = append(calls, "backups")
				return nil
			})
			want := []string{"configuration", "backups"}
			if fail {
				want = []string{"configuration"}
			}
			if !reflect.DeepEqual(calls, want) || (err != nil) != fail {
				t.Fatalf("cleanup order=%v err=%v", calls, err)
			}
		})
	}
}
