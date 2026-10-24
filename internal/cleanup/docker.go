package cleanup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"snail_tool/internal/container/clientproxy"
	"snail_tool/internal/container/daemonproxy"
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
	cleanupClientProxy   = clientproxy.Cleanup
)

type fileCleanup struct {
	path string
	data []byte
}

func (action fileCleanup) apply() error {
	return shared.AtomicWriteFile(action.path, action.data, shared.AtomicWriteOptions{Mode: 0644})
}

func cleanupDocker(account *system.Account) error {
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
	proxyPlan, err := daemonproxy.PrepareRemoval(dockerProxyPath, dockerManagedStore)
	if err != nil {
		return err
	}
	// Preflight service files before changing the user config. Client proxy
	// cleanup itself never requires restarting Docker.
	if err := cleanupClientProxy(account); err != nil {
		return err
	}
	for _, action := range actions {
		if err := action.apply(); err != nil {
			return err
		}
		if err := dockerManagedStore.ForgetManaged(action.path); err != nil {
			return fmt.Errorf("Docker 配置已清理，但移除写入记录失败: %w", err)
		}
	}
	if proxyPlan != nil {
		if err := proxyPlan.Apply(); err != nil {
			return err
		}
	}
	if (len(actions) > 0 || proxyPlan != nil) && cleanupCommandExists("systemctl") {
		if proxyPlan != nil {
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
