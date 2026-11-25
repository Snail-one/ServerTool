package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDuplicateDockerRepositoriesOnlyChecksEnabledSources(t *testing.T) {
	const aptURL = "https://download.docker.com/linux/debian"
	const rpmURL = "https://download.docker.com/linux/rhel/$releasever/$basearch/stable"
	for _, tt := range []struct {
		name, family, file, content string
		conflict                    bool
	}{
		{name: "active list", file: "extra.list", content: "deb " + aptURL + " bookworm stable", conflict: true},
		{name: "active list with options", file: "extra.list", content: "deb [arch=amd64 signed-by=/keys/docker.asc] " + aptURL + " bookworm stable", conflict: true},
		{name: "active source packages", file: "extra.list", content: "deb-src " + aptURL + " bookworm stable", conflict: true},
		{name: "backup list", file: "docker.list.save", content: "deb " + aptURL + " bookworm stable"},
		{name: "backup sources", file: "docker.sources.bak", content: "Types: deb\nURIs: " + aptURL},
		{name: "invalid apt filename", file: "docker copy.list", content: "deb " + aptURL + " bookworm stable"},
		{name: "commented list", file: "extra.list", content: "  # deb " + aptURL + " bookworm stable"},
		{name: "list inline comment", file: "extra.list", content: "deb https://example.com bookworm main # " + aptURL},
		{name: "list URL in options", file: "extra.list", content: "deb [signed-by=" + aptURL + "] https://example.com bookworm main"},
		{name: "lookalike host", file: "extra.list", content: "deb https://download.docker.com.evil/linux/debian bookworm stable"},
		{name: "different distribution", file: "extra.list", content: "deb " + aptURL + "-other bookworm stable"},
		{name: "active deb822", file: "extra.sources", content: "Types: deb\nURIs: " + aptURL + "\nSuites: bookworm\nComponents: stable", conflict: true},
		{name: "disabled deb822", file: "extra.sources", content: "Types: deb\nURIs: " + aptURL + "\nEnabled: no"},
		{name: "disabled deb822 before URI", file: "extra.sources", content: "Enabled: No\nTypes: deb\nURIs: " + aptURL},
		{name: "commented deb822", file: "extra.sources", content: "# Types: deb\n# URIs: " + aptURL},
		{name: "deb822 URL in description", file: "extra.sources", content: "Types: deb\nURIs: https://example.com\nDescription: " + aptURL},
		{name: "deb822 missing types", file: "extra.sources", content: "URIs: " + aptURL},
		{name: "deb822 multiline URLs", file: "extra.sources", content: "Types: deb\nURIs: https://example.com\n " + aptURL + "\nEnabled: yes", conflict: true},
		{name: "deb822 embedded key ignores URI text", file: "extra.sources", content: "Types: deb\nURIs: https://example.com\nSigned-By:\n URIs: " + aptURL},
		{name: "disabled and active stanza", file: "extra.sources", content: "Types: deb\nURIs: " + aptURL + "\nEnabled: no\n\nTypes: deb-src\nURIs: " + aptURL, conflict: true},
		{name: "disabled Docker and unrelated active stanza", file: "extra.sources", content: "Types: deb\nURIs: " + aptURL + "\nEnabled: no\n\nTypes: deb\nURIs: https://example.com"},
		{name: "RPM active", family: "rpm", file: "extra.repo", content: "[docker]\nbaseurl=" + rpmURL + "\nenabled=1", conflict: true},
		{name: "RPM defaults to active", family: "rpm", file: "extra.repo", content: "[docker]\nbaseurl=" + rpmURL, conflict: true},
		{name: "RPM disabled", family: "rpm", file: "extra.repo", content: "[docker]\nbaseurl=" + rpmURL + "\nenabled=0"},
		{name: "RPM disabled before URL", family: "rpm", file: "extra.repo", content: "[docker]\nenabled=False\nbaseurl=" + rpmURL},
		{name: "RPM backup", family: "rpm", file: "docker.repo.rpmsave", content: "[docker]\nbaseurl=" + rpmURL},
		{name: "RPM commented", family: "rpm", file: "extra.repo", content: "[docker]\n# baseurl=" + rpmURL + "\n; mirrorlist=" + rpmURL},
		{name: "RPM URL in name", family: "rpm", file: "extra.repo", content: "[unrelated]\nname=" + rpmURL + "\nbaseurl=https://example.com"},
		{name: "RPM multiline URLs", family: "rpm", file: "extra.repo", content: "[docker]\nbaseurl=https://example.com\n " + rpmURL, conflict: true},
		{name: "RPM disabled and active sections", family: "rpm", file: "extra.repo", content: "[disabled]\nbaseurl=" + rpmURL + "\nenabled=0\n[active]\nbaseurl=" + rpmURL, conflict: true},
		{name: "RPM disabled Docker and unrelated active section", family: "rpm", file: "extra.repo", content: "[disabled]\nbaseurl=" + rpmURL + "\nenabled=0\n[active]\nbaseurl=https://example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			dist := dockerDistribution{ID: "debian", Family: "apt"}
			managed := "docker.sources"
			if tt.family == "rpm" {
				dist.ID, dist.Family = "rhel", "rpm"
				managed = "docker-ce.repo"
			}
			installer := &dockerInstaller{readDir: os.ReadDir, readFile: os.ReadFile}
			err := installer.checkRepositoryDirectory(dir, managed, dist)
			if (err != nil) != tt.conflict {
				t.Fatalf("conflict=%v error=%v", tt.conflict, err)
			}
			if err != nil && !strings.Contains(err.Error(), tt.file) {
				t.Fatalf("error does not identify conflicting file: %v", err)
			}
		})
	}
}

