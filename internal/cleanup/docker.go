package cleanup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

var (
	dockerDaemonPath     = "/etc/docker/daemon.json"
	dockerProxyPath      = "/etc/systemd/system/docker.service.d/http-proxy.conf"
	cleanupUnitExists    = system.SystemdUnitExists
	cleanupCommandRun    = system.Run
	cleanupCommandExists = system.CommandExists
	dockerManagedStore   = shared.BackupStore{Root: shared.DataDir, Module: "docker"}
)

type fileCleanup struct {
	path   string
	data   []byte
	remove bool
}

func (action fileCleanup) apply() error {
	if action.remove {
		return shared.RemoveRegularFile(action.path)
	}
	return shared.AtomicWriteFile(action.path, action.data, shared.AtomicWriteOptions{Mode: 0644})
}

func cleanupDocker(_ *system.Account) error {
	var actions []fileCleanup
	content, exists, err := shared.ReadRegularFile(dockerDaemonPath)
	if err != nil {
		return err
	}
	if exists {
		var config map[string]any
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		if err := decoder.Decode(&config); err != nil || config == nil {
			return fmt.Errorf("Docker daemon.json 不是有效 JSON 对象，未清理")
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("Docker daemon.json 包含多余内容，未清理")
		}
		_, driver := config["log-driver"]
		_, options := config["log-opts"]
		if driver || options {
			if err := requireManagedDockerFile(dockerDaemonPath, content); err != nil {
				return err
			}
			delete(config, "log-driver")
			delete(config, "log-opts")
			data, err := json.MarshalIndent(config, "", "  ")
			if err != nil {
				return err
			}
			actions = append(actions, fileCleanup{path: dockerDaemonPath, data: append(data, '\n')})
		}
	}
	content, exists, err = shared.ReadRegularFile(dockerProxyPath)
	if err != nil {
		return err
	}
	proxyChanged := false
	if exists {
		var kept []string
		for _, line := range strings.SplitAfter(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			managed := false
			for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
				if strings.HasPrefix(trimmed, `Environment="`+name+`=`) {
					if !strings.HasSuffix(trimmed, `"`) || strings.Count(trimmed, `"`) != 2 {
						return fmt.Errorf("Docker 代理配置格式复杂，未清理，请手动检查: %s", dockerProxyPath)
					}
					managed = true
				}
			}
			if managed {
				proxyChanged = true
			} else {
				kept = append(kept, line)
			}
		}
		if proxyChanged {
			if err := requireManagedDockerFile(dockerProxyPath, content); err != nil {
				return err
			}
			cleaned := strings.Join(kept, "")
			meaningful := false
			for _, line := range strings.Split(cleaned, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && line != "[Service]" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";") {
					meaningful = true
				}
			}
			actions = append(actions, fileCleanup{path: dockerProxyPath, data: []byte(cleaned), remove: !meaningful})
		}
	}
	for _, action := range actions {
		if err := action.apply(); err != nil {
			return err
		}
		if err := dockerManagedStore.ForgetManaged(action.path); err != nil {
			return fmt.Errorf("Docker 配置已清理，但移除写入记录失败: %w", err)
		}
	}
	if len(actions) > 0 && cleanupCommandExists("systemctl") {
		if proxyChanged {
			if err := cleanupCommandRun("systemctl", "daemon-reload"); err != nil {
				return err
			}
		}
		if cleanupUnitExists("docker.service") {
			if err := cleanupCommandRun("systemctl", "try-restart", "docker.service"); err != nil {
				return fmt.Errorf("Docker 配置已清理，应用配置失败: %w", err)
			}
		}
	}
	return nil
}

func requireManagedDockerFile(path string, content []byte) error {
	matches, err := dockerManagedStore.MatchesManaged(path, content)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("Docker 配置没有本工具写入记录或已被修改，已保留，请手动检查: %s", path)
	}
	return nil
}
