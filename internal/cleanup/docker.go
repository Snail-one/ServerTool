package cleanup

import (
	"fmt"

	"snail_tool/internal/container/clientproxy"
	"snail_tool/internal/container/daemonlog"
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

func cleanupDocker(account *system.Account) error {
	logPlan, err := daemonlog.PrepareRemoval(dockerDaemonPath, dockerManagedStore)
	if err != nil {
		return err
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
	if logPlan != nil {
		if err := logPlan.Apply(); err != nil {
			return err
		}
	}
	if proxyPlan != nil {
		if err := proxyPlan.Apply(); err != nil {
			return err
		}
	}
	if (logPlan != nil || proxyPlan != nil) && cleanupCommandExists("systemctl") {
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
