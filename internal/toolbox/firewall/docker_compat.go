package firewall

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

const (
	dockerForwardMark     = "0x00800000"
	dockerForwardMarkExpr = "ct mark set ct mark or 0x00800000"
	dockerForwardUnit     = "snail-docker-forward.service"
)

// DockerForwardUnitPath is the boot helper that reinstalls the DOCKER-USER rule.
var DockerForwardUnitPath = "/etc/systemd/system/" + dockerForwardUnit

var (
	dockerForwardAttempts = 30
	dockerForwardWait     = func() { time.Sleep(time.Second) }
	errDockerUserMissing  = errors.New("DOCKER-USER 链还不存在")
)

func withoutDockerForwardMark(content string) string {
	return strings.ReplaceAll(content, dockerForwardMarkExpr+" ", "")
}

func addDockerForwardMarkLines(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.Contains(line, dockerForwardMarkExpr) {
			continue
		}
		switch {
		case strings.Contains(line, "counter accept"):
			lines[i] = strings.Replace(line, "counter accept", dockerForwardMarkExpr+" counter accept", 1)
		case strings.Contains(line, " accept"):
			lines[i] = strings.Replace(line, " accept", " "+dockerForwardMarkExpr+" accept", 1)
		}
	}
	return strings.Join(lines, "\n")
}

func withDockerForwardMark(content string) (string, bool, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return "", false, err
	}
	marked := map[string]bool{
		"forward": true, "forward_public_ports": true,
		"forward_private_ports": true, "forward_source_allow": true,
	}
	var positions []int
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text != "chain" || tokens[i+2].text != "{" || !marked[tokens[i+1].text] {
			continue
		}
		close, err := closingBrace(tokens, i+2)
		if err != nil {
			return "", false, err
		}
		for j := i + 3; j < close; j++ {
			if tokens[j].text != "counter" {
				continue
			}
			end, err := counterStatementEnd(tokens, j)
			if err != nil {
				return "", false, err
			}
			if end+1 >= len(tokens) || tokens[end+1].text != "accept" {
				j = end
				continue
			}
			lineStart := strings.LastIndex(content[:tokens[j].start], "\n") + 1
			if strings.Contains(content[lineStart:tokens[j].start], dockerForwardMarkExpr) {
				j = end
				continue
			}
			positions = append(positions, tokens[j].start)
			j = end
		}
		i = close
	}
	if len(positions) == 0 {
		return content, false, nil
	}
	updated := content
	expr := dockerForwardMarkExpr + " "
	for i := len(positions) - 1; i >= 0; i-- {
		at := positions[i]
		updated = updated[:at] + expr + updated[at:]
	}
	return updated, true, nil
}

func dockerForwardRuleArgs() []string {
	return []string{"-m", "connmark", "--mark", dockerForwardMark + "/" + dockerForwardMark, "-j", "ACCEPT"}
}

func dockerUserExists(command string) bool {
	_, err := commandOutput(command, "-S", "DOCKER-USER")
	return err == nil
}

func ensureDockerUserAccept(command string) (bool, error) {
	if !dockerUserExists(command) {
		return false, errDockerUserMissing
	}
	rule := dockerForwardRuleArgs()
	if _, err := commandOutput(command, append([]string{"-C", "DOCKER-USER"}, rule...)...); err == nil {
		return false, nil
	}
	if _, err := commandOutput(command, append([]string{"-I", "DOCKER-USER", "1"}, rule...)...); err != nil {
		return false, fmt.Errorf("写入 %s DOCKER-USER 失败: %w", command, err)
	}
	return true, nil
}

func removeDockerUserAccept(command string) (bool, error) {
	if !commandExists(command) || !dockerUserExists(command) {
		return false, nil
	}
	rule := dockerForwardRuleArgs()
	removed := false
	for {
		if _, err := commandOutput(command, append([]string{"-C", "DOCKER-USER"}, rule...)...); err != nil {
			return removed, nil
		}
		if _, err := commandOutput(command, append([]string{"-D", "DOCKER-USER"}, rule...)...); err != nil {
			return removed, fmt.Errorf("移除 %s DOCKER-USER 规则失败: %w", command, err)
		}
		removed = true
	}
}

func SyncDockerForward() error {
	var last error
	for attempt := 0; attempt < dockerForwardAttempts; attempt++ {
		if dockerUserExists("iptables") {
			last = nil
			break
		}
		_, last = commandOutput("iptables", "-S", "DOCKER-USER")
		if attempt+1 == dockerForwardAttempts {
			return fmt.Errorf("等待 DOCKER-USER 超时: %v", last)
		}
		dockerForwardWait()
	}
	if _, err := ensureDockerUserAccept("iptables"); err != nil {
		return err
	}
	if commandExists("ip6tables") {
		if _, err := ensureDockerUserAccept("ip6tables"); err != nil && !errors.Is(err, errDockerUserMissing) {
			return err
		}
	}
	return nil
}

