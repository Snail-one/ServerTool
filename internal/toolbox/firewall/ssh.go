package firewall

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// detectSSHPorts uses live TCP listeners rather than assuming the on-disk
// sshd configuration has already been loaded by the service.
func detectSSHPorts() ([]int, error) {
	out, err := commandOutput("ss", "-H", "-ltnp")
	if err != nil {
		return nil, fmt.Errorf("读取 TCP 监听端口失败，未生成防火墙配置（需要 ss 和 root 权限）: %w: %s", err, strings.TrimSpace(out))
	}
	listening, ports := parseListeners(out)

	// ssh.socket may own the listener instead of sshd, including an override
	// of the port configured in sshd_config.
	if commandExists("systemctl") {
		for _, unit := range []string{"ssh.socket", "sshd.socket"} {
			if socketOutput, err := commandOutput("systemctl", "show", unit, "--property=Listen", "--value"); err == nil {
				fields := strings.Fields(socketOutput)
				for i := 1; i < len(fields); i++ {
					if fields[i] == "(Stream)" {
						if port, ok := endpointPort(fields[i-1]); ok && listening[port] {
							ports[port] = true
						}
					}
				}
			}
		}
	}

	// The current SSH connection is an additional source, useful with socket
	// activation or a nonstandard sshd invocation. Never use the client port.
	if fields := strings.Fields(os.Getenv("SSH_CONNECTION")); len(fields) == 4 {
		if port, err := strconv.Atoi(fields[3]); err == nil && listening[port] {
			ports[port] = true
		}
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("未检测到 SSH 实际监听端口，已停止生成；请确认 SSH 服务已启动，并使用 sudo 运行本工具")
	}
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result, nil
}

func parseListeners(output string) (map[int]bool, map[int]bool) {
	listening := make(map[int]bool)
	ssh := make(map[int]bool)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "LISTEN" {
			continue
		}
		port, ok := endpointPort(fields[3])
		if !ok {
			continue
		}
		listening[port] = true
		if strings.Contains(line, `"sshd"`) || strings.Contains(line, `"sshd-session"`) {
			ssh[port] = true
		}
	}
	return listening, ssh
}

func endpointPort(endpoint string) (int, bool) {
	i := strings.LastIndexByte(endpoint, ':')
	if i < 0 {
		return 0, false
	}
	port, err := strconv.Atoi(endpoint[i+1:])
	return port, err == nil && port >= 1 && port <= 65535
}
