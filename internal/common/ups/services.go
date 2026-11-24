package ups

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
)

type nutSystemdService struct {
	unit      string
	unitFiles []string
	optional  bool
}

func activateNUTServices() error {
	for _, service := range nutSystemdServices() {
		if !nutSystemdServiceExists(service) {
			if service.optional {
				log.Warn("未检测到 ", service.unit, ".service，跳过该服务")
				continue
			}
			return fmt.Errorf("未检测到 systemd 服务: %s.service", service.unit)
		}

		log.Info("设置 ", service.unit, " 开机启动...")
		if err := system.Run("systemctl", "enable", service.unit); err != nil {
			return fmt.Errorf("设置 %s 开机启动失败: %w", service.unit, err)
		}

		log.Info("启动/重启 ", service.unit, "...")
		if err := system.Run("systemctl", "restart", service.unit); err != nil {
			return fmt.Errorf("启动/重启 %s 失败: %w", service.unit, err)
		}
	}
	return nil
}

func nutSystemdServices() []nutSystemdService {
	return []nutSystemdService{
		{
			unit:      "nut-driver@" + upsMonitorName,
			unitFiles: []string{"nut-driver@" + upsMonitorName + ".service", "nut-driver@.service"},
			optional:  true,
		},
		{unit: "nut-server"},
		{unit: "nut-monitor"},
	}
}

func nutSystemdServiceExists(service nutSystemdService) bool {
	if len(service.unitFiles) == 0 {
		return system.SystemdUnitExists(service.unit + ".service")
	}
	for _, unitFile := range service.unitFiles {
		if system.SystemdUnitExists(unitFile) {
			return true
		}
	}
	return false
}

func verifyUPSCommunication() error {
	log.Info("验证 UPS 通信：upsc ups")
	out, err := system.Output("upsc", "ups")
	if strings.TrimSpace(out) != "" {
		fmt.Println(out)
	}
	if err != nil {
		return fmt.Errorf("upsc ups 验证失败，请检查 USB 连接、vendorid/productid 和 NUT 服务日志: %w", err)
	}
	return nil
}