func dockerPresent() bool {
	return commandExists("docker") || commandExists("dockerd")
}

func requireDockerPresent() error {
	if dockerPresent() {
		return nil
	}
	return fmt.Errorf("未安装 Docker，不能开启")
}

func dockerFirewallBackend() (string, error) {
	if err := requireDockerPresent(); err != nil {
		return "", err
	}
	if !commandExists("docker") {
		return "", fmt.Errorf("未找到 docker 命令")
	}
	output, err := commandOutput("docker", "info")
	if err != nil {
		return "", fmt.Errorf("读取 Docker 信息失败: %w: %s", err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "Firewall Backend" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", fmt.Errorf("docker info 没有 Firewall Backend，无法判断防火墙后端")
}

func dockerCompatEnabled() (bool, error) {
	info, err := os.Lstat(DockerForwardUnitPath)
	if os.IsNotExist(err) || (err == nil && !info.Mode().IsRegular()) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !commandExists("systemctl") {
		return false, fmt.Errorf("找到兼容服务文件，但系统没有 systemctl")
	}
	output, err := commandOutput("systemctl", "is-enabled", dockerForwardUnit)
	state := strings.TrimSpace(output)
	if state == "enabled" || state == "enabled-runtime" {
		return true, nil
	}
	if err != nil && state != "disabled" && state != "static" && state != "indirect" && state != "not-found" {
		return false, fmt.Errorf("读取 Docker 放行标记状态失败: %w: %s", err, state)
	}
	return false, nil
}

func dockerCompatStateField() ui.CardField {
	field := ui.CardField{Label: "Docker 放行标记"}
	enabled, err := dockerCompatEnabled()
	if err != nil {
		field.Value = "无法判断"
		field.Detail = err.Error()
		return field
	}
	field.Value = "已关闭"
	if enabled {
		field.Value = ui.PrimaryText("已开启")
	}
	return field
}

func dockerForwardUnitText() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法确定程序路径: %w", err)
	}
	return `[Unit]
Description=补上 Snail 转发标记放行
After=docker.service
PartOf=docker.service

[Service]
Type=oneshot
ExecStart=` + executable + ` sync-docker-forward
RemainAfterExit=yes

[Install]
WantedBy=docker.service
`, nil
}

func installDockerForwardUnit() error {
	if !commandExists("systemctl") {
		return fmt.Errorf("当前系统没有 systemctl，无法在 Docker 启动后补上兼容规则")
	}
	text, err := dockerForwardUnitText()
	if err != nil {
		return err
	}
	if err := os.WriteFile(DockerForwardUnitPath, []byte(text), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", DockerForwardUnitPath, err)
	}
	if output, err := commandOutput("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w: %s", err, strings.TrimSpace(output))
	}
	if output, err := commandOutput("systemctl", "enable", dockerForwardUnit); err != nil {
		return fmt.Errorf("启用 %s 失败: %w: %s", dockerForwardUnit, err, strings.TrimSpace(output))
	}
	return nil
}

func removeDockerForwardUnit() error {
	if !commandExists("systemctl") {
		return nil
	}
	_, _ = commandOutput("systemctl", "disable", "--now", dockerForwardUnit)
	if err := os.Remove(DockerForwardUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s 失败: %w", DockerForwardUnitPath, err)
	}
	_, _ = commandOutput("systemctl", "daemon-reload")
	return nil
}

func applyDockerForwardMarkIfEnabled(content string) (string, error) {
	enabled, err := dockerCompatEnabled()
	if err != nil || !enabled {
		return content, err
	}
	updated, _, err := withDockerForwardMark(content)
	return updated, err
}

