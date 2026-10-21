package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

func setupTest(t *testing.T) {
	t.Helper()
	previousExists, previousRun, previousOutput, previousRoot, previousPath := commandExists, commandRun, commandOutput, isRoot, configPath
	previousDataDir := dataDir
	t.Cleanup(func() {
		commandExists, commandRun, commandOutput, isRoot, configPath = previousExists, previousRun, previousOutput, previousRoot, previousPath
		dataDir = previousDataDir
	})
	t.Setenv("SSH_CONNECTION", "")
	configPath = filepath.Join(t.TempDir(), "nftables.conf")
	dataDir = filepath.Join(t.TempDir(), "snailtool")
	isRoot = func() bool { return true }
	commandExists = func(name string) bool { return name == "nft" }
	commandRun = func(name string, args ...string) error {
		t.Fatalf("unexpected mutation: %s %v", name, args)
		return nil
	}
	commandOutput = func(name string, args ...string) (string, error) {
		t.Fatalf("unexpected query: %s %v", name, args)
		return "", errors.New("unexpected query")
	}
}

func inputUI(t *testing.T, input string) *ui.UI {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	previous := os.Stdin
	os.Stdin = r
	view := ui.New()
	os.Stdin = previous
	t.Cleanup(func() { _ = r.Close() })
	return view
}

func TestDetectSSHPortsUsesLiveListenersAndSocketActivation(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "systemctl" }
	t.Setenv("SSH_CONNECTION", "192.0.2.10 54321 192.0.2.20 2022")
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "ss" {
			if !reflect.DeepEqual(args, []string{"-H", "-ltnp"}) {
				t.Fatalf("unexpected ss arguments: %v", args)
			}
			return `LISTEN 0 128 0.0.0.0:2222 0.0.0.0:* users:(("sshd",pid=1,fd=3))
LISTEN 0 128 [::]:2222 [::]:* users:(("sshd",pid=1,fd=4))
LISTEN 0 128 [::]:2200 [::]:* users:(("systemd",pid=1,fd=5))
LISTEN 0 128 *:2022 *:*
LISTEN 0 128 *:80 *:* users:(("nginx",pid=2,fd=3))
LISTEN 0 128 [fe80::1%eth0]:2223 [::]:* users:(("sshd",pid=3,fd=4))`, nil
		}
		if name == "systemctl" && args[1] == "ssh.socket" {
			return "[::]:2200 (Stream) [::]:9999 (Stream)", nil
		}
		return "", errors.New("missing unit")
	}
	got, err := detectSSHPorts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{2022, 2200, 2222, 2223}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH ports = %v, want %v", got, want)
	}
}

func TestDetectSSHPortsFailsInsteadOfAssumingPort22(t *testing.T) {
	for _, queryErr := range []error{nil, errors.New("permission denied")} {
		t.Run("query", func(t *testing.T) {
			setupTest(t)
			t.Setenv("SSH_CONNECTION", "192.0.2.10 54321 192.0.2.20 22")
			commandOutput = func(string, ...string) (string, error) {
				return `LISTEN 0 128 *:80 *:* users:(("nginx",pid=2,fd=3))`, queryErr
			}
			if ports, err := detectSSHPorts(); err == nil || len(ports) != 0 {
				t.Fatalf("unidentified SSH listener must stop generation: ports=%v err=%v", ports, err)
			}
		})
	}
}

func TestBuildConfigKeepsPublicAndPrivatePortsSeparate(t *testing.T) {
	for _, choices := range []struct{ public, private bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		content, err := buildConfig([]int{2222, 22000}, choices.public, choices.private)
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			for _, protocol := range []string{"tcp", "udp"} {
				for scope, web := range map[string]bool{"public": choices.public, "private": choices.private} {
					ports := "$snail_ssh_port, $snail_ssh_port_2"
					if web {
						ports += ", 80, 443"
					}
					if !strings.Contains(content, "define snail_"+scope+"_"+family+"_"+protocol+"_ports = { "+ports+" }") {
						t.Fatalf("%s %s %s ports do not reflect scope choice: web=%v", scope, family, protocol, web)
					}
					if !strings.Contains(content, "meta nfproto "+family+" "+protocol+" dport $snail_"+scope+"_"+family+"_"+protocol+"_ports counter accept") {
						t.Fatalf("missing independent %s %s %s rule", scope, family, protocol)
					}
				}
			}
		}
		for _, required := range []string{
			"#!/usr/sbin/nft -f\n", "define snail_ssh_port = 2222", "define snail_ssh_port_2 = 22000",
			"add table inet snail\nflush table inet snail", "type filter hook input priority 0; policy drop;",
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
		if _, err := buildConfig(ports, false, false); err == nil {
			t.Fatalf("invalid SSH ports accepted: %v", ports)
		}
	}
}