func TestDuplicateDockerRepositoryChecksMainAPTList(t *testing.T) {
	for _, prefix := range []string{"", "# "} {
		installer := &dockerInstaller{readFile: func(string) ([]byte, error) {
			return []byte(prefix + "deb https://download.docker.com/linux/ubuntu noble stable"), nil
		}}
		err := installer.checkRepositoryFile("/etc/apt/sources.list", dockerDistribution{ID: "ubuntu", Family: "apt"})
		if (err != nil) != (prefix == "") {
			t.Fatalf("prefix=%q error=%v", prefix, err)
		}
	}
}

func TestDockerKeyFingerprintRejectsExtraPrimaryKeys(t *testing.T) {
	const other = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	for _, tt := range []struct {
		name, output string
		wantErr      bool
	}{
		{name: "one primary and subkey", output: "pub:::::::::\nfpr:::::::::" + dockerDebFingerprint + ":\nsub:::::::::\nfpr:::::::::" + other + ":\n"},
		{name: "additional primary", output: "pub:::::::::\nfpr:::::::::" + dockerDebFingerprint + ":\npub:::::::::\nfpr:::::::::" + other + ":\n", wantErr: true},
		{name: "additional primary without fingerprint", output: "pub:::::::::\nfpr:::::::::" + dockerDebFingerprint + ":\npub:::::::::\n", wantErr: true},
		{name: "subkey cannot substitute for primary", output: "pub:::::::::\nsub:::::::::\nfpr:::::::::" + dockerDebFingerprint + ":\n", wantErr: true},
		{name: "orphan fingerprint", output: "fpr:::::::::" + dockerDebFingerprint + ":\n", wantErr: true},
		{name: "private key", output: "sec:::::::::\nfpr:::::::::" + dockerDebFingerprint + ":\n", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installer := &dockerInstaller{output: func(string, ...string) (string, error) { return tt.output, nil }}
			got, err := installer.keyFingerprint("key.asc")
			if (err != nil) != tt.wantErr || err == nil && got != dockerDebFingerprint {
				t.Fatalf("fingerprint=%q error=%v wantErr=%v", got, err, tt.wantErr)
			}
		})
	}
}

func TestDockerInstallExtraPrimaryKeyStopsBeforeRepository(t *testing.T) {
	installer, calls := fakeDockerInstaller(t)
	baseOutput := dockerFakeOutput(dockerDebFingerprint)
	installer.output = func(name string, args ...string) (string, error) {
		output, err := baseOutput(name, args...)
		if name == "gpg" {
			output += "pub:::::::::\nfpr:::::::::AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA:\n"
		}
		return output, err
	}
	err := installer.install()
	if err == nil || !strings.Contains(err.Error(), "多个主密钥") {
		t.Fatalf("error=%v", err)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "write ") || strings.Contains(call, " install ") || strings.Contains(call, " remove ") || strings.HasPrefix(call, "systemctl ") {
			t.Fatalf("system changed after rejecting the key bundle: %v", *calls)
		}
	}
}
