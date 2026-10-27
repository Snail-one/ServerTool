package daemonlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"snail_tool/internal/container/update"
	"snail_tool/internal/input"
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
var backupStore = shared.BackupStore{Root: shared.DataDir, Module: "docker"}

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
	ui.ClearScreen()
	ui.MenuTitle("容器管理", "Docker 配置", "配置日志轮转")
	if !isRoot() {
		return fmt.Errorf("配置 Docker 日志轮转需要 root 权限，请使用 sudo 运行本工具")
	}
	if !systemdUnitExists("docker.service") {
		return fmt.Errorf("未检测到 docker.service，无法配置 Docker 日志轮转")
	}

	config, _, err := readDockerDaemonJSON(daemonPath)
	if err != nil {
		return err
	}
	status, _ := dockerLogConfigStatus(config)
	fields := []ui.CardField{{Label: "配置文件", Value: daemonPath}}
	fields = append(fields, logStatusFields(status)...)
	fields = append(fields,
		ui.CardField{Label: "应用方式", Value: "保存后重启 Docker；对新建容器生效，已有容器需重建"},
	)
	ui.PrintSectionCard("当前配置", fields...)
	fmt.Println()

	rotation, skip, err := promptLogRotationConfig(view)
	if err != nil {
		return err
	}
	if skip {
		return shared.ErrReturnToMenu
	}

	result, err := writeDockerDaemonLogConfig(daemonPath, rotation, func(dockerLogStatus) (bool, error) {
		fmt.Println()
		return view.Confirm("是否覆盖当前 Docker 日志配置？(y/N)：")
	})
	if err != nil {
		return err
	}
	if result.canceled {
		fmt.Println("已取消 Docker 日志轮转配置")
		return shared.ErrReturnToMenu
	}

	log.Info("重启 Docker 服务...")
	if err := run("systemctl", "restart", "docker"); err != nil {
		return fmt.Errorf("重启 Docker 服务失败: %w", err)
	}

	fmt.Println()
	fields = []ui.CardField{
		{Label: "配置文件", Value: daemonPath},
		{Label: "写入目录", Value: daemonDir},
	}
	if result.backupPath != "" {
		fields = append(fields, ui.CardField{Label: "备份文件", Value: result.backupPath})
	}
	fields = append(fields,
		ui.CardField{Label: "日志驱动", Value: dockerLogDriver},
		ui.CardField{Label: "轮转配置", Value: fmt.Sprintf("max-size=%s, max-file=%s", rotation.maxSize, rotation.maxFile)},
		ui.CardField{Label: "已执行", Value: "systemctl restart docker"},
		ui.CardField{Label: "说明", Value: "仅影响后续新建容器；已有容器通常需要重建"},
	)
	ui.PrintSuccessCard("Docker 日志轮转配置完成", fields...)
	if rebuildRunningCompose != nil {
		if err := rebuildRunningCompose(view, "是否立即重建运行中的 Compose 项目使配置生效？(y/N)："); err != nil {
			return err
		}
	}
	return nil
}