func TestWriteConfigValidatesBeforeBackupAndWrite(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid", true: "valid"}[valid], func(t *testing.T) {
			setupTest(t)
			const original = "# original rules\n"
			if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			content, _ := buildConfig([]int{2222}, true, true)
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" || len(args) != 3 || args[0] != "-c" || args[1] != "-f" {
					t.Fatalf("must validate without applying: %s %v", name, args)
				}
				if got := shared.ReadFileString(args[2]); got != content {
					t.Fatal("validated content differs from proposed config")
				}
				if got := shared.ReadFileString(configPath); got != original {
					t.Fatal("file changed before validation")
				}
				if !valid {
					return "syntax error", errors.New("invalid configuration")
				}
				return "", nil
			}
			err := writeConfig(content)
			backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			if !valid {
				if err == nil || shared.ReadFileString(configPath) != original || len(backups) != 0 {
					t.Fatalf("failed validation modified config or made backup: err=%v backups=%v", err, backups)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if shared.ReadFileString(configPath) != content || len(backups) != 1 || shared.ReadFileString(backups[0]) != original {
				t.Fatal("write did not preserve original in backup")
			}
			if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("existing file permissions not preserved: info=%v err=%v", info, err)
			}
		})
	}
}

func TestConfigureCancellationAndDetectionFailureDoNotWrite(t *testing.T) {
	for _, scenario := range []string{"nonroot", "cancel install", "cancel write", "no SSH", "SSH changed"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			input := "n\nn\n1\n"
			if scenario == "nonroot" {
				isRoot = func() bool { return false }
			}
			if scenario == "cancel install" {
				commandExists = func(string) bool { return false }
				input = "n\n"
			}
			if scenario == "cancel write" {
				input = "y\ny\nq\n"
			}
			calls := 0
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "ss" {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				calls++
				if scenario == "no SSH" {
					return "", nil
				}
				port := "2222"
				if scenario == "SSH changed" && calls > 1 {
					port = "2200"
				}
				return `LISTEN 0 128 *:` + port + ` *:* users:(("sshd",pid=1,fd=3))`, nil
			}
			err := configure(inputUI(t, input))
			if errors.Is(err, shared.ErrReturnToMenu) {
				err = nil
			}
			wantErr := scenario == "nonroot" || scenario == "no SSH" || scenario == "SSH changed"
			if (err != nil) != wantErr {
				t.Fatalf("err=%v wantErr=%v", err, wantErr)
			}
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("aborted configuration wrote file: %v", err)
			}
		})
	}
}

