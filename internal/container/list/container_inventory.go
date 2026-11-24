package list

import (
	"encoding/json"
	"fmt"
	"strings"

	"snail_tool/internal/container/runtime"
	"snail_tool/internal/system"
)

type containerInfo struct {
	ID         string
	Name       string
	Ports      string
	Status     string
	State      string
	CreatedAt  string
	RunningFor string
	IsRunning  bool
}

func listContainers(rt runtime.Runtime) ([]containerInfo, error) {
	out, err := system.Output(rt.Name, "ps", "-a", "--format", "json")
	if err == nil {
		conts, parseErr := parseContainersJSON(out)
		if parseErr == nil {
			return conts, nil
		}
	}

	conts, fallbackErr := listContainersText(rt)
	if fallbackErr != nil {
		if err != nil {
			return nil, fmt.Errorf("获取容器列表失败: %w", err)
		}
		return nil, fmt.Errorf("获取容器列表失败: %w", fallbackErr)
	}
	return conts, nil
}

func listContainersText(rt runtime.Runtime) ([]containerInfo, error) {
	format := "{{.ID}}\t{{.Names}}\t{{.Ports}}\t{{.Status}}\t{{.CreatedAt}}\t{{.RunningFor}}"
	out, err := system.Output(rt.Name, "ps", "-a", "--format", format)
	if err != nil {
		return nil, err
	}
	return parseContainersText(out), nil
}

func parseContainersText(out string) []containerInfo {
	var conts []containerInfo

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}
		c := containerInfo{
			ID:         parts[0],
			Name:       parts[1],
			Ports:      parts[2],
			Status:     parts[3],
			CreatedAt:  parts[4],
			RunningFor: parts[5],
		}
		if len(parts) >= 7 {
			c.State = parts[6]
		}
		conts = append(conts, normalizeContainer(c))
	}
	return conts
}

func parseContainersJSON(out string) ([]containerInfo, error) {
	raw := strings.TrimSpace(out)
	if raw == "" {
		return nil, nil
	}

	if strings.HasPrefix(raw, "[") {
		var rows []map[string]any
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, err
		}
		return containersFromJSONObjects(rows), nil
	}

	var rows []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return containersFromJSONObjects(rows), nil
}

func containersFromJSONObjects(rows []map[string]any) []containerInfo {
	conts := make([]containerInfo, 0, len(rows))
	for _, row := range rows {
		c := containerInfo{
			ID:         jsonStringField(row, "ID", "Id", "id", "ContainerID"),
			Name:       jsonStringField(row, "Names", "Name", "names", "name"),
			Ports:      jsonStringField(row, "Ports", "ports"),
			Status:     jsonStringField(row, "Status", "status"),
			State:      jsonStringField(row, "State", "state"),
			CreatedAt:  jsonStringField(row, "CreatedAt", "Created", "createdAt", "created"),
			RunningFor: jsonStringField(row, "RunningFor", "runningFor"),
		}
		conts = append(conts, normalizeContainer(c))
	}
	return conts
}

func jsonStringField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return strings.TrimSpace(jsonValueString(value))
		}
	}
	return ""
}

func jsonValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(jsonValueString(item))
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if text := formatPortMap(v); text != "" {
			return text
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	default:
		return fmt.Sprint(v)
	}
}

func formatPortMap(port map[string]any) string {
	containerPort := jsonStringField(port, "container_port", "ContainerPort", "containerPort")
	hostPort := jsonStringField(port, "host_port", "HostPort", "hostPort")
	hostIP := jsonStringField(port, "host_ip", "HostIP", "hostIp")
	protocol := jsonStringField(port, "protocol", "Protocol")

	if containerPort == "" && hostPort == "" {
		return ""
	}
	if protocol == "" {
		protocol = "tcp"
	}
	target := containerPort
	if target != "" {
		target += "/" + protocol
	}
	if hostPort == "" {
		return target
	}
	if hostIP != "" {
		return fmt.Sprintf("%s:%s->%s", hostIP, hostPort, target)
	}
	return fmt.Sprintf("%s->%s", hostPort, target)
}

func normalizeContainer(c containerInfo) containerInfo {
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Ports = strings.TrimSpace(c.Ports)
	c.Status = strings.TrimSpace(c.Status)
	c.State = normalizeContainerState(c.State, c.Status)
	c.CreatedAt = strings.TrimSpace(c.CreatedAt)
	c.RunningFor = strings.TrimSpace(c.RunningFor)
	c.IsRunning = c.State == containerStateRunning
	return c
}

func normalizeContainerState(state, status string) string {
	lowerState := strings.ToLower(strings.TrimSpace(state))
	switch lowerState {
	case containerStateRunning, containerStateExited, containerStatePaused, containerStateCreated, containerStateRestarting:
		return lowerState
	case "dead", "removing":
		return lowerState
	}

	lowerStatus := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(lowerStatus, "paused"):
		return containerStatePaused
	case strings.Contains(lowerStatus, "restarting"):
		return containerStateRestarting
	case strings.HasPrefix(lowerStatus, "up") || strings.Contains(lowerStatus, "running"):
		return containerStateRunning
	case strings.HasPrefix(lowerStatus, "exited") || strings.HasPrefix(lowerStatus, "dead"):
		if strings.HasPrefix(lowerStatus, "dead") {
			return containerStateDead
		}
		return containerStateExited
	case strings.HasPrefix(lowerStatus, "created"):
		return containerStateCreated
	default:
		return containerStateUnknown
	}
}
