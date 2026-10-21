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
	t.Cleanup(func() {
		commandExists, commandRun, commandOutput, isRoot, configPath = previousExists, previousRun, previousOutput, previousRoot, previousPath
	})
	t.Setenv("SSH_CONNECTION", "")
	configPath = filepath.Join(t.TempDir(), "nftables.conf")
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
	for _, web := range []bool{false, true} {
		content, err := buildConfig([]int{2222, 22000}, web)
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			if !strings.Contains(content, "define snail_public_"+family+"_tcp_ports = { $snail_ssh_port, $snail_ssh_port_2 }") {
				t.Fatalf("public SSH ports missing or include web ports:\n%s", content)
			}
			private := "$snail_ssh_port, $snail_ssh_port_2"
			if web {
				private += ", 80, 443"
			}
			if !strings.Contains(content, "define snail_private_"+family+"_tcp_ports = { "+private+" }") {
				t.Fatalf("private ports do not reflect choice: web=%v", web)
			}
		}
		for _, required := range []string{
			"#!/usr/sbin/nft -f\n", "define snail_ssh_port = 2222", "define snail_ssh_port_2 = 22000",
			"add table inet snail\nflush table inet snail", "type filter hook input priority 0; policy drop;",
			"ct state established,related counter accept", `iifname "lo" counter accept`,
			"ip saddr $snail_private_ipv4 goto private_ports", "ip6 saddr $snail_private_ipv6 goto private_ports",
			"udp sport 547 udp dport 546 counter accept", "mld2-listener-report", "nd-router-advert",
		} {
			if !strings.Contains(content, required) {
				t.Fatalf("missing required rule %q", required)
			}
		}
		if strings.Contains(content, "flush ruleset") || strings.Contains(content, "__") {
			t.Fatal("configuration contains global flush or unexpanded placeholders")
		}
	}
	for _, ports := range [][]int{nil, {0}, {65536}, {-1}} {
		if _, err := buildConfig(ports, false); err == nil {
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
			content, _ := buildConfig([]int{2222}, true)
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
			backups, _ := filepath.Glob(configPath + ".bak.*")
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

func TestRunCancellationAndDetectionFailureDoNotWrite(t *testing.T) {
	for _, scenario := range []string{"nonroot", "cancel install", "cancel write", "no SSH", "SSH changed"} {
		t.Run(scenario, func(t *testing.T) {
			setupTest(t)
			input := "n\ny\n"
			if scenario == "nonroot" {
				isRoot = func() bool { return false }
			}
			if scenario == "cancel install" {
				commandExists = func(string) bool { return false }
				input = "n\n"
			}
			if scenario == "cancel write" {
				input = "y\nn\n"
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
			err := Run(inputUI(t, input))
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

func TestRunConfirmsInstallAndWritesWithoutApplying(t *testing.T) {
	setupTest(t)
	installed := false
	commandExists = func(name string) bool { return name == "apt-get" || name == "nft" && installed }
	var commands []string
	commandRun = func(name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "apt-get" && len(args) > 0 && args[0] == "install" {
			installed = true
		}
		return nil
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "ss" {
			return `LISTEN 0 128 *:2200 *:* users:(("sshd",pid=1,fd=3))`, nil
		}
		if name == "nft" && len(args) == 3 && args[0] == "-c" && args[1] == "-f" {
			return "", nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return "", nil
	}
	if err := Run(inputUI(t, "y\ny\ny\n")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"apt-get update", "apt-get install -y nftables"}; !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected mutations (must not apply or enable firewall): %v", commands)
	}
	content := shared.ReadFileString(configPath)
	if !strings.Contains(content, "define snail_ssh_port = 2200") || !strings.Contains(content, "$snail_ssh_port, 80, 443") {
		t.Fatal("configuration omitted SSH or requested private web ports")
	}
}