func TestRunConfirmsInstallAndWritesThenApplies(t *testing.T) {
	setupTest(t)
	installed := false
	commandExists = func(name string) bool { return name == "apt-get" || name == "nft" && installed }
	var commands []string
	var operations []string
	var validated string
	commandRun = func(name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			installed = true
		}
		return nil
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name == "ss" {
			return `LISTEN 0 128 *:2200 *:* users:(("sshd",pid=1,fd=3))`, nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			operations = append(operations, "check")
			validated = shared.ReadFileString(args[2])
			return "", nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) {
			operations = append(operations, "load")
			if validated == "" || shared.ReadFileString(configPath) != validated {
				t.Fatal("loading must follow validation and writing of the same content")
			}
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	if err := Run(inputUI(t, "y\n1\ny\n\n1\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if want := []string{"apt-get update", "apt-get install -y nftables"}; !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected installation or service commands: %v", commands)
	}
	if !reflect.DeepEqual(operations, []string{"check", "load"}) {
		t.Fatalf("must validate, write and automatically load exactly once: %v", operations)
	}
	content := shared.ReadFileString(configPath)
	if !strings.Contains(content, "define snail_ssh_port = 2200") ||
		!strings.Contains(content, "define snail_public_ipv4_tcp_ports = { $snail_ssh_port, 80, 443 }") ||
		!strings.Contains(content, "define snail_public_ipv4_udp_ports = { $snail_ssh_port, 80, 443 }") ||
		!strings.Contains(content, "define snail_private_ipv4_tcp_ports = { $snail_ssh_port }") ||
		!strings.Contains(content, "define snail_private_ipv4_udp_ports = { $snail_ssh_port }") {
		t.Fatal("configuration did not preserve SSH, public consent and private default-off")
	}
}

func TestAskWebDefaultsClosedForPublicAndPrivate(t *testing.T) {
	for _, scope := range []string{"公网", "局域网"} {
		open, err := askWeb(inputUI(t, "\n"), scope)
		if err != nil || open {
			t.Fatalf("%s Web ports must stay closed on Enter: open=%v err=%v", scope, open, err)
		}
	}
}

func TestEditConfigUsesFirstAvailableEditorAndBacksUp(t *testing.T) {
	for _, available := range [][]string{{"vim", "vi", "nano"}, {"vi", "nano"}, {"nano"}, nil} {
		t.Run(strings.Join(available, "+"), func(t *testing.T) {
			setupTest(t)
			const original = "# existing firewall\n"
			if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			commandExists = func(name string) bool {
				for _, candidate := range available {
					if name == candidate {
						return true
					}
				}
				return false
			}
			launched := ""
			validations := 0
			commandRun = func(name string, args ...string) error {
				launched = name
				if !reflect.DeepEqual(args, []string{configPath}) {
					t.Fatalf("unexpected editor arguments: %v", args)
				}
				return os.WriteFile(configPath, []byte("# edited\n"), 0644)
			}
			commandOutput = func(name string, args ...string) (string, error) {
				if launched == "" || shared.ReadFileString(configPath) != "# edited\n" {
					t.Fatal("validation must run after the editor saves and exits")
				}
				if name != "nft" || !reflect.DeepEqual(args, []string{"-c", "-f", configPath}) {
					t.Fatalf("must check the saved file without applying: %s %v", name, args)
				}
				validations++
				return "", nil
			}
			err := editConfig()
			backups, _ := filepath.Glob(filepath.Join(firewallBackupDir(), "nftables.conf.bak.*"))
			if len(available) == 0 {
				if err == nil || !strings.Contains(err.Error(), "未找到 vim、vi、nano") || launched != "" || validations != 0 || len(backups) != 0 || shared.ReadFileString(configPath) != original {
					t.Fatalf("missing editors must report error without modifications: err=%v editor=%s", err, launched)
				}
				return
			}
			if err != nil || launched != available[0] || validations != 1 || len(backups) != 1 || shared.ReadFileString(backups[0]) != original {
				t.Fatalf("editor or backup incorrect: err=%v editor=%s backups=%v", err, launched, backups)
			}
		})
	}
}

func TestEditConfigValidationFailurePreservesEditsAndReportsDetails(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "vim" }
	commandRun = func(name string, args ...string) error {
		if name != "vim" || !reflect.DeepEqual(args, []string{configPath}) {
			t.Fatalf("unexpected mutation: %s %v", name, args)
		}
		return os.WriteFile(configPath, []byte("invalid nft config\n"), 0644)
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-c", "-f", configPath}) {
			t.Fatalf("must only check configuration: %s %v", name, args)
		}
		return "nftables.conf:1: syntax error", errors.New("exit status 1")
	}
	err := editConfig()
	if err == nil || !strings.Contains(err.Error(), "配置校验失败") || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("missing validation error details: %v", err)
	}
	if shared.ReadFileString(configPath) != "invalid nft config\n" {
		t.Fatal("failed validation must retain edits for correction")
	}
}

func TestRunViewsFilesAndLoadedRulesWithoutSSHDetectionOrWrites(t *testing.T) {
	setupTest(t)
	if err := os.WriteFile(configPath, []byte("# existing rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	queries := 0
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		if name != "nft" || !reflect.DeepEqual(args, []string{"list", "ruleset"}) {
			t.Fatalf("viewing must not detect SSH or alter rules: %s %v", name, args)
		}
		queries++
		return "table inet snail {}", nil
	}
	if err := Run(inputUI(t, "2\n\n4\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if queries != 1 || shared.ReadFileString(configPath) != "# existing rules\n" {
		t.Fatal("viewing must read rules exactly once and preserve the config")
	}
}

func TestConfigurePreviewsAndReselectsPortsBeforeWriting(t *testing.T) {
	setupTest(t)
	validations := 0
	loads := 0
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "ss" {
			return `LISTEN 0 128 *:2222 *:* users:(("sshd",pid=1,fd=3))`, nil
		}
		if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) {
			loads++
			if validations != 1 || strings.Contains(shared.ReadFileString(configPath), "$snail_ssh_port, 80, 443") {
				t.Fatal("must load the validated configuration reflecting the final selection")
			}
			return "", nil
		}
		if name != "nft" || len(args) != 3 || args[0] != "-c" || args[1] != "-f" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		validations++
		if strings.Contains(shared.ReadFileString(args[2]), "$snail_ssh_port, 80, 443") {
			t.Fatal("reselecting no must remove private web ports from written configuration")
		}
		return "", nil
	}
	if err := configure(inputUI(t, "y\ny\n2\n\n3\ninvalid\nn\nn\n1\n")); err != nil {
		t.Fatal(err)
	}
	if validations != 1 || loads != 1 {
		t.Fatalf("validation=%d loads=%d, want one of each after final confirmation", validations, loads)
	}
}

