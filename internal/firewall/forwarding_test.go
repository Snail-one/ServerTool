package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/ui"
)

const (
	ipv4Forwarding = "net.ipv4.ip_forward"
	ipv6Forwarding = "net.ipv6.conf.all.forwarding"
)

func writeForwardingValue(t *testing.T, key, value string) {
	t.Helper()
	path := filepath.Join(forwardingProcRoot, strings.ReplaceAll(key, ".", "/"))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestForwardingMenuAndBothCardsRefreshAfterChange(t *testing.T) {
	setupTest(t)
	writeForwardingValue(t, "net.ipv4.ip_forward", "0")
	writeForwardingValue(t, "net.ipv6.conf.all.forwarding", "0")
	commandExists = func(name string) bool { return name == "nft" || name == "sysctl" }
	commandOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "nft":
			if !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
				t.Fatalf("unexpected nft command: %v", args)
			}
			return `{"nftables":[]}`, nil
		case "sysctl":
			if len(args) != 2 || args[0] != "-w" {
				t.Fatalf("unexpected sysctl command: %v", args)
			}
			key, value, _ := strings.Cut(args[1], "=")
			writeForwardingValue(t, key, value)
			return "", nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return "", nil
		}
	}
	output := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "8\n4\n1\ny\n\nq\nq\n")); !errors.Is(err, ui.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "IPv4 已关闭 · IPv6 已关闭") || strings.Count(output, "IPv4 已开启 · IPv6 已关闭") != 3 {
		t.Fatalf("both menus must refresh the kernel state after the success card: %s", output)
	}
}
