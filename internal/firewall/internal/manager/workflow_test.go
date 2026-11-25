package manager_test

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestRunConfirmsInstallAndWritesThenApplies(t *testing.T) {
	env := testenv.New(t)
	installed := false
	env.CommandExists = func(name string) bool { return name == "apt-get" || name == "nft" && installed }
	var commands []string
	var operations []string
	var validated string
	env.CommandRun = func(name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			installed = true
		}
		return nil
	}
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if testenv.IsSSHDQuery(name, args) {
			return testenv.SSHDConfig(2200), nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			operations = append(operations, "check")
			validated = storage.ReadFileString(args[2])
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
			operations = append(operations, "load")
			if validated == "" || storage.ReadFileString(env.ConfigPath) != validated {
				t.Fatal("loading must follow validation and writing of the same content")
			}
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	if err := env.Run(testenv.Input(t, "y\nd\n\n\ny\n\n1\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if want := []string{"apt-get update", "apt-get install -y nftables"}; !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected installation or service commands: %v", commands)
	}
	if !reflect.DeepEqual(operations, []string{"check", "load"}) {
		t.Fatalf("must validate, write and automatically load exactly once: %v", operations)
	}
	content := storage.ReadFileString(env.ConfigPath)
	if !strings.Contains(content, "elements = { 2200 }") ||
		!strings.Contains(content, "set snail_public_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2200, 80, 443 }\n    }") ||
		!strings.Contains(content, "set snail_public_ipv4_udp_ports {\n        type inet_service\n        flags interval\n        elements = { 2200, 80, 443 }\n    }") ||
		!strings.Contains(content, "set snail_private_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2200 }\n    }") ||
		!strings.Contains(content, "set snail_private_ipv4_udp_ports {\n        type inet_service\n        flags interval\n        elements = { 2200 }\n    }") {
		t.Fatal("configuration did not preserve SSH, public consent and private default-off")
	}
}

func TestRunViewsFilesAndLoadedRulesWithoutSSHDetectionOrWrites(t *testing.T) {
	env := testenv.New(t)
	if err := os.WriteFile(env.ConfigPath, []byte("# existing rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	queries := 0
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name != "nft" || !reflect.DeepEqual(args, []string{"list", "ruleset"}) {
			t.Fatalf("viewing must not detect SSH or alter rules: %s %v", name, args)
		}
		queries++
		return "table inet snail {}", nil
	}
	if err := env.Run(testenv.Input(t, "C\n\n5\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if queries != 1 || storage.ReadFileString(env.ConfigPath) != "# existing rules\n" {
		t.Fatal("viewing must read rules exactly once and preserve the config")
	}
}

func TestRunLoadMenuAppliesLatestFile(t *testing.T) {
	env := testenv.New(t)
	if err := os.WriteFile(env.ConfigPath, []byte("# latest saved configuration\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	if err := env.Run(testenv.Input(t, "r\ny\n\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if want := []string{"-c -f " + env.ConfigPath, "-f " + env.ConfigPath}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("menu commands=%v, want %v", calls, want)
	}
}
