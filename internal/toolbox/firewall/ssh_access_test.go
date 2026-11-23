package firewall

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"snail_tool/internal/shared"
)

func TestSSHAccessSwitchUpdatesOnlyTheChosenScope(t *testing.T) {
	content, err := buildConfig([]int{2222, 2200}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err = withPingSetting(content, true)
	if err != nil {
		t.Fatal(err)
	}
	forward := content[strings.Index(content, "set snail_forward_public_ipv4_tcp_ports"):]
	closed, changed, err := withSSHAccess(content, true, false)
	if err != nil || !changed {
		t.Fatalf("closing public SSH: changed=%v err=%v", changed, err)
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			publicSet := "set snail_public_" + family + "_" + protocol + "_ports {\n        type inet_service\n        flags interval\n        elements = { 80, 443 }\n    }"
			if !strings.Contains(closed, publicSet) {
				t.Fatalf("public %s %s still contains SSH or lost web ports", family, protocol)
			}
		}
	}
	if !strings.Contains(closed, "set snail_private_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2200, 2222 }\n    }") ||
		!strings.Contains(closed, "set snail_private_ipv6_udp_ports {\n        type inet_service\n        flags interval\n        elements = { 2200, 2222 }\n    }") ||
		!strings.Contains(closed, "set snail_ssh_port {\n        type inet_service\n        flags interval\n        elements = { 2200, 2222 }\n    }") {
		t.Fatal("closing public SSH changed private lists or the service-port record")
	}
	if closed[strings.Index(closed, "set snail_forward_public_ipv4_tcp_ports"):] != forward {
		t.Fatal("SSH access switch changed forward rules")
	}
	ping, found, err := readPingSetting(closed)
	if err != nil || !found || !ping {
		t.Fatal("SSH access switch changed Ping")
	}
	again, changed, err := withSSHAccess(closed, true, false)
	if err != nil || changed || again != closed {
		t.Fatal("closing an already closed scope must not rewrite the config")
	}
	opened, changed, err := withSSHAccess(closed, true, true)
	if err != nil || !changed || !strings.Contains(opened, "elements = { 80, 443, 2200, 2222 }") {
		t.Fatal("reopening public SSH did not restore both service ports beside the web ports")
	}
	ranged, err := buildConfig([]int{2222}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	ranged, err = withNamedSetElements(ranged, "snail_private_ipv4_tcp_ports", "2000-3000, 51820")
	if err != nil {
		t.Fatal(err)
	}
	split, changed, err := withSSHAccess(ranged, false, false)
	if err != nil || !changed || !strings.Contains(split, "set snail_private_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2000-2221, 2223-3000, 51820 }\n    }") {
		t.Fatalf("closing private SSH did not split the covering range: changed=%v err=%v", changed, err)
	}
	if !strings.Contains(split, "elements = { 2222, 80, 443 }") {
		t.Fatal("closing private SSH changed the public lists")
	}
}

func TestSSHAccessStatusReportsPartialLists(t *testing.T) {
	content, err := buildConfig([]int{2222}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err = withNamedSetElements(content, "snail_public_ipv4_tcp_ports", "80")
	if err != nil {
		t.Fatal(err)
	}
	ssh, _ := parsePolicyPorts("2222")
	public, err := sshScopeStatusOf(content, inputScopeLists(true), ssh)
	private, err2 := sshScopeStatusOf(content, inputScopeLists(false), ssh)
	if err != nil || err2 != nil || public != sshScopePartial || private != sshScopeOpen {
		t.Fatalf("status public=%v private=%v err=%v %v", public, private, err, err2)
	}
	opened, changed, err := withSSHAccess(content, true, true)
	if err != nil || !changed {
		t.Fatal(err)
	}
	public, err = sshScopeStatusOf(opened, inputScopeLists(true), ssh)
	if err != nil || public != sshScopeOpen || !strings.Contains(opened, "set snail_public_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 80, 2222 }\n    }") {
		t.Fatal("opening a partial scope did not fill only the missing list")
	}
}

func TestConfigureSSHAccessFromMenu(t *testing.T) {
	setupTest(t)
	content, err := buildConfig([]int{2222}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	calls := acceptRuleCommands(t)
	output := commandOutput
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		return output(name, args...)
	}
	text := capturePortOutput(t, func() {
		if err := Run(inputUI(t, "9\n5\n2\ny\n\nq\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
			t.Fatal(err)
		}
	})
	saved := shared.ReadFileString(configPath)
	if !strings.Contains(saved, "set snail_public_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 80, 443 }\n    }") ||
		!strings.Contains(saved, "set snail_private_ipv4_tcp_ports {\n        type inet_service\n        flags interval\n        elements = { 2222 }\n    }") ||
		!strings.Contains(text, "公网 SSH 已关闭") ||
		!reflect.DeepEqual(*calls, []string{"check", "load"}) {
		t.Fatalf("menu did not close only public SSH: calls=%v", *calls)
	}
	field := sshAccessStateField()
	if field.Label != "SSH 访问" || !strings.Contains(field.Value, "公网"+sshAccessText(sshScopeClosed)) || !strings.Contains(field.Value, "局域网"+sshAccessText(sshScopeOpen)) {
		t.Fatalf("feature status did not reflect the switch: %#v", field)
	}
}

func TestConfigureSSHAccessCancelDoesNotWrite(t *testing.T) {
	setupTest(t)
	content, err := buildConfig([]int{2222}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	commandOutput = func(name string, args ...string) (string, error) {
		if name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			return `{"nftables":[]}`, nil
		}
		t.Fatalf("cancel must not validate or load: %s %v", name, args)
		return "", nil
	}
	if err := Run(inputUI(t, "9\n5\n2\nn\nq\nq\n")); !errors.Is(err, shared.ErrReturnToMenu) {
		t.Fatal(err)
	}
	if shared.ReadFileString(configPath) != content {
		t.Fatal("declining the confirmation rewrote the config")
	}
}
