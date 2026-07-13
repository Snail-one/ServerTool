package runtime

import (
	"fmt"
	"os"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const dockerInstallScriptURL = "https://get.docker.com"

type dockerScriptInstaller struct {
	tempDir       string
	download      func(string, string) error
	run           func(string, ...string) error
	output        func(string, ...string) (string, error)
	commandExists func(string) bool
	confirm       func() (bool, error)
}

func newDockerScriptInstaller(view *ui.UI) *dockerScriptInstaller {
	return &dockerScriptInstaller{
		download:      downloadDockerInstallScript,
		run:           system.Run,
		output:        system.Output,
		commandExists: system.CommandExists,
		confirm: func() (bool, error) {
			fmt.Println("即将下载并执行 Docker 官方安装脚本：")
			fmt.Println(dockerInstallScriptURL)
			fmt.Println()
			fmt.Println("注意：Docker 官方建议此便捷脚本主要用于测试和开发环境。")
			fmt.Println("脚本默认安装最新 stable 版本，可能带来未经验证的主版本升级。")
			return view.Confirm("确认继续使用官方脚本安装 Docker？(y/N): ")
		},
	}
}

func (installer *dockerScriptInstaller) install() error {
	if !installer.commandExists("sh") {
		return fmt.Errorf("Docker 脚本安装在环境检查阶段失败: 未找到 sh")
	}

	confirmed, err := installer.confirm()
	if err != nil {
		return fmt.Errorf("Docker 脚本安装在确认阶段失败: %w", err)
	}
	if !confirmed {
		log.Info("已取消 Docker 脚本安装，未修改系统")
		return nil
	}

	temp, err := os.CreateTemp(installer.tempDir, "servertool-get-docker-*.sh")
	if err != nil {
		return fmt.Errorf("Docker 脚本安装在临时文件创建阶段失败: %w", err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("Docker 脚本安装在临时文件创建阶段失败: %w", err)
	}
	defer os.Remove(tempPath)

	log.Info("[Docker 脚本安装/下载] 来源：", dockerInstallScriptURL)
	if err := installer.download(dockerInstallScriptURL, tempPath); err != nil {
		return fmt.Errorf("Docker 脚本安装在下载阶段失败: %w", err)
	}
	if err := validateDockerInstallScript(tempPath); err != nil {
		return fmt.Errorf("Docker 脚本安装在内容验证阶段失败: %w", err)
	}

	log.Info("[Docker 脚本安装/执行] sh ", tempPath)
	if err := installer.run("sh", tempPath); err != nil {
		return fmt.Errorf("Docker 官方安装脚本执行失败: %w", err)
	}

	if installer.commandExists("systemctl") {
		log.Info("[Docker 脚本安装/服务启动] systemctl enable --now docker")
		if err := installer.run("systemctl", "enable", "--now", "docker"); err != nil {
			return fmt.Errorf("Docker 脚本安装在服务启动阶段失败: %w", err)
		}
	}

	if err := verifyDockerInstallation(installer.output); err != nil {
		return fmt.Errorf("Docker 脚本安装在本地验证阶段失败: %w", err)
	}
	log.Info("Docker 官方脚本安装完成")
	return nil
}

func validateDockerInstallScript(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(data), "#!/bin/sh") {
		return fmt.Errorf("下载内容不是预期的 Docker Shell 安装脚本，已拒绝执行")
	}
	return nil
}
