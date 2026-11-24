package compose

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"snail_tool/internal/container/compose/update"
	"snail_tool/internal/container/internal/listing"
	"snail_tool/internal/system"
)

type composeProjectInfo struct {
	Name        string
	Dir         string
	Status      string
	StatusError string
	Containers  composeContainerSummary
}

type composeContainerSummary struct {
	HasData    bool
	Total      int
	Running    int
	Exited     int
	Paused     int
	Restarting int
	Created    int
	Unknown    int
}

func FindProjectDir(compose update.ComposeCommand, project string) (string, error) {
	if project == "" {
		return "", nil
	}
	projs, err := update.GetAllComposeProjectDirsFromLS(compose)
	if err != nil || len(projs) == 0 {
		// fallback: try to use ls json for name->dir map
	}
	// re-fetch map
	output, err := system.Output(compose.Name, append(compose.Args, "ls", "--format", "json")...)
	if err != nil {
		return "", err
	}
	var projects []struct {
		Name        string `json:"Name"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal([]byte(output), &projects); err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.Name == project && p.ConfigFiles != "" {
			for _, f := range strings.Split(p.ConfigFiles, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					return filepath.Dir(f), nil
				}
			}
		}
	}
	return "", nil
}

func listComposeProjectsFromLS(compose update.ComposeCommand) ([]composeProjectInfo, error) {
	output, err := composeOutputGlobal(compose, "ls", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("执行 compose ls 失败: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" || output == "[]" || output == "null" {
		return nil, nil
	}

	var rows []struct {
		Name        string `json:"Name"`
		Status      string `json:"Status"`
		ConfigFiles string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		return nil, fmt.Errorf("解析 compose ls 输出失败: %w (原始: %s)", err, output)
	}

	byDir := make(map[string]composeProjectInfo)
	for _, row := range rows {
		for _, file := range strings.Split(row.ConfigFiles, ",") {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			dir := filepath.Clean(filepath.Dir(file))
			if dir == "." || dir == "" {
				continue
			}
			byDir[dir] = composeProjectInfo{
				Name:   strings.TrimSpace(row.Name),
				Dir:    dir,
				Status: strings.TrimSpace(row.Status),
			}
		}
	}

	return enrichComposeProjects(compose, sortComposeProjects(mapComposeProjects(byDir))), nil
}

func listComposeProjectsFromDirs(compose update.ComposeCommand, dirs []string) []composeProjectInfo {
	projects := make([]composeProjectInfo, 0, len(dirs))
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" {
			continue
		}
		projects = append(projects, composeProjectInfo{
			Name: filepath.Base(dir),
			Dir:  dir,
		})
	}
	return enrichComposeProjects(compose, sortComposeProjects(projects))
}

func mapComposeProjects(projects map[string]composeProjectInfo) []composeProjectInfo {
	result := make([]composeProjectInfo, 0, len(projects))
	for _, project := range projects {
		result = append(result, project)
	}
	return result
}

func sortComposeProjects(projects []composeProjectInfo) []composeProjectInfo {
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Name != projects[j].Name {
			return projects[i].Name < projects[j].Name
		}
		return projects[i].Dir < projects[j].Dir
	})
	return projects
}

func enrichComposeProjects(compose update.ComposeCommand, projects []composeProjectInfo) []composeProjectInfo {
	for i := range projects {
		summary, projectName, err := composeProjectContainerSummary(compose, projects[i].Dir)
		if err != nil {
			projects[i].StatusError = err.Error()
			continue
		}
		projects[i].Containers = summary
		if projects[i].Name == "" && projectName != "" {
			projects[i].Name = projectName
		}
	}
	return projects
}

func composeProjectContainerSummary(compose update.ComposeCommand, dir string) (composeContainerSummary, string, error) {
	output, err := composeOutputInDir(compose, dir, "ps", "--format", "json")
	if err == nil {
		summary, projectName, parseErr := parseComposePSJSON(output)
		if parseErr == nil {
			return summary, projectName, nil
		}
	}

	output, fallbackErr := composeOutputInDir(compose, dir, "ps")
	if fallbackErr != nil {
		if err != nil {
			return composeContainerSummary{}, "", fmt.Errorf("compose ps 失败: %w", err)
		}
		return composeContainerSummary{}, "", fmt.Errorf("compose ps 失败: %w", fallbackErr)
	}
	return parseComposePSText(output), "", nil
}

func parseComposePSJSON(output string) (composeContainerSummary, string, error) {
	raw := strings.TrimSpace(output)
	if raw == "" || raw == "[]" || raw == "null" {
		return composeContainerSummary{HasData: true}, "", nil
	}

	var rows []map[string]any
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return composeContainerSummary{}, "", err
		}
	} else {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return composeContainerSummary{}, "", err
			}
			rows = append(rows, row)
		}
	}

	summary := composeContainerSummary{HasData: true}
	projectName := ""
	for _, row := range rows {
		state := listing.NormalizeContainerState(
			listing.JsonStringField(row, "State", "state"),
			listing.JsonStringField(row, "Status", "status"),
		)
		summary.addState(state)
		if projectName == "" {
			projectName = listing.JsonStringField(row, "Project", "project")
		}
	}
	return summary, projectName, nil
}

func parseComposePSText(output string) composeContainerSummary {
	summary := composeContainerSummary{HasData: true}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "name ") || strings.Contains(strings.ToLower(line), "no containers") {
			continue
		}
		summary.addState(normalizeComposePSTextState(line))
	}
	return summary
}

func normalizeComposePSTextState(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "paused"):
		return listing.StatePaused
	case strings.Contains(lower, "restarting"):
		return listing.StateRestarting
	case strings.Contains(lower, "exited"):
		return listing.StateExited
	case strings.Contains(lower, "created"):
		return listing.StateCreated
	case strings.Contains(lower, "dead"):
		return "dead"
	case strings.Contains(lower, "running") || strings.Contains(lower, " up "):
		return listing.StateRunning
	default:
		return listing.StateUnknown
	}
}

func (s *composeContainerSummary) addState(state string) {
	s.HasData = true
	s.Total++
	switch state {
	case listing.StateRunning:
		s.Running++
	case listing.StateExited, "dead":
		s.Exited++
	case listing.StatePaused:
		s.Paused++
	case listing.StateRestarting:
		s.Restarting++
	case listing.StateCreated:
		s.Created++
	default:
		s.Unknown++
	}
}

func composeOutputGlobal(compose update.ComposeCommand, args ...string) (string, error) {
	out, err := exec.Command(compose.Name, composeArgs(compose, args...)...).CombinedOutput()
	return string(out), err
}

func composeOutputInDir(compose update.ComposeCommand, dir string, args ...string) (string, error) {
	cmd := exec.Command(compose.Name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func composeArgs(compose update.ComposeCommand, args ...string) []string {
	allArgs := append([]string{}, compose.Args...)
	return append(allArgs, args...)
}