func clearDockerForwardMarks() (bool, error) {
	version, err := readConfigVersion()
	if err != nil || !version.exists || !strings.Contains(string(version.content), dockerForwardMarkExpr) {
		return false, err
	}
	updated := withoutDockerForwardMark(string(version.content))
	if err := writeConfig(updated, version); err != nil {
		return false, err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return true, fmt.Errorf("转发标记已从配置删除，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
	}
	return true, nil
}

func prepareDockerForwardConfig() error {
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	if !version.exists || !hasManagedConfig(string(version.content)) || !strings.Contains(string(version.content), "hook forward") {
		return fmt.Errorf("请先在防火墙管理中选择 7 生成本工具防火墙配置，再开启 Docker 放行标记")
	}
	updated, changed, err := withDockerForwardMark(string(version.content))
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := writeConfig(updated, version); err != nil {
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("转发标记已写入配置，但未成功加载；原配置已备份，可输入 r 重试: %w", err)
	}
	return nil
}

func enableDockerCompat() error {
	backend, err := dockerFirewallBackend()
	if err != nil {
		return err
	}
	if backend != "iptables" {
		return fmt.Errorf("当前 Docker 防火墙后端是 %s，没有 DOCKER-USER，暂不能开启这项兼容", backend)
	}
	if !commandExists("iptables") {
		return fmt.Errorf("未找到 iptables，无法写入 DOCKER-USER")
	}
	if err := prepareDockerForwardConfig(); err != nil {
		return err
	}
	if err := installDockerForwardUnit(); err != nil {
		return err
	}
	if _, err := ensureDockerUserAccept("iptables"); err != nil {
		if errors.Is(err, errDockerUserMissing) {
			return errDockerUserMissing
		}
		return err
	}
	if commandExists("ip6tables") {
		if _, err := ensureDockerUserAccept("ip6tables"); err != nil && !errors.Is(err, errDockerUserMissing) {
			return err
		}
	}
	return nil
}

func disableDockerCompat() (bool, error) {
	removed := false
	for _, command := range []string{"iptables", "ip6tables"} {
		done, err := removeDockerUserAccept(command)
		if err != nil {
			return removed, err
		}
		removed = removed || done
	}
	enabled, err := dockerCompatEnabled()
	if err != nil {
		return removed, err
	}
	if enabled || fileExists(DockerForwardUnitPath) {
		if err := removeDockerForwardUnit(); err != nil {
			return removed, err
		}
		removed = true
	}
	cleared, err := clearDockerForwardMarks()
	if err != nil {
		return removed, err
	}
	return removed || cleared, nil
}

func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func ConfigureDockerCompat(view *ui.UI) error {
	if !isRoot() {
		return fmt.Errorf("设置 Docker 放行标记需要 root 权限，请使用 sudo 运行本工具")
	}
	for {
		enabled, err := dockerCompatEnabled()
		if err != nil {
			return err
		}
		state := "已关闭"
		if enabled {
			state = ui.PrimaryText("已开启")
		}
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "常用功能设置", "Docker 放行标记")
		ui.PrintInfoCard("Docker 放行标记",
			ui.CardField{Label: "开关", Value: state},
			ui.CardField{Label: "作用", Value: "本工具转发放行后，Docker 不再把这些连接丢掉"},
			ui.CardField{Label: "做法", Value: "转发放行打连接标记，DOCKER-USER 只接受这个标记"},
			ui.CardField{Label: "开机", Value: "Docker 每次启动后自动补上这条规则"},
			ui.CardField{Label: "不影响", Value: "没有标记的流量仍走 Docker 原来的规则"},
			ui.CardField{Label: "前提", Value: "已安装 Docker 才能开启，关闭不受此限制"},
		)
		ui.MenuOption("1", "开启")
		ui.MenuOption("2", "关闭")
		ui.MenuExit("0/q", "返回常用功能设置")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
		if err != nil {
			return err
		}
		if choice == "on" {
			if err := requireDockerPresent(); err != nil {
				return err
			}
			confirmed, err := view.Confirm("确认开启 Docker 放行标记？会更新防火墙转发规则并加载。(y/N，0/q 返回)：")
			if errors.Is(err, shared.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return err
			}
			if !confirmed {
				return shared.ErrReturnToMenu
			}
			err = enableDockerCompat()
			if errors.Is(err, errDockerUserMissing) {
				ui.PrintSuccessCard("Docker 放行标记已开启",
					ui.CardField{Label: "当前规则", Value: "DOCKER-USER 还不存在，Docker 启动后自动补上"},
					ui.CardField{Label: "开机", Value: dockerForwardUnit},
				)
				return nil
			}
			if err != nil {
				return err
			}
			log.Info("Docker 放行标记已开启")
			ui.PrintSuccessCard("Docker 放行标记已开启",
				ui.CardField{Label: "DOCKER-USER", Value: "已接受转发标记 " + dockerForwardMark},
				ui.CardField{Label: "开机", Value: dockerForwardUnit},
			)
			return nil
		}
		confirmed, err := view.Confirm("确认关闭 Docker 放行标记？Docker 会重新拦截未被它自己放行的转发。(y/N，0/q 返回)：")
		if errors.Is(err, shared.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			return shared.ErrReturnToMenu
		}
		removed, err := disableDockerCompat()
		if err != nil {
			return err
		}
		if !removed {
			ui.PrintInfoCard("无需修改", ui.CardField{Label: "开关", Value: "已关闭"})
			view.Pause()
			continue
		}
		ui.PrintSuccessCard("Docker 放行标记已关闭",
			ui.CardField{Label: "DOCKER-USER", Value: "已移除转发标记放行"},
			ui.CardField{Label: "防火墙标记", Value: "已从防火墙配置中去掉"},
		)
		return nil
	}
}
