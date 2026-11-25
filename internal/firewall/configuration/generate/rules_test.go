package generate_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	rulespkg "snail_tool/internal/firewall/internal/rules"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
)

func TestRegeneratingBaseConfigurationPreservesManagedRules(t *testing.T) {
	env := testenv.New(t)
	wanted := []rulespkg.Rule{{ID: "aaaaaaaa", Kind: "source_port", Source: "192.168.0.0/16", Ports: "21069", Protocol: "udp"}}
	env.RuleConfig(t, wanted)
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if testenv.IsSSHDQuery(name, args) {
			return testenv.SSHDConfig(2222), nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	if err := env.Generate(testenv.Input(t, "\n\n\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	got, err := rulespkg.ManagedRules(storage.ReadFileString(env.ConfigPath))
	if err != nil || !reflect.DeepEqual(got, wanted) {
		t.Fatalf("regeneration lost managed rules: %v %v", got, err)
	}
}

func TestRegenerationDropsUnconvertedPortRules(t *testing.T) {
	env := testenv.New(t)
	kept := rulespkg.Rule{ID: "bbbbbbbb", Kind: "source_port", Source: "192.168.0.0/16", Ports: "21069", Protocol: "udp"}
	legacy := rulespkg.Rule{ID: "aaaaaaaa", Kind: "port", Ports: "3100", Protocol: "both"}
	version := env.RuleConfig(t, []rulespkg.Rule{legacy, kept})
	forwardLegacy, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "cccccccc", Kind: "port", Family: "ipv4", Ports: "8211", Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	forwardKept, err := rulespkg.NormalizeAllowRule(rulespkg.Rule{ID: "dddddddd", Kind: "source", Source: "192.0.2.10", Protocol: "both"})
	if err != nil {
		t.Fatal(err)
	}
	content, err := rulespkg.ReplaceManagedRulesIn(string(version.Content), []rulespkg.Rule{forwardLegacy, forwardKept}, rulespkg.ForwardBlock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.ConfigPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if testenv.IsSSHDQuery(name, args) {
			return testenv.SSHDConfig(2222), nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	if err := env.Generate(testenv.Input(t, "\n\n\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	saved := storage.ReadFileString(env.ConfigPath)
	got, err := rulespkg.ManagedRules(saved)
	if err != nil || !reflect.DeepEqual(got, []rulespkg.Rule{kept}) {
		t.Fatalf("access rules = %+v, err=%v", got, err)
	}
	forward, err := rulespkg.ManagedRulesIn(saved, rulespkg.ForwardBlock)
	if err != nil || !reflect.DeepEqual(forward, []rulespkg.Rule{forwardKept}) {
		t.Fatalf("forward rules = %+v, err=%v", forward, err)
	}
	if strings.Contains(saved, "dport 3100") || strings.Contains(saved, "dport 8211") {
		t.Fatal("unconverted port rules were written into the new config")
	}
}
