package firewall

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

// Only request networking metadata; inspect's full output also contains container
// environment variables and other unrelated configuration.
const dockerPortsFormat = `{"name":{{json .Name}},"running":{{json .State.Running}},"networkMode":{{json .HostConfig.NetworkMode}},"ports":{{json .NetworkSettings.Ports}}}`

type dockerPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string
}

type dockerPortContainer struct {
	Name        string
	Running     bool
	NetworkMode string
	Ports       map[string][]dockerPortBinding
}

func inspectDockerPorts() ([]dockerPortContainer, error) {
	output, err := commandOutput("docker", "ps", "--quiet", "--no-trunc")
	if err != nil {
		return nil, fmt.Errorf("读取运行中容器失败: %w: %s", err, strings.TrimSpace(output))
	}
	ids := strings.Fields(output)
	for _, id := range ids {
		if _, err := hex.DecodeString(id); err != nil || len(id) != 64 {
			return nil, fmt.Errorf("Docker 返回的容器 ID 无效")
		}
	}
	var containers []dockerPortContainer
	// Bound argument size on hosts with many containers.
	for start := 0; start < len(ids); start += 100 {
		end := start + 100
		if end > len(ids) {
			end = len(ids)
		}
		args := append([]string{"inspect", "--type", "container", "--format", dockerPortsFormat}, ids[start:end]...)
		output, err := commandOutput("docker", args...)
		if err != nil {
			return nil, fmt.Errorf("读取容器端口映射失败: %w: %s", err, strings.TrimSpace(output))
		}
		decoder := json.NewDecoder(strings.NewReader(output))
		count := 0
		for {
			var container dockerPortContainer
			if err := decoder.Decode(&container); err == io.EOF {
				break
			} else if err != nil {
				return nil, fmt.Errorf("解析容器端口映射失败: %w", err)
			}
			count++
			if container.Running {
				containers = append(containers, container)
			}
		}
		if count != end-start {
			return nil, fmt.Errorf("Docker 返回的容器端口信息不完整")
		}
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return containers, nil
}

func dockerPortFields(containers []dockerPortContainer) ([]ui.CardField, error) {
	var fields []ui.CardField
	for _, container := range containers {
		name := strings.TrimPrefix(container.Name, "/")
		if container.NetworkMode == "host" {
			fields = append(fields, ui.CardField{Label: name, Value: "host 网络：共用宿主机网络，需查看服务实际监听端口"})
			continue
		}
		keys := make([]string, 0, len(container.Ports))
		for port, bindings := range container.Ports {
			if len(bindings) == 0 {
				continue // EXPOSE without a published binding is not a host port.
			}
			parts := strings.Split(port, "/")
			if len(parts) != 2 {
				return nil, fmt.Errorf("容器 %s 的端口信息无效", name)
			}
			if _, err := system.ValidatePort(parts[0]); err != nil || (parts[1] != "tcp" && parts[1] != "udp" && parts[1] != "sctp") {
				return nil, fmt.Errorf("容器 %s 的端口信息无效", name)
			}
			keys = append(keys, port)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, _, _ := strings.Cut(keys[i], "/")
			b, _, _ := strings.Cut(keys[j], "/")
			ai, _ := strconv.Atoi(a)
			bi, _ := strconv.Atoi(b)
			if ai != bi {
				return ai < bi
			}
			return keys[i] < keys[j]
		})
		for _, port := range keys {
			bindings := container.Ports[port]
			sort.Slice(bindings, func(i, j int) bool {
				if bindings[i].HostIP != bindings[j].HostIP {
					return bindings[i].HostIP < bindings[j].HostIP
				}
				return bindings[i].HostPort < bindings[j].HostPort
			})
			target, protocol, _ := strings.Cut(port, "/")
			for _, binding := range bindings {
				address := binding.HostIP
				scope := "绑定地址未指定"
				if address != "" {
					ip, err := netip.ParseAddr(address)
					if err != nil {
						return nil, fmt.Errorf("容器 %s 的绑定地址无效", name)
					}
					scope = "IPv6"
					if ip.Is4() {
						scope = "IPv4"
					}
					if ip.IsUnspecified() {
						scope += " 全部地址"
					} else if ip.IsLoopback() {
						scope += " 回环地址"
					}
				}
				value := "直接路由（无宿主机映射端口）"
				if binding.HostPort != "" {
					if _, err := system.ValidatePort(binding.HostPort); err != nil {
						return nil, fmt.Errorf("容器 %s 的宿主机端口无效", name)
					}
					value = net.JoinHostPort(address, binding.HostPort)
				}
				fields = append(fields, ui.CardField{Label: name, Value: fmt.Sprintf("%s → %s / %s · %s", value, target, strings.ToUpper(protocol), scope)})
			}
		}
	}
	return fields, nil
}

func showDockerPorts() {
	if !commandExists("docker") {
		return
	}
	containers, err := inspectDockerPorts()
	var fields []ui.CardField
	if err == nil {
		fields, err = dockerPortFields(containers)
	}
	if err != nil {
		ui.PrintInfoCard("Docker 已发布端口", ui.CardField{Label: "无法读取", Value: err.Error()})
		return
	}
	fields = append([]ui.CardField{{Label: "数据来源", Value: "Docker 当前连接的 daemon；运行中容器的端口映射"}}, fields...)
	if len(fields) == 1 {
		fields = append(fields, ui.CardField{Label: "端口映射", Value: "未发现运行中容器的已发布端口"})
	}
	fields = append(fields, ui.CardField{Label: "说明", Value: "宿主机绑定地址:端口 → 容器端口 / 协议；不包含仅 EXPOSE 的端口", Detail: "Docker 映射通常经过转发规则，与上方 input 放行范围分别显示；映射不代表外网一定可达"})
	ui.PrintInfoCard("Docker 已发布端口", fields...)
}
