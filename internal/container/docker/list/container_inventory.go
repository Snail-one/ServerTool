package list

import (
	"encoding/json"
	"fmt"
	"strings"

	"snail_tool/internal/container/docker/listing"
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

func listContainers() ([]containerInfo, error) {
	out, err := system.Output(runtimeName, "ps", "-a", "--format", "json")
	if err == nil {
		conts, parseErr := parseContainersJSON(out)
		if parseErr == nil {
			return conts, nil
		}
	}

	conts, fallbackErr := listContainersText()
	if fallbackErr != nil {
		if err != nil {
			return nil, fmt.Errorf("获取容器列表失败: %w", err)
		}
		return nil, fmt.Errorf("获取容器列表失败: %w", fallbackErr)
	}
	return conts, nil
}

func listContainersText() ([]containerInfo, error) {
	format := "{{.ID}}\t{{.Names}}\t{{.Ports}}\t{{.Status}}\t{{.CreatedAt}}\t{{.RunningFor}}"
	out, err := system.Output(runtimeName, "ps", "-a", "--format", format)
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
			ID:         listing.JsonStringField(row, "ID", "Id", "id", "ContainerID"),
			Name:       listing.JsonStringField(row, "Names", "Name", "names", "name"),
			Ports:      listing.JsonStringField(row, "Ports", "ports"),
			Status:     listing.JsonStringField(row, "Status", "status"),
			State:      listing.JsonStringField(row, "State", "state"),
			CreatedAt:  listing.JsonStringField(row, "CreatedAt", "Created", "createdAt", "created"),
			RunningFor: listing.JsonStringField(row, "RunningFor", "runningFor"),
		}
		conts = append(conts, normalizeContainer(c))
	}
	return conts
}

func normalizeContainer(c containerInfo) containerInfo {
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Ports = strings.TrimSpace(c.Ports)
	c.Status = strings.TrimSpace(c.Status)
	c.State = listing.NormalizeContainerState(c.State, c.Status)
	c.CreatedAt = strings.TrimSpace(c.CreatedAt)
	c.RunningFor = strings.TrimSpace(c.RunningFor)
	c.IsRunning = c.State == listing.StateRunning
	return c
}
