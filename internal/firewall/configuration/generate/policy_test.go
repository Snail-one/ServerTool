package generate_test

import (
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/access"
	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
)

func TestInitialGenerationSupportsAdvancedPolicyDraft(t *testing.T) {
	env := testenv.New(t)
	loads := 0
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if testenv.IsSSHDQuery(name, args) {
			return testenv.SSHDConfig(2222), nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" {
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
			loads++
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	// First generation -> custom private IPv6 UDP -> return to final confirmation -> save.
	if err := env.Generate(testenv.Input(t, "\n\nn\nn\n4\n3\n2\n2\n1\n51820\nl\n2\n3\nfd12::/48\ns\ny\n1\n")); err != nil {
		t.Fatal(err)
	}
	content := storage.ReadFileString(env.ConfigPath)
	value, err := rulespkg.ReadPolicyValue(content, access.Lists[7])
	if err != nil || value != "2222, 51820" || loads != 1 || !strings.Contains(content, "define snail_private_ipv6 = { fd12::/48 }") {
		t.Fatalf("custom generation failed: ports=%q loads=%d err=%v", value, loads, err)
	}
}
