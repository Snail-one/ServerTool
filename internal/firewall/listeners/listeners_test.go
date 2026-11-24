package listeners

import (
	"os"
	"strings"
	"testing"
)

func TestFormatListenersShowsSocketAndProcessTitle(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	text := strings.Join(formatListeners(sampleListeners()), "\n")
	if strings.Contains(text, "\033") {
		t.Fatalf("NO_COLOR output contains color codes:\n%s", text)
	}
	for _, want := range []string{
		"udp   UNCONN  0     0     192.168.59.128:68",
		"0.0.0.0:*",
		`users:(("dhcpcd",pid=1060,fd=3))`,
		"    └─ dhcpcd: [BOOTP proxy] 192.168.59.128",
		"[fe80::cd0f:cb16:26e0:87b2]%ens37:546",
		"[::]:*",
		"    └─ dhcpcd: [DHCP6 proxy] fe80::cd0f:cb16:26e0:87b2",
		`users:(("sshd",pid=900,fd=3),("sshd",pid=901,fd=4))`,
		"    └─ sshd: /usr/sbin/sshd -D",
		`users:(("nginx",pid=1,fd=6),("nginx",pid=2,fd=6))`,
		"    └─ nginx: master process nginx",
		"    └─ nginx: worker process",
		"    └─ sshd: /usr/sbin/sshd -D\n\nudp   UNCONN",
		"    └─ dhcpcd: [BOOTP proxy] 192.168.59.128\n\ntcp   LISTEN",
		"    └─ nginx: worker process\n\nudp   UNCONN",
		"    └─ nginx: master process nginx\n    └─ nginx: worker process",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.HasPrefix(text, "\n") || strings.HasSuffix(text, "\n") || strings.Contains(text, "\n\n\n") {
		t.Fatalf("process gap is not a single blank line:\n%s", text)
	}
}

func TestFormatListenersColorsSocketFields(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")

	const (
		reset     = "\033[0m"
		green     = "\033[32m"
		yellow    = "\033[33m"
		cyan      = "\033[36m"
		purple    = "\033[35m"
		lightBlue = "\033[94m"
	)
	text := strings.Join(formatListeners(sampleListeners()), "\n")
	for _, want := range []string{
		cyan + "udp" + reset + "   " + green + "UNCONN" + reset,
		cyan + "tcp" + reset + "   " + green + "LISTEN" + reset,
		"192.168.59.128:" + lightBlue + "68" + reset,
		"[fe80::cd0f:cb16:26e0:87b2]%ens37:" + lightBlue + "546" + reset,
		"0.0.0.0:*",
		"[::]:*",
		`users:(("dhcpcd",pid=` + yellow + "1060" + reset + `,fd=3))`,
		`users:(("sshd",pid=` + yellow + "900" + reset + `,fd=3),("sshd",pid=` + yellow + "901" + reset + `,fd=4))`,
		`users:(("nginx",pid=` + yellow + "1" + reset + `,fd=6),("nginx",pid=` + yellow + "2" + reset + `,fd=6))`,
		"    └─ " + purple + "dhcpcd: [BOOTP proxy] 192.168.59.128" + reset,
		"    └─ " + purple + "nginx: master process nginx" + reset,
		"    └─ " + purple + "nginx: worker process" + reset,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	for _, address := range []string{"192.168.59.128:", "[fe80::cd0f:cb16:26e0:87b2]", "0.0.0.0:", "[::]:"} {
		if strings.Contains(text, cyan+address) || strings.Contains(text, lightBlue+address) || strings.Contains(text, purple+address) {
			t.Errorf("address %q was colored in\n%s", address, text)
		}
	}
	if strings.Contains(text, lightBlue+"*") {
		t.Errorf("wildcard port was colored in\n%s", text)
	}
}

func sampleListeners() []Listener {
	output := strings.Join([]string{
		`udp UNCONN 0 0 192.168.59.128:68 0.0.0.0:* users:(("dhcpcd",pid=1060,fd=3))`,
		`udp UNCONN 0 0 [fe80::cd0f:cb16:26e0:87b2]%ens37:546 [::]:* users:(("dhcpcd",pid=972,fd=3))`,
		`tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=900,fd=3),("sshd",pid=901,fd=4))`,
		`tcp LISTEN 0 511 127.0.0.1:80 0.0.0.0:* users:(("nginx",pid=1,fd=6),("nginx",pid=2,fd=6))`,
	}, "\n")
	return Parse(output, func(pid string) string {
		return map[string]string{
			"1060": "dhcpcd: [BOOTP proxy] 192.168.59.128",
			"972":  "dhcpcd: [DHCP6 proxy] fe80::cd0f:cb16:26e0:87b2",
			"900":  "sshd: /usr/sbin/sshd -D",
			"901":  "sshd: /usr/sbin/sshd -D",
			"1":    "nginx: master process nginx",
			"2":    "nginx: worker process",
		}[pid]
	})
}
