package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

func portFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("portpolicy/testdata/ports_ruleset.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadedPortsMenuUsesReadOnlyKernelQueries(t *testing.T) {
	setupTest(t)
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("port view must not validate or apply configuration: %s %v", name, args)
		}
		return portFixture(t), nil
	}
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "4\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "公网 IPv4 TCP") || !strings.Contains(output, "DHCPv6 客户端") {
		t.Fatal("menu did not render the loaded-port summary")
	}
}

func capturePortOutput(t *testing.T, run func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous; _ = file.Close() }()
	run()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