func TestConfigureStopsOnCheckWriteOrLoadFailure(t *testing.T) {
	for _, scenario := range []string{"check", "write", "load"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			if scenario == "write" {
				configPath = filepath.Join(t.TempDir(), "missing-directory", "nftables.conf")
			}
			loads := 0
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "ss" {
					return `LISTEN 0 128 *:2222 *:* users:(("sshd",pid=1,fd=3))`, nil
				}
				if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
					if scenario == "check" {
						return "syntax error", errors.New("invalid config")
					}
					return "", nil
				}
				if name == "nft" && reflect.DeepEqual(args, []string{"-f", configPath}) {
					loads++
					return "netlink failure", errors.New("load failed")
				}
				t.Fatalf("unexpected command: %s %v", name, args)
				return "", nil
			}
			err := configure(inputUI(t, "n\nn\n1\n"))
			if err == nil {
				t.Fatal("expected error")
			}
			if scenario == "load" {
				if loads != 1 || !strings.Contains(err.Error(), "已写入") || !strings.Contains(err.Error(), "未成功加载") || !strings.Contains(err.Error(), "netlink failure") {
					t.Fatalf("load failure must report saved config and detailed cause: loads=%d err=%v", loads, err)
				}
				if !strings.Contains(shared.ReadFileString(configPath), "define snail_ssh_port = 2222") {
					t.Fatal("load failure must preserve the generated configuration for retry")
				}
			} else if loads != 0 {
				t.Fatal("check or write failure must not load firewall rules")
			}
		})
	}
}

func TestLoadConfigChecksAndConfirmsBeforeApplying(t *testing.T) {
	for _, scenario := range []string{"missing", "empty", "invalid", "cancel", "changed", "success", "load failure", "nonroot"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			const content = "table inet snail {}\n"
			if scenario != "missing" {
				initial := content
				if scenario == "empty" {
					initial = " \n"
				}
				if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "nonroot" {
				isRoot = func() bool { return false }
			}
			var commands []string
			commandOutput = func(name string, args ...string) (string, error) {
				if name != "nft" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				switch {
				case reflect.DeepEqual(args, []string{"-c", "-f", configPath}):
					commands = append(commands, "check")
					if scenario == "invalid" {
						return "syntax error", errors.New("exit status 1")
					}
					if scenario == "changed" {
						if err := os.WriteFile(configPath, []byte("# changed\n"), 0644); err != nil {
							t.Fatal(err)
						}
					}
					return "", nil
				case reflect.DeepEqual(args, []string{"-f", configPath}):
					commands = append(commands, "load")
					if scenario == "load failure" {
						return "netlink: permission denied", errors.New("exit status 1")
					}
					return "", nil
				default:
					t.Fatalf("unexpected nft arguments: %v", args)
					return "", nil
				}
			}
			input := "y\n"
			if scenario == "cancel" {
				input = "n\n"
			}
			err := loadConfig(inputUI(t, input))
			wantErr := scenario != "success" && scenario != "cancel"
			if (err != nil) != wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, wantErr)
			}
			var want []string
			switch scenario {
			case "missing", "empty", "nonroot":
			case "success", "load failure":
				want = []string{"check", "load"}
			default:
				want = []string{"check"}
			}
			if !reflect.DeepEqual(commands, want) {
				t.Fatalf("commands=%v, want %v", commands, want)
			}
			if scenario == "invalid" && !strings.Contains(err.Error(), "syntax error") {
				t.Fatalf("validation details missing: %v", err)
			}
			if scenario == "load failure" && !strings.Contains(err.Error(), "netlink: permission denied") {
				t.Fatalf("load error details missing: %v", err)
			}
			if scenario == "success" || scenario == "cancel" || scenario == "invalid" || scenario == "load failure" {
				if shared.ReadFileString(configPath) != content {
					t.Fatal("loading must preserve the saved configuration")
				}
			}
		})
	}
}

func TestRunLoadMenuAppliesLatestFile(t *testing.T) {
	setupTest(t)
	if err := os.WriteFile(configPath, []byte("# latest saved configuration\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		if reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	if err := Run(inputUI(t, "r\ny\n\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if want := []string{"-c -f " + configPath, "-f " + configPath}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("menu commands=%v, want %v", calls, want)
	}
}
