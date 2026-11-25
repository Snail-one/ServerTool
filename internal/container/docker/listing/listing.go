package listing

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	StateRunning    = "running"
	StateExited     = "exited"
	StatePaused     = "paused"
	StateCreated    = "created"
	StateRestarting = "restarting"
	StateDead       = "dead"
	StateUnknown    = "unknown"
)

func JsonStringField(row map[string]any, keys ...string) string {
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
	containerPort := JsonStringField(port, "container_port", "ContainerPort", "containerPort")
	hostPort := JsonStringField(port, "host_port", "HostPort", "hostPort")
	hostIP := JsonStringField(port, "host_ip", "HostIP", "hostIp")
	protocol := JsonStringField(port, "protocol", "Protocol")

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

func NormalizeContainerState(state, status string) string {
	lowerState := strings.ToLower(strings.TrimSpace(state))
	switch lowerState {
	case StateRunning, StateExited, StatePaused, StateCreated, StateRestarting:
		return lowerState
	case "dead", "removing":
		return lowerState
	}

	lowerStatus := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(lowerStatus, "paused"):
		return StatePaused
	case strings.Contains(lowerStatus, "restarting"):
		return StateRestarting
	case strings.HasPrefix(lowerStatus, "up") || strings.Contains(lowerStatus, "running"):
		return StateRunning
	case strings.HasPrefix(lowerStatus, "exited") || strings.HasPrefix(lowerStatus, "dead"):
		if strings.HasPrefix(lowerStatus, "dead") {
			return StateDead
		}
		return StateExited
	case strings.HasPrefix(lowerStatus, "created"):
		return StateCreated
	default:
		return StateUnknown
	}
}

func DefaultText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func TruncateText(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
