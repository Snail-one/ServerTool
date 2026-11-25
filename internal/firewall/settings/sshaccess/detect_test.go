package sshaccess_test

import (
	"errors"
	"reflect"
	"testing"

	"snail_tool/internal/firewall/internal/testenv"
)

func TestDetectSSHPortsUsesEffectiveConfiguration(t *testing.T) {
	env := testenv.New(t)
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if !testenv.IsSSHDQuery(name, args) {
			t.Fatalf("unexpected query: %s %v", name, args)
		}
		return "port 2222\nport 2200\nlistenaddress 0.0.0.0:2222\nlistenaddress [::]:2222\nlistenaddress 0.0.0.0\nlistenaddress [::]\nlistenaddress [::]:4022\npasswordauthentication yes\n", nil
	}
	got, err := env.SSHAccess().DetectPorts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{2200, 2222, 4022}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH ports = %v, want %v", got, want)
	}
}

func TestDetectSSHPortsUsesConfiguredPortOnly(t *testing.T) {
	env := testenv.New(t)
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if !testenv.IsSSHDQuery(name, args) {
			t.Fatalf("unexpected query: %s %v", name, args)
		}
		return testenv.SSHDConfig(22783), nil
	}
	got, err := env.SSHAccess().DetectPorts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{22783}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH ports = %v, want %v", got, want)
	}
}

func TestDetectListeningSSHPortsUsesLiveListenersAndSocketActivation(t *testing.T) {
	env := testenv.New(t)
	env.CommandExists = func(name string) bool { return name == "systemctl" }
	env.CommandOutput = func(name string, args ...string) (string, error) {
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
	got, err := env.SSHAccess().DetectListeningPorts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{2200, 2222, 2223}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH listeners = %v, want %v", got, want)
	}
}

func TestDetectListeningSSHPortsIgnoresUnrelatedListenerOnAdvertisedSocketPort(t *testing.T) {
	env := testenv.New(t)
	env.CommandExists = func(name string) bool { return name == "systemctl" }
	env.CommandOutput = func(name string, args ...string) (string, error) {
		if name == "ss" {
			return `LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("docker-proxy",pid=9,fd=4))
LISTEN 0 128 *:22783 *:* users:(("sshd",pid=1,fd=3))`, nil
		}
		if name == "systemctl" && len(args) > 1 && args[1] == "ssh.socket" {
			return "0.0.0.0:22 (Stream)", nil
		}
		return "", errors.New("missing unit")
	}
	got, err := env.SSHAccess().DetectListeningPorts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{22783}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH listeners = %v, want %v", got, want)
	}
}

func TestDetectSSHPortsFailsInsteadOfAssumingPort22(t *testing.T) {
	for _, scenario := range []struct {
		name string
		out  string
		err  error
	}{
		{name: "command", err: errors.New("exit status 1")},
		{name: "empty"},
		{name: "invalid port", out: "port 65536\n"},
		{name: "invalid listen", out: "port 22783\nlistenaddress [::]:oops\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			env := testenv.New(t)
			env.CommandOutput = func(name string, args ...string) (string, error) {
				if !testenv.IsSSHDQuery(name, args) {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				return scenario.out, scenario.err
			}
			if ports, err := env.SSHAccess().DetectPorts(); err == nil || len(ports) != 0 {
				t.Fatalf("invalid SSH configuration must stop generation: ports=%v err=%v", ports, err)
			}
		})
	}
}
