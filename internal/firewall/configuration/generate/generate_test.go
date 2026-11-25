package generate_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/configuration/generate"
	"snail_tool/internal/firewall/internal/testenv"
	"snail_tool/internal/storage"
	"snail_tool/internal/ui"
)

func TestBuildConfigKeepsPublicAndPrivatePortsSeparate(t *testing.T) {
	for _, choices := range []struct{ public, private bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		content, err := generate.Build([]int{2222, 22000}, choices.public, choices.private)
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			for _, protocol := range []string{"tcp", "udp"} {
				for scope, web := range map[string]bool{"public": choices.public, "private": choices.private} {
					ports := "2222, 22000"
					if web {
						ports += ", 80, 443"
					}
					set := "set snail_" + scope + "_" + family + "_" + protocol + "_ports {\n        type inet_service\n        flags interval\n        elements = { " + ports + " }\n    }"
					if !strings.Contains(content, set) {
						t.Fatalf("%s %s %s ports do not reflect scope choice: web=%v", scope, family, protocol, web)
					}
					if !strings.Contains(content, "meta nfproto "+family+" "+protocol+" dport @snail_"+scope+"_"+family+"_"+protocol+"_ports counter accept") {
						t.Fatalf("missing independent %s %s %s rule", scope, family, protocol)
					}
				}
			}
		}
		for _, required := range []string{
			"#!/usr/sbin/nft -f\n", "set snail_ssh_port {\n        type inet_service\n        flags interval\n        elements = { 2222, 22000 }\n    }",
			"destroy table inet snail", "type filter hook input priority 0; policy drop;",
			"ct state established,related counter accept", `iifname "lo" counter accept`,
			"ip saddr $snail_private_ipv4 goto private_ports", "ip6 saddr $snail_private_ipv6 goto private_ports",
			"udp sport 547 udp dport 546 counter accept", "mld2-listener-report", "nd-router-advert",
			"chain source_allow {", "jump source_allow",
		} {
			if !strings.Contains(content, required) {
				t.Fatalf("missing required rule %q", required)
			}
		}
		if strings.Contains(content, "flush ruleset") || strings.Contains(content, "__") {
			t.Fatal("configuration contains global flush or unexpanded placeholders")
		}
		jump := strings.Index(content, "jump source_allow")
		privateRoute := strings.Index(content, "ip saddr $snail_private_ipv4 goto private_ports")
		if jump < 0 || jump > privateRoute {
			t.Fatal("source exceptions must be evaluated before public/private port restrictions")
		}
		source := content[strings.Index(content, "chain source_allow {"):strings.Index(content, "chain input {")]
		if !strings.Contains(source, "\n        return\n") {
			t.Fatal("unmatched sources must return to normal port filtering")
		}
		for _, line := range strings.Split(source, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "#") && strings.Contains(line, "accept") {
				t.Fatal("source allow examples must remain disabled by default")
			}
		}
	}
	for _, ports := range [][]int{nil, {0}, {65536}, {-1}} {
		if _, err := generate.Build(ports, false, false); err == nil {
			t.Fatalf("invalid SSH ports accepted: %v", ports)
		}
	}
}

func TestConfigureCancellationAndDetectionFailureDoNotWrite(t *testing.T) {
	for _, scenario := range []string{"nonroot", "cancel install", "cancel write", "no SSH", "SSH changed"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			input := "\n\n\n\n1\n"
			if scenario == "nonroot" {
				env.IsRoot = func() bool { return false }
			}
			if scenario == "cancel install" {
				env.CommandExists = func(string) bool { return false }
				input = "n\n"
			}
			if scenario == "cancel write" {
				input = "y\ny\ny\ny\nq\nq\nq\nq\nq\n"
			}
			calls := 0
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if !testenv.IsSSHDQuery(name, args) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				calls++
				if scenario == "no SSH" {
					return "", nil
				}
				port := 2222
				if scenario == "SSH changed" && calls > 1 {
					port = 2200
				}
				return testenv.SSHDConfig(port), nil
			}
			err := env.Generate(testenv.Input(t, input))
			if errors.Is(err, ui.ErrReturnToMenu) {
				err = nil
			}
			wantErr := scenario == "nonroot" || scenario == "no SSH" || scenario == "SSH changed"
			if (err != nil) != wantErr {
				t.Fatalf("err=%v wantErr=%v", err, wantErr)
			}
			if _, err := os.Stat(env.ConfigPath); !os.IsNotExist(err) {
				t.Fatalf("aborted configuration wrote file: %v", err)
			}
		})
	}
}

func TestAskWebDefaultsClosedForPublicAndPrivate(t *testing.T) {
	for _, scope := range []string{"公网", "局域网"} {
		open, err := configuration.AskWeb(testenv.Input(t, "\n"), scope)
		if err != nil || open {
			t.Fatalf("%s Web ports must stay closed on Enter: open=%v err=%v", scope, open, err)
		}
	}
}

func TestAskSSHDefaultsOpenForPublicAndPrivate(t *testing.T) {
	for _, scope := range []string{"公网", "局域网"} {
		open, err := configuration.AskSSH(testenv.Input(t, "\n"), scope, "2222")
		if err != nil || !open {
			t.Fatalf("%s SSH must stay open on Enter: open=%v err=%v", scope, open, err)
		}
		open, err = configuration.AskSSH(testenv.Input(t, "n\n"), scope, "2222")
		if err != nil || open {
			t.Fatalf("%s SSH must close on n: open=%v err=%v", scope, open, err)
		}
	}
	open, err := configuration.AskSSH(testenv.Input(t, "invalid\ny\n"), "公网", "2222")
	if err != nil || !open {
		t.Fatalf("invalid SSH answer must be asked again: open=%v err=%v", open, err)
	}
}