func promptLogRotationConfig(view promptReader) (logRotationConfig, bool, error) {
	ui.MenuSection("请选择 Docker 日志轮转配置")
	ui.MenuOptionHint("1", "推荐配置", "100m × 3")
	ui.MenuOptionHint("2", "保守配置", "50m × 5")
	ui.MenuOptionHint("3", "节省空间", "10m × 3")
	ui.MenuOptionHint("4", "自定义配置", "max-size / max-file")
	ui.MenuExit("0/q", "返回")
	fmt.Println()
	ui.PrintField("说明", "日志驱动固定为 json-file")
	fmt.Println()

	for {
		choice, err := view.Ask("请选择：")
		if err != nil {
			return logRotationConfig{}, false, err
		}

		if shared.IsReturnChoice(choice) {
			return logRotationConfig{}, true, nil
		}
		if rotation, ok := logRotationPreset(strings.TrimSpace(choice)); ok {
			return rotation, false, nil
		}
		if strings.TrimSpace(choice) == "4" {
			break
		}
		ui.InvalidChoice()
		fmt.Println()
	}

	maxSize, err := view.Ask("请输入 max-size（正整数 + k/m/g，例如 100m）：")
	if err != nil {
		return logRotationConfig{}, false, err
	}
	if strings.TrimSpace(maxSize) != "0" && shared.IsReturnChoice(maxSize) {
		return logRotationConfig{}, true, nil
	}
	maxSize, err = normalizeMaxSize(maxSize)
	if err != nil {
		return logRotationConfig{}, false, err
	}

	maxFile, err := view.Ask("请输入 max-file（正整数，建议 1-99）：")
	if err != nil {
		return logRotationConfig{}, false, err
	}
	if strings.TrimSpace(maxFile) != "0" && shared.IsReturnChoice(maxFile) {
		return logRotationConfig{}, true, nil
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
	if err := input.Text(raw); err != nil {
		return "", err
	}
	value := strings.TrimSpace(raw)
	if !maxSizePattern.MatchString(value) {
		return "", fmt.Errorf("max-size 格式无效：%s（示例：100m，单位支持 k/m/g）", raw)
	}
	// Docker parses sizes into an int64 byte count. Check multiplication before
	// saving rather than letting an oversized setting prevent Docker startup.
	amount, err := strconv.ParseInt(value[:len(value)-1], 10, 64)
	multiplier := int64(1024)
	switch strings.ToLower(value[len(value)-1:]) {
	case "m":
		multiplier *= 1024
	case "g":
		multiplier *= 1024 * 1024
	}
	if err != nil || amount > (1<<63-1)/multiplier {
		return "", fmt.Errorf("max-size 过大，换算后的字节数不能超过 int64 范围")
	}
	return strings.ToLower(value), nil
}

func normalizeMaxFile(raw string) (string, error) {
	if err := input.Text(raw); err != nil {
		return "", err
	}
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
		backupPath, err := backupStore.Snapshot(path)
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
	if err := backupStore.RememberManaged(path, data); err != nil {
		return result, fmt.Errorf("记录 Docker 日志配置归属失败，未写入配置: %w", err)
	}
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

// CurrentSummary 返回当前 Docker 日志轮转的单行状态，供容器管理信息卡显示。
func CurrentSummary() string {
	return summarizeLogConfig(dockerDaemonPath)
}

func summarizeLogConfig(path string) string {
	config, exists, err := readDockerDaemonJSON(path)
	if err != nil {
		if exists {
			return "无法解析"
		}
		return "无法读取"
	}
	if !exists {
		return "未配置"
	}
	status, configured := dockerLogConfigStatus(config)
	if !configured {
		return "未配置"
	}
	return formatLogSummary(status)
}

func formatLogSummary(status dockerLogStatus) string {
	parts := make([]string, 0, 2)
	if status.hasLogDriver {
		if driver := scalarString(status.logDriver); driver != "" {
			parts = append(parts, driver)
		}
	}
	switch opts := status.logOpts.(type) {
	case map[string]any:
		size := scalarString(opts["max-size"])
		file := scalarString(opts["max-file"])
		switch {
		case size != "" && file != "":
			parts = append(parts, size+" × "+file)
		case size != "":
			parts = append(parts, "max-size="+size)
		case file != "":
			parts = append(parts, "max-file="+file)
		}
	default:
		if status.hasLogOpts && status.logOpts != nil {
			parts = append(parts, "日志选项无法识别")
		}
	}
	if len(parts) == 0 {
		return "未配置"
	}
	return strings.Join(parts, "，")
}

func scalarString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func logStatusFields(status dockerLogStatus) []ui.CardField {
	driver := "未设置"
	if status.hasLogDriver {
		driver = scalarString(status.logDriver)
	}
	fields := []ui.CardField{{Label: "日志驱动", Value: driver}}
	if opts, ok := status.logOpts.(map[string]any); ok && len(opts) > 0 {
		keys := make([]string, 0, len(opts))
		for key := range opts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			label := key
			switch key {
			case "max-size":
				label = "单文件大小"
			case "max-file":
				label = "保留文件数"
			}
			fields = append(fields, ui.CardField{Label: label, Value: scalarString(opts[key])})
		}
		return fields
	}
	options := "未设置"
	if status.hasLogOpts && status.logOpts != nil {
		options = scalarString(status.logOpts)
	}
	return append(fields, ui.CardField{Label: "日志选项", Value: options})
}
