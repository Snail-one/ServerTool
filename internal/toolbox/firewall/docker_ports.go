package firewall

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

type dockerPortSection struct {
	scope  string
	fields []ui.CardField
}

func dockerPortSections(containers []dockerPortContainer) ([]dockerPortSection, error) {
	fieldsByScope := make(map[string][]ui.CardField)
	for _, container := range containers {
		rows, err := dockerContainerPortRows(container)
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(container.Name, "/")
		for scope, values := range rows {
			fields := append(fieldsByScope[scope], ui.CardField{Value: ui.PrimaryBoldText(name)})
			for i, value := range values {
				branch := "├─ "
				if i == len(values)-1 {
					branch = "└─ "
				}
				fields = append(fields, ui.CardField{Value: branch + value})
			}
			fieldsByScope[scope] = fields
		}
	}
	var sections []dockerPortSection
	for _, scope := range []string{"全部地址", "回环地址", "指定地址", "地址未指定", "直接路由", "host 网络"} {
		if fields := fieldsByScope[scope]; len(fields) > 0 {
			sections = append(sections, dockerPortSection{scope: scope, fields: fields})
		}
	}
	return sections, nil
}

type dockerPortGroupKey struct {
	hostPort, target int
	protocol, scope  string
}

type dockerPortGroup struct {
	families  int
	addresses map[string]bool
}

func dockerContainerPortRows(container dockerPortContainer) (map[string][]string, error) {
	if container.NetworkMode == "host" {
		return map[string][]string{"host 网络": {"共用宿主机网络，需查看服务实际监听端口"}}, nil
	}
	name := strings.TrimPrefix(container.Name, "/")
	groups := make(map[dockerPortGroupKey]*dockerPortGroup)
	for port, bindings := range container.Ports {
		if len(bindings) == 0 {
			continue // EXPOSE without a published binding is not a host port.
		}
		target, protocol, _ := strings.Cut(port, "/")
		targetPort, err := system.ValidatePort(target)
		if err != nil || (protocol != "tcp" && protocol != "udp" && protocol != "sctp") {
			return nil, fmt.Errorf("容器 %s 的端口信息无效", name)
		}
		for _, binding := range bindings {
			hostPort := 0 // No host port in routed gateway mode.
			if binding.HostPort != "" {
				hostPort, err = system.ValidatePort(binding.HostPort)
				if err != nil {
					return nil, fmt.Errorf("容器 %s 的宿主机端口无效", name)
				}
			}
			scope, family := "地址未指定", 0
			address := binding.HostIP
			if address != "" {
				ip, err := netip.ParseAddr(address)
				if err != nil {
					return nil, fmt.Errorf("容器 %s 的绑定地址无效", name)
				}
				address = ip.String()
				family = 2
				if ip.Is4() {
					family = 1
				}
				switch {
				case ip.IsUnspecified():
					scope = "全部地址"
				case ip.IsLoopback():
					scope = "回环地址"
				default:
					scope = "指定地址"
				}
			}
			key := dockerPortGroupKey{hostPort: hostPort, target: targetPort, protocol: strings.ToUpper(protocol), scope: scope}
			group := groups[key]
			if group == nil {
				group = &dockerPortGroup{addresses: make(map[string]bool)}
				groups[key] = group
			}
			group.families |= family
			if address != "" {
				group.addresses[address] = true
			}
		}
	}
	keys := make([]dockerPortGroupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.hostPort != b.hostPort {
			if a.hostPort == 0 || b.hostPort == 0 {
				return b.hostPort == 0 // List routed entries after host mappings.
			}
			return a.hostPort < b.hostPort
		}
		if a.target != b.target {
			return a.target < b.target
		}
		if a.protocol != b.protocol {
			return a.protocol < b.protocol
		}
		return a.scope < b.scope
	})
	rows := make(map[string][]string)
	for _, key := range keys {
		group := groups[key]
		families := []string{"地址族未指定", "IPv4", "IPv6", "IPv4 / IPv6"}[group.families]
		scope := key.scope
		if key.scope == "全部地址" {
			scope += "（" + families + "）"
		} else if len(group.addresses) > 0 {
			addresses := make([]string, 0, len(group.addresses))
			for address := range group.addresses {
				addresses = append(addresses, address)
			}
			sort.Strings(addresses)
			scope += "（" + strings.Join(addresses, "、") + "）"
		}
		value := fmt.Sprintf("%d / %s", key.target, key.protocol)
		section := key.scope
		if key.hostPort == 0 {
			section = "直接路由"
			routeScope := families
			if key.scope != "全部地址" {
				routeScope = scope
			}
			value += " · 直接路由（" + routeScope + "，无宿主机映射端口）"
		} else {
			value = ui.PrimaryText(strconv.Itoa(key.hostPort)) + " → " + value + " · " + scope
		}
		rows[section] = append(rows[section], value)
	}
	return rows, nil
}

func showDockerPorts() {
	if !commandExists("docker") {
		return
	}
	containers, err := inspectDockerPorts()
	var sections []dockerPortSection
	if err == nil {
		sections, err = dockerPortSections(containers)
	}
	if err != nil {
		ui.PrintInfoCard("Docker 已发布端口", ui.CardField{Label: "无法读取", Value: err.Error()})
		return
	}
	metadata := []ui.CardField{
		{Label: "数据来源", Value: "当前 Docker 服务 · 运行中容器"},
		{Label: "映射格式", Value: "宿主机端口 → 容器端口 / 协议"},
	}
	if len(sections) == 0 {
		ui.PrintInfoCard("Docker 已发布端口", append(metadata, ui.CardField{Label: "端口映射", Value: "未发现运行中容器的已发布端口"})...)
		return
	}
	for i, section := range sections {
		fields := section.fields
		if i == 0 {
			fields = append(metadata, fields...)
		}
		if i == len(sections)-1 {
			fields = append(fields, ui.CardField{Label: "提示", Value: "发布映射不代表公网一定可达；仅 EXPOSE 的端口不显示"})
		}
		title := "Docker 已发布端口 · " + section.scope
		if section.scope == "host 网络" {
			title = "Docker host 网络容器"
		} else if section.scope == "直接路由" {
			title = "Docker 直接路由端口"
		}
		ui.PrintInfoCard(title, fields...)
	}
}
