package firewall

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// sshdCommandPath matches the SSH module so firewall generation and the
// security card read the same effective configuration.
const sshdCommandPath = "/usr/sbin/sshd"

// detectSSHPorts reads the effective sshd configuration. Port numbers come
// from every port directive and from listenaddress values that include a port.
func detectSSHPorts() ([]int, error) {
	out, err := commandOutput(sshdCommandPath, "-T")
	if err != nil {
		detail := strings.TrimSpace(out)
		if detail != "" {
			return nil, fmt.Errorf("读取 SSH 生效配置失败，未生成防火墙配置: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("读取 SSH 生效配置失败，未生成防火墙配置: %w", err)
	}
	ports, err := parseSSHDPorts(out)
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("sshd -T 未返回 SSH 端口，已停止生成")
	}
	return ports, nil
}

func parseSSHDPorts(output string) ([]int, error) {
	seen := make(map[int]bool)
	var ports []int
	add := func(raw string) error {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("SSH 配置端口无效 %q，已停止生成", raw)
		}
		if !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
		return nil
	}
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		switch strings.ToLower(fields[0]) {
		case "port":
			if err := add(value); err != nil {
				return nil, err
			}
		case "listenaddress":
			_, port, err := net.SplitHostPort(value)
			if err != nil {
				continue
			}
			if err := add(port); err != nil {
				return nil, err
			}
		}
	}
	sort.Ints(ports)
	return ports, nil
}

// detectListeningSSHPorts reports ports owned by a running SSH service.
// Firewall generation uses sshd -T; this is only for keeping the current
// session reachable while a new SSH configuration is reloaded.
func detectListeningSSHPorts() ([]int, error) {
	out, err := commandOutput("ss", "-H", "-ltnp")
	if err != nil {
		return nil, fmt.Errorf("读取 TCP 监听端口失败（需要 ss 和 root 权限）: %w: %s", err, strings.TrimSpace(out))
	}
	ports, systemdPorts := parseListeners(out)

	// ssh.socket may own the listener instead of sshd, including an override
	// of the port configured in sshd_config. The socket address alone is not
	// enough: another program, such as Docker, may be listening on that port.
	if commandExists("systemctl") {
		for _, unit := range []string{"ssh.socket", "sshd.socket"} {
			if socketOutput, err := commandOutput("systemctl", "show", unit, "--property=Listen", "--value"); err == nil {
				fields := strings.Fields(socketOutput)
				for i := 1; i < len(fields); i++ {
					if fields[i] == "(Stream)" {
						if port, ok := endpointPort(fields[i-1]); ok && systemdPorts[port] {
							ports[port] = true
						}
					}
				}
			}
		}
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("未检测到 SSH 实际监听端口")
	}
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result, nil
}

func parseListeners(output string) (map[int]bool, map[int]bool) {
	ssh := make(map[int]bool)
	systemd := make(map[int]bool)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "LISTEN" {
			continue
		}
		port, ok := endpointPort(fields[3])
		if !ok {
			continue
		}
		for _, name := range listenerProcesses(line) {
			switch name {
			case "sshd", "sshd-session":
				ssh[port] = true
			case "systemd":
				systemd[port] = true
			}
		}
	}
	return ssh, systemd
}

func listenerProcesses(line string) []string {
	var names []string
	rest := line
	for {
		start := strings.Index(rest, `(("`)
		if start < 0 {
			return names
		}
		rest = rest[start+3:]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return names
		}
		names = append(names, rest[:end])
		rest = rest[end+1:]
	}
}

func endpointPort(endpoint string) (int, bool) {
	i := strings.LastIndexByte(endpoint, ':')
	if i < 0 {
		return 0, false
	}
	port, err := strconv.Atoi(endpoint[i+1:])
	return port, err == nil && port >= 1 && port <= 65535
}