func TestBuildScopedConfigHonorsSSHSwitches(t *testing.T) {
	content, err := generate.BuildScoped([]int{2222}, false, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			publicSet := "set snail_public_" + family + "_" + protocol + "_ports {\n        type inet_service\n        flags interval\n        elements = { 80, 443 }\n    }"
			privateSet := "set snail_private_" + family + "_" + protocol + "_ports {\n        type inet_service\n        flags interval\n        elements = { 2222 }\n    }"
			if !strings.Contains(content, publicSet) || !strings.Contains(content, privateSet) {
				t.Fatalf("closed public SSH with web still open did not split the lists:\n%s", content)
			}
		}
	}
	if !strings.Contains(content, "elements = { 2222 }") || !strings.Contains(content, "当前仅开放 80、443") || !strings.Contains(content, "当前仅开放 SSH 端口") {
		t.Fatal("SSH service port or scope descriptions were lost")
	}

	closed, err := generate.BuildScoped([]int{2222}, false, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			for _, scope := range []string{"public", "private"} {
				empty := "set snail_" + scope + "_" + family + "_" + protocol + "_ports {\n        type inet_service\n        flags interval\n    }"
				if !strings.Contains(closed, empty) {
					t.Fatalf("closed %s %s %s list is not an empty named set", scope, family, protocol)
				}
			}
		}
	}
	if strings.Contains(closed, "elements = { }") {
		t.Fatal("closing both SSH scopes must omit the elements line instead of writing an empty list")
	}
	if !strings.Contains(closed, "set snail_ssh_port {\n        type inet_service\n        flags interval\n        elements = { 2222 }\n    }") || !strings.Contains(closed, "当前未开放端口") {
		t.Fatal("closed SSH scopes must still record the service port")
	}
}

func TestConfigureClosesPublicSSHBeforeWebQuestions(t *testing.T) {
	env := testenv.New(t)
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
	if err := env.Generate(testenv.Input(t, "n\n\n\n\n1\n")); err != nil {
		t.Fatal(err)
	}
	content := storage.ReadFileString(env.ConfigPath)
	public := "set snail_public_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n    }"
	private := "set snail_private_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2222 }\n    }"
	if !strings.Contains(content, public) || !strings.Contains(content, private) || !strings.Contains(content, "set snail_ssh_port {\n        type inet_service\n        flags interval\n        elements = { 2222 }\n    }") {
		t.Fatal("public SSH must close before the web questions, while private SSH and the service-port record stay open")
	}
}

func TestConfigurePreviewsAndReselectsPortsBeforeWriting(t *testing.T) {
	env := testenv.New(t)
	validations := 0
	loads := 0
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if testenv.IsSSHDQuery(name, args) {
			return testenv.SSHDConfig(2222), nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
			loads++
			if validations != 1 || strings.Contains(storage.ReadFileString(env.ConfigPath), "elements = { 2222, 80, 443 }") {
				t.Fatal("must load the validated configuration reflecting the final selection")
			}
			return "", nil
		}
		if name != "nft" || len(args) != 3 || args[0] != "-c" || args[1] != "-f" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		validations++
		if strings.Contains(storage.ReadFileString(args[2]), "elements = { 2222, 80, 443 }") {
			t.Fatal("reselecting no must remove private web ports from written configuration")
		}
		return "", nil
	}
	if err := env.Generate(testenv.Input(t, "\n\ny\ny\n2\n\n3\ninvalid\n\n\nn\nn\n1\n")); err != nil {
		t.Fatal(err)
	}
	if validations != 1 || loads != 1 {
		t.Fatalf("validation=%d loads=%d, want one of each after final confirmation", validations, loads)
	}
}

func TestConfigureStopsOnCheckWriteOrLoadFailure(t *testing.T) {
	for _, scenario := range []string{"check", "write", "load"} {
		t.Run(scenario, func(t *testing.T) {
			env := testenv.New(t)
			if scenario == "write" {
				env.ConfigPath = filepath.Join(t.TempDir(), "missing-directory", "nftables.conf")
			}
			loads := 0
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if testenv.IsSSHDQuery(name, args) {
					return testenv.SSHDConfig(2222), nil
				}
				if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					if scenario == "check" {
						return "syntax error", errors.New("invalid config")
					}
					return "", nil
				}
				if name == "nft" && reflect.DeepEqual(args, []string{"-f", env.ConfigPath}) {
					loads++
					return "netlink failure", errors.New("load failed")
				}
				t.Fatalf("unexpected command: %s %v", name, args)
				return "", nil
			}
			err := env.Generate(testenv.Input(t, "\n\n\n\n1\n"))
			if err == nil {
				t.Fatal("expected error")
			}
			if scenario == "load" {
				if loads != 1 || !strings.Contains(err.Error(), "已写入") || !strings.Contains(err.Error(), "未成功加载") || !strings.Contains(err.Error(), "netlink failure") {
					t.Fatalf("load failure must report saved config and detailed cause: loads=%d err=%v", loads, err)
				}
				if !strings.Contains(storage.ReadFileString(env.ConfigPath), "elements = { 2222 }") {
					t.Fatal("load failure must preserve the generated configuration for retry")
				}
			} else if loads != 0 {
				t.Fatal("check or write failure must not load firewall rules")
			}
		})
	}
}
