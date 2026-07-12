package daemonlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"snail_tool/internal/container/update"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	dockerDaemonDir  = "/etc/docker"
	dockerDaemonPath = "/etc/docker/daemon.json"
	dockerLogDriver  = "json-file"
)

var maxSizePattern = regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]$`)
var maxFilePattern = regexp.MustCompile(`^[0-9]+$`)

type logRotationConfig struct {
	maxSize string
	maxFile string
}

type dockerLogStatus struct {
	hasLogDriver bool
	logDriver    any
	hasLogOpts   bool
	logOpts      any
}

type writeDockerDaemonLogConfigResult struct {
	written    bool
	canceled   bool
	overwrote  bool
	backupPath string
}

type promptReader interface {
	Ask(prompt string) (string, error)
	Confirm(prompt string) (bool, error)
}

type systemRunFunc func(string, ...string) error
type rebuildRunningComposeFunc func(update.ComposeRebuildConfirmer, string) error

func Run(view *ui.UI) error {
	return ConfigureDockerLogRotation(view)
}

func ConfigureDockerLogRotation(view *ui.UI) error {
	return configureDockerLogRotation(
		view,
		dockerDaemonPath,
		dockerDaemonDir,
		system.IsRoot,
		system.SystemdUnitExists,
		system.Run,
		update.RebuildRunningComposeProjectsWithPrompt,
	)
}

func configureDockerLogRotation(
	view promptReader,
	daemonPath string,
	daemonDir string,
	isRoot func() bool,
	systemdUnitExists func(string) bool,
	run systemRunFunc,
	rebuildRunningCompose rebuildRunningComposeFunc,
) error {
	if !isRoot() {
		return fmt.Errorf("配置 Docker 日志轮转需要 root 权限，请使用 sudo 运行本工具")
	}
	if !systemdUnitExists("docker.service") {
		return fmt.Errorf("未检测到 docker.service，无法配置 Docker 日志轮转")
	}

	fmt.Println("\033[32m[INFO]\033[0m 配置 Docker 日志轮转")
	fmt.Println()
	printDockerDaemonJSON(daemonPath)
	fmt.Println()

	rotation, skip, err := promptLogRotationConfig(view)
	if err != nil {
		return err
	}
	if skip {
		fmt.Println("已返回容器管理")
		return nil
	}

	result, err := writeDockerDaemonLogConfig(daemonPath, rotation, func(status dockerLogStatus) (bool, error) {
		fmt.Println()
		fmt.Println("检测到当前 daemon.json 已包含 Docker 日志配置：")
		printDockerLogStatus(status)
		fmt.Println()
		return view.Confirm("是否覆盖当前 Docker 日志配置？(y/N): ")
	})
	if err != nil {
		return err
	}
	if result.canceled {
		fmt.Println("已取消 Docker 日志轮转配置")
		return nil
	}

	log.Info("重启 Docker 服务...")
	if err := run("systemctl", "restart", "docker"); err != nil {
		return fmt.Errorf("重启 Docker 服务失败: %w", err)
	}

	fmt.Println()
	fmt.Println("Docker 日志轮转配置完成")
	fmt.Printf("配置文件：%s\n", daemonPath)
	fmt.Printf("写入目录：%s\n", daemonDir)
	if result.backupPath != "" {
		fmt.Printf("备份文件：%s\n", result.backupPath)
	}
	fmt.Printf("日志驱动：%s\n", dockerLogDriver)
	fmt.Printf("轮转配置：max-size=%s, max-file=%s\n", rotation.maxSize, rotation.maxFile)
	fmt.Println("已执行：systemctl restart docker")
	fmt.Println("说明：该配置只影响后续新建容器；已有容器通常需要重建后才会应用。")
	if rebuildRunningCompose != nil {
		if err := rebuildRunningCompose(view, "是否立即重建运行中的 Compose 项目使配置生效？(y/N): "); err != nil {
			return err
		}
	}
	return nil
}

func promptLogRotationConfig(view promptReader) (logRotationConfig, bool, error) {
	fmt.Println("请选择 Docker 日志轮转配置：")
	fmt.Println("1) 推荐：100m x 3")
	fmt.Println("2) 保守：50m x 5")
	fmt.Println("3) 节省空间：10m x 3")
	fmt.Println("4) 自定义 max-size / max-file")
	fmt.Println("0/q) 返回")
	fmt.Println()
	fmt.Println("说明：log-driver 固定为 json-file。")
	fmt.Println()

	choice, err := view.Ask("输入选项: ")
	if err != nil {
		return logRotationConfig{}, false, err
	}

	if shared.IsReturnChoice(choice) {
		return logRotationConfig{}, true, nil
	}
	if rotation, ok := logRotationPreset(strings.TrimSpace(choice)); ok {
		return rotation, false, nil
	}
	if strings.TrimSpace(choice) != "4" {
		return logRotationConfig{}, false, fmt.Errorf("无效 Docker 日志轮转选项: %s", choice)
	}

	maxSize, err := view.Ask("请输入 max-size（正整数 + k/m/g，例如 100m）: ")
	if err != nil {
		return logRotationConfig{}, false, err
	}
	maxSize, err = normalizeMaxSize(maxSize)
	if err != nil {
		return logRotationConfig{}, false, err
	}

	maxFile, err := view.Ask("请输入 max-file（正整数，建议 1-99）: ")
	if err != nil {
		return logRotationConfig{}, false, err
	}
	maxFile, err = normalizeMaxFile(maxFile)
	if err != nil {
		return logRotationConfig{}, false, err
	}

	return logRotationConfig{maxSize: maxSize, maxFile: maxFile}, false, nil
}

func logRotationPreset(choice string) (logRotationConfig, bool) {
	switch choice {
	case "1":
		return defaultLogRotationConfig(), true
	case "2":
		return logRotationConfig{maxSize: "50m", maxFile: "5"}, true
	case "3":
		return logRotationConfig{maxSize: "10m", maxFile: "3"}, true
	default:
		return logRotationConfig{}, false
	}
}

func defaultLogRotationConfig() logRotationConfig {
	return logRotationConfig{maxSize: "100m", maxFile: "3"}
}

func normalizeMaxSize(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !maxSizePattern.MatchString(value) {
		return "", fmt.Errorf("max-size 格式无效：%s（示例：100m，单位支持 k/m/g）", raw)
	}
	return strings.ToLower(value), nil
}

func normalizeMaxFile(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !maxFilePattern.MatchString(value) {
		return "", fmt.Errorf("max-file 必须是正整数：%s", raw)
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return "", fmt.Errorf("max-file 必须是正整数：%s", raw)
	}
	return strconv.Itoa(n), nil
}

func writeDockerDaemonLogConfig(path string, rotation logRotationConfig, confirmOverwrite func(dockerLogStatus) (bool, error)) (writeDockerDaemonLogConfigResult, error) {
	config, exists, err := readDockerDaemonJSON(path)
	if err != nil {
		return writeDockerDaemonLogConfigResult{}, err
	}

	status, hasLogConfig := dockerLogConfigStatus(config)
	result := writeDockerDaemonLogConfigResult{overwrote: hasLogConfig}
	if hasLogConfig {
		if confirmOverwrite == nil {
			return result, errors.New("检测到已有 Docker 日志配置，但未提供覆盖确认")
		}
		confirmed, err := confirmOverwrite(status)
		if err != nil {
			return result, err
		}
		if !confirmed {
			result.canceled = true
			return result, nil
		}
	}

	applyDockerLogRotation(config, rotation)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return result, err
	}
	if exists {
		backupPath, err := system.Backup(path)
		if err != nil {
			return result, fmt.Errorf("备份 %s 失败: %w", path, err)
		}
		result.backupPath = backupPath
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return result, err
	}
	data = append(data, '\n')
	if err := shared.AtomicWriteFile(path, data, shared.AtomicWriteOptions{Mode: 0644, ForceMode: true}); err != nil {
		return result, err
	}

	result.written = true
	return result, nil
}

func readDockerDaemonJSON(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, err
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, true, fmt.Errorf("无法解析 %s：%w，已取消写入，请手动检查", path, err)
	}
	if config == nil {
		return nil, true, fmt.Errorf("%s 必须是 JSON 对象，已取消写入，请手动检查", path)
	}
	return config, true, nil
}

func applyDockerLogRotation(config map[string]any, rotation logRotationConfig) {
	config["log-driver"] = dockerLogDriver
	config["log-opts"] = map[string]any{
		"max-size": rotation.maxSize,
		"max-file": rotation.maxFile,
	}
}

func dockerLogConfigStatus(config map[string]any) (dockerLogStatus, bool) {
	status := dockerLogStatus{}
	status.logDriver, status.hasLogDriver = config["log-driver"]
	status.logOpts, status.hasLogOpts = config["log-opts"]
	return status, status.hasLogDriver || status.hasLogOpts
}

func printDockerDaemonJSON(path string) {
	fmt.Printf("当前配置文件：%s\n", path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("当前配置内容：文件不存在，将创建新文件")
			return
		}
		fmt.Printf("当前配置内容：读取失败：%v\n", err)
		return
	}
	if strings.TrimSpace(string(data)) == "" {
		fmt.Println("当前配置内容：（空文件）")
		return
	}
	fmt.Println("当前配置内容：")
	fmt.Print(string(data))
	if !strings.HasSuffix(string(data), "\n") {
		fmt.Println()
	}
}

func printDockerLogStatus(status dockerLogStatus) {
	if status.hasLogDriver {
		fmt.Printf("log-driver: %s\n", jsonValueString(status.logDriver))
	} else {
		fmt.Println("log-driver: 未设置")
	}
	if status.hasLogOpts {
		fmt.Printf("log-opts: %s\n", jsonValueString(status.logOpts))
	} else {
		fmt.Println("log-opts: 未设置")
	}
}

func jsonValueString(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
