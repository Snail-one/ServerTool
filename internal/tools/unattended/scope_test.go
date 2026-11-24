package unattended

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateScopeWithAPT(t *testing.T) {
	aptConfig, err := exec.LookPath("apt-config")
	if err != nil {
		t.Skip("apt-config is unavailable")
	}
	dir := t.TempDir()
	parts := filepath.Join(dir, "apt.conf.d")
	if err := os.Mkdir(parts, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mainConfig := filepath.Join(dir, "apt.conf")
	startup := filepath.Join(dir, "test.conf")
	write(startup, fmt.Sprintf("Dir::Etc::Parts %q;\nDir::Etc::Main %q;\n", parts, mainConfig))
	t.Setenv("APT_CONFIG", startup)
	service := manager{
		path: filepath.Join(parts, filepath.Base(configPath)),
		query: func(_ string, args ...string) (string, error) {
			output, err := exec.Command(aptConfig, args...).CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("apt-config: %w: %s", err, output)
			}
			return string(output), nil
		},
	}
	write(service.path, managedHeader+`Unattended-Upgrade::Origins-Pattern { "origin=Custom"; };`)
	write(filepath.Join(parts, "50unattended-upgrades"), `
Unattended-Upgrade::Allowed-Origins { "Debian:stable"; };
Unattended-Upgrade::Package-Blacklist { "^custom-package$"; };
`)
	read := func() (updateScope, []byte, bool) {
		t.Helper()
		scope, previous, exists, err := service.readScope()
		if err != nil {
			t.Fatal(err)
		}
		return scope, previous, exists
	}
	load := func() map[string]map[string]int {
		t.Helper()
		output, err := service.query("apt-config", "dump")
		if err != nil {
			t.Fatal(err)
		}
		return aptScopeLists(output)
	}

	first := updateScope{Managed: true, Security: true, Reminder: true}
	if err := service.saveScope(first, nil, false); err != nil {
		t.Fatal(err)
	}
	lists := load()
	if !samePatterns(lists[originsKey], []string{
		"origin=Debian,codename=${distro_codename},label=Debian-Security",
		"origin=Debian,codename=${distro_codename}-security,label=Debian-Security",
	}) || len(lists[allowedKey]) != 0 || !samePatterns(lists[blacklistKey], []string{"^custom-package$", kernelPattern, dockerPattern}) {
		t.Fatalf("unexpected security-only config: %v", lists)
	}
	if labels := effectiveScopeLabels(lists); labels["1"] != "开启" || labels["2"] != "关闭" || labels["3"] != "关闭" || labels["5"] != "关闭" {
		t.Fatalf("unexpected security-only labels: %v", labels)
	}
	current, previous, exists := read()
	if current != first || !exists {
		t.Fatalf("unexpected saved scope: %+v", current)
	}

	second := updateScope{Managed: true, Ordinary: true, Kernel: true, Docker: true, Reminder: true}
	write(mainConfig, `Unattended-Upgrade::Origins-Pattern:: "origin=Override\, Inc";`)
	if err := service.saveScope(second, previous, exists); err == nil {
		t.Fatal("overridden APT origins should fail verification")
	}
	current, previous, exists = read()
	if current != first || !exists {
		t.Fatalf("previous scope was not restored: %+v", current)
	}
	write(mainConfig, "")
	if err := service.saveScope(second, previous, exists); err != nil {
		t.Fatal(err)
	}
	lists = load()
	if !samePatterns(lists[originsKey], []string{
		"origin=Debian,codename=${distro_codename},label=Debian",
		"origin=Debian,codename=${distro_codename}-updates,label=Debian",
		"site=download.docker.com,codename=${distro_codename},component=stable",
	}) || !samePatterns(lists[blacklistKey], []string{"^custom-package$"}) {
		t.Fatalf("unexpected ordinary and Docker config: %v", lists)
	}

	_, previous, exists = read()
	if err := service.saveScope(updateScope{Reminder: true}, previous, exists); err != nil {
		t.Fatal(err)
	}
	lists = load()
	if !samePatterns(lists[originsKey], []string{"origin=Custom"}) || !samePatterns(lists[allowedKey], []string{"Debian:stable"}) {
		t.Fatalf("restoring the scope with the reminder enabled changed the native APT policy: %v", lists)
	}
	if labels := effectiveScopeLabels(lists); labels["1"] != "由自定义 APT 规则决定" || labels["5"] != "由自定义 APT 规则决定" {
		t.Fatalf("custom origins produced definitive labels: %v", labels)
	}
}

func TestParseStableDebianRelease(t *testing.T) {
	version, codename, err := parseStableRelease(strings.NewReader("Origin: Debian\nSuite: stable\nVersion: 13.8\nCodename: trixie\nSHA256:\n abc file\n"))
	if err != nil || version != 13 || codename != "trixie" {
		t.Fatalf("unexpected stable release: %d %q %v", version, codename, err)
	}
	if _, _, err := parseStableRelease(strings.NewReader("Origin: Debian\nSuite: testing\nVersion: 14\nCodename: forky\n")); err == nil {
		t.Fatal("testing must not be treated as a stable release")
	}
}
