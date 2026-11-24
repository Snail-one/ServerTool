package firewall

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

const (
	dockerForwardMark     = "0x00800000"
	dockerForwardMarkExpr = "ct mark set ct mark or 0x00800000"
	dockerForwardUnit     = "snail-docker-forward.service"
	dockerForwardComment  = "snail-docker-forward"
)

var dockerForwardUnitPath = "/etc/systemd/system/" + dockerForwardUnit

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
	return configureDockerForwardMark(content, true)
}

func configureDockerForwardMark(content string, enabled bool) (string, bool, error) {
	tokens, err := configTokens(content)
	if err != nil {
		return "", false, err
	}
	type edit struct {
		start, end  int
		replacement string
	}
	var edits []edit
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].text != "table" || tokens[i+1].text != "inet" || tokens[i+2].text != "snail" || tokens[i+3].text != "{" {
			continue
		}
		close, err := closingBrace(tokens, i+3)
		if err != nil {
			return "", false, err
		}
		for j := i + 4; j+2 < close; j++ {
			if tokens[j].text != "chain" || tokens[j+2].text != "{" {
				continue
			}
			end, err := closingBrace(tokens, j+2)
			if err != nil {
				return "", false, err
			}
			chain := tokens[j+1].text
			if chain == "forward" || !enabled && dockerForwardPolicyChain(chain) {
				// Older configs also marked the generic established/related rule.
				start, stop := tokens[j+2].end, tokens[end].start
				body := withoutDockerForwardMark(content[start:stop])
				if body != content[start:stop] {
					edits = append(edits, edit{start, stop, body})
				}
			}
			if enabled && dockerForwardPolicyChain(chain) {
				for _, at := range dockerForwardMarkPositions(content, tokens[j+3:end]) {
					edits = append(edits, edit{at, at, dockerForwardMarkExpr + " "})
				}
			}
			j = end
		}
		i = close
	}
	for i := len(edits) - 1; i >= 0; i-- {
		change := edits[i]
		content = content[:change.start] + change.replacement + content[change.end:]
	}
	return content, len(edits) > 0, nil
}

func dockerForwardPolicyChain(chain string) bool {
	switch chain {
	case "forward_public_ports", "forward_private_ports", "forward_source_allow":
		return true
	}
	return false
}

// Insert before each rule's accept verdict, keeping a trailing anonymous
// counter after the mark when present. Ignore policies and nested expressions.
func dockerForwardMarkPositions(content string, tokens []nftToken) []int {
	var positions []int
	ruleStart, depth := 0, 0
	for i, token := range tokens {
		if depth == 0 && i > 0 {
			prior := tokens[i-1]
			if prior.text == ";" || prior.text != "\\" && strings.Contains(content[prior.end:token.start], "\n") {
				ruleStart = i
			}
		}
		switch token.text {
		case "{":
			depth++
		case "}":
			depth--
		case "accept":
			if depth != 0 {
				continue
			}
			prior := i - 1
			for prior >= ruleStart && tokens[prior].text == "\\" {
				prior--
			}
			if (prior >= ruleStart && tokens[prior].text == "policy") || hasDockerForwardMark(tokens[ruleStart:i]) {
				continue
			}
			at := i
			for at-2 >= ruleStart && (tokens[at-2].text == "packets" || tokens[at-2].text == "bytes") {
				at -= 2
			}
			if at > ruleStart && tokens[at-1].text == "counter" {
				at--
			} else {
				at = i
			}
			positions = append(positions, tokens[at].start)
		}
	}
	return positions
}

func hasDockerForwardMark(tokens []nftToken) bool {
	mark := strings.Fields(dockerForwardMarkExpr)
	for i := 0; i+len(mark) <= len(tokens); i++ {
		if slices.EqualFunc(tokens[i:i+len(mark)], mark, func(token nftToken, word string) bool {
			return token.text == word
		}) {
			return true
		}
	}
	return false
}

func dockerForwardRuleArgs() []string {
	return []string{"-m", "connmark", "--mark", dockerForwardMark + "/" + dockerForwardMark,
		"-m", "comment", "--comment", dockerForwardComment, "-j", "ACCEPT"}
}

func dockerUserAttached(command string) (bool, error) {
	output, err := commandOutput(command, "-S", "FORWARD")
	if err != nil {
		return false, fmt.Errorf("读取 %s FORWARD 失败: %w: %s", command, err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Join(strings.Fields(line), " ") == "-A FORWARD -j DOCKER-USER" {
			return true, nil
		}
	}
	return false, nil
}

// Inspect only the exact managed rule and its untagged predecessor.
func dockerUserRules(command string) (rules [][]string, err error) {
	output, err := commandOutput(command, "-S", "DOCKER-USER")
	if err != nil {
		return nil, fmt.Errorf("读取 %s DOCKER-USER 失败: %w: %s", command, err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "-A" && fields[1] == "DOCKER-USER" {
			rules = append(rules, fields[2:])
		}
	}
	return rules, nil
}

func dockerUserManagedRule(rule []string) (owned, tagged bool) {
	// iptables may print hexadecimal marks without leading zeroes.
	if len(rule) < 6 || !slices.Equal(rule[:3], []string{"-m", "connmark", "--mark"}) {
		return false, false
	}
	legacy := []string{"-j", "ACCEPT"}
	tagged = len(rule) == 10 && rule[4] == "-m" && rule[5] == "comment" &&
		rule[6] == "--comment" && strings.Trim(rule[7], `"`) == dockerForwardComment &&
		slices.Equal(rule[8:], legacy)
	if !tagged && !slices.Equal(rule[4:], legacy) {
		return false, false
	}
	value, mask, found := strings.Cut(rule[3], "/")
	mark, markErr := strconv.ParseUint(value, 0, 32)
	bits, maskErr := strconv.ParseUint(mask, 0, 32)
	return found && markErr == nil && maskErr == nil && mark == 0x00800000 && bits == mark, tagged
}

func dockerUserRulePosition(rules [][]string) (position int, ready bool) {
	position = 1
	ruleIndex, managed := 0, 0
	beforeReturn := true
	for i, rule := range rules {
		owned, tagged := dockerUserManagedRule(rule)
		if owned {
			managed++
			if tagged {
				ruleIndex = i + 1
			}
			continue
		}
		if slices.Equal(rule, []string{"-j", "RETURN"}) {
			beforeReturn = false
		}
		if beforeReturn {
			position++
		}
	}
	return position, managed == 1 && ruleIndex == position
}

func deleteDockerUserRules(command string, rules [][]string) (bool, error) {
	removed := false
	for _, rule := range rules {
		owned, tagged := dockerUserManagedRule(rule)
		if !owned {
			continue
		}
		args := dockerForwardRuleArgs()
		if !tagged {
			args = append(args[:4:4], "-j", "ACCEPT")
		}
		if output, err := commandOutput(command, append([]string{"-D", "DOCKER-USER"}, args...)...); err != nil {
			return removed, fmt.Errorf("移除 %s DOCKER-USER 规则失败: %w: %s", command, err, strings.TrimSpace(output))
		}
		removed = true
	}
	return removed, nil
}

func ensureDockerUserAccept(command string) error {
	rules, err := dockerUserRules(command)
	if err != nil {
		return err
	}
	position, ready := dockerUserRulePosition(rules)
	if ready {
		return nil
	}
	if _, err := deleteDockerUserRules(command, rules); err != nil {
		return err
	}
	args := append([]string{"-I", "DOCKER-USER", strconv.Itoa(position)}, dockerForwardRuleArgs()...)
	if output, err := commandOutput(command, args...); err != nil {
		return fmt.Errorf("写入 %s DOCKER-USER 失败: %w: %s", command, err, strings.TrimSpace(output))
	}
	return nil
}

func removeDockerUserAccept(command string) (bool, error) {
	if !commandExists(command) {
		return false, nil
	}
	output, err := commandOutput(command, "-S")
	if err != nil {
		return false, fmt.Errorf("读取 %s filter 表失败: %w: %s", command, err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "-N DOCKER-USER" {
			rules, err := dockerUserRules(command)
			if err != nil {
				return false, err
			}
			return deleteDockerUserRules(command, rules)
		}
	}
	return false, nil
}

func syncDockerUserRules() error {
	attached, err := dockerUserAttached("iptables")
	if err != nil {
		return err
	}
	if !attached {
		return errDockerUserMissing
	}
	if err := ensureDockerUserAccept("iptables"); err != nil {
		return err
	}
	if commandExists("ip6tables") {
		attached, err := dockerUserAttached("ip6tables")
		if err != nil {
			return err
		}
		if attached {
			return ensureDockerUserAccept("ip6tables")
		}
	}
	return nil
}

func SyncDockerForward() error {
	release, err := lockDockerForward()
	if err != nil {
		return err
	}
	defer release()
	enabled, err := dockerCompatEnabled()
	if err != nil || !enabled {
		return err
	}
	backend, backendErr := dockerFirewallBackend()
	if backend != "" && backend != "iptables" {
		return fmt.Errorf("当前 Docker 防火墙后端是 %s，无法同步 DOCKER-USER", backend)
	}
	if err := migrateDockerForwardConfig(); err != nil {
		return err
	}
	for attempt := 0; attempt < dockerForwardAttempts; attempt++ {
		err := syncDockerUserRules()
		if !errors.Is(err, errDockerUserMissing) {
			return err
		}
		if attempt+1 < dockerForwardAttempts {
			dockerForwardWait()
		}
	}
	if backendErr != nil {
		return fmt.Errorf("等待 FORWARD 跳转到 DOCKER-USER 超时: %w", backendErr)
	}
	return fmt.Errorf("等待 FORWARD 跳转到 DOCKER-USER 超时: %w", errDockerUserMissing)
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
	if !commandExists("docker") {
		return "", nil
	}
	output, err := commandOutput("docker", "--host", "unix:///var/run/docker.sock", "info")
	if err != nil {
		return "", fmt.Errorf("读取 Docker 信息失败: %w: %s", err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "Firewall Backend" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", nil
}

func dockerCompatEnabled() (bool, error) {
	info, err := os.Lstat(dockerForwardUnitPath)
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

func liveDockerForwardConfig() (string, error) {
	output, err := commandOutput("nft", "-nn", "list", "table", "inet", "snail")
	if err != nil {
		if strings.Contains(output, "No such file or directory") {
			return "", nil
		}
		return "", fmt.Errorf("读取内核转发标记失败: %w: %s", err, strings.TrimSpace(output))
	}
	// nft prints the symbolic 'or' operator as a vertical bar.
	return strings.ReplaceAll(output, "ct mark set ct mark | 0x00800000", dockerForwardMarkExpr), nil
}

func dockerForwardMarksLoaded(expected, live string) bool {
	if !strings.Contains(live, "hook forward") || strings.Contains(live, "flags dormant") {
		return false
	}
	wanted := strings.Count(expected, dockerForwardMarkExpr)
	if wanted == 0 || wanted != strings.Count(live, dockerForwardMarkExpr) {
		return false
	}
	_, changed, err := withDockerForwardMark(live)
	return err == nil && !changed
}

func dockerCompatStateField() ui.CardField {
	field := ui.CardField{Label: "Docker 放行标记"}
	label, err := dockerCompatStatus()
	field.Value = label
	if err != nil {
		field.Detail = err.Error()
	}
	return field
}

func dockerCompatStatus() (string, error) {
	enabled, err := dockerCompatEnabled()
	if err != nil {
		return "无法判断", err
	}
	version, err := readConfigVersion()
	if err != nil {
		return "无法判断", err
	}
	saved := string(version.content)
	if !enabled {
		if fileExists(dockerForwardUnitPath) {
			return "规则未同步", nil
		}
		if strings.Contains(saved, dockerForwardMarkExpr) {
			_, marked, err := configureDockerForwardMark(saved, false)
			if err != nil {
				return "无法判断", err
			}
			if marked {
				return "规则未同步", nil
			}
		}
		return "已关闭", nil
	}
	if backend, _ := dockerFirewallBackend(); backend != "" && backend != "iptables" {
		return "后端不兼容（" + backend + "）", nil
	}
	expected, changed, err := withDockerForwardMark(saved)
	if err != nil {
		return "无法判断", err
	}
	if changed || !strings.Contains(expected, dockerForwardMarkExpr) {
		return "规则未同步", nil
	}
	live, err := liveDockerForwardConfig()
	if err != nil {
		return "无法判断", err
	}
	if !dockerForwardMarksLoaded(expected, live) {
		return "规则未同步", nil
	}
	for _, command := range []string{"iptables", "ip6tables"} {
		if !commandExists(command) {
			if command == "iptables" {
				return "规则未同步", fmt.Errorf("未找到 iptables")
			}
			continue
		}
		attached, err := dockerUserAttached(command)
		if err != nil {
			return "无法判断", err
		}
		if !attached {
			if command == "iptables" {
				return "等待 Docker 转发链", nil
			}
			continue
		}
		rules, err := dockerUserRules(command)
		if err != nil {
			return "无法判断", err
		}
		if _, ready := dockerUserRulePosition(rules); !ready {
			return "规则未同步", nil
		}
	}
	return ui.PrimaryText("已生效"), nil
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
	if err := os.WriteFile(dockerForwardUnitPath, []byte(text), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", dockerForwardUnitPath, err)
	}
	if output, err := commandOutput("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w: %s", err, strings.TrimSpace(output))
	}
	if output, err := commandOutput("systemctl", "enable", dockerForwardUnit); err != nil {
		return fmt.Errorf("启用 %s 失败: %w: %s", dockerForwardUnit, err, strings.TrimSpace(output))
	}
	return nil
}

func stopDockerForwardUnit() error {
	if !commandExists("systemctl") {
		return fmt.Errorf("未找到 systemctl，无法停止 Docker 放行标记服务")
	}
	if output, err := commandOutput("systemctl", "disable", "--now", dockerForwardUnit); err != nil {
		return fmt.Errorf("关闭 %s 失败: %w: %s", dockerForwardUnit, err, strings.TrimSpace(output))
	}
	return nil
}

func removeDockerForwardUnit() error {
	if err := shared.RemoveRegularFile(dockerForwardUnitPath); err != nil {
		return err
	}
	if output, err := commandOutput("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// RemoveDockerForwardService removes the boot helper before Docker is uninstalled.
func RemoveDockerForwardService() error {
	release, err := lockDockerForward()
	if err != nil {
		return err
	}
	defer release()
	if !fileExists(dockerForwardUnitPath) {
		return nil
	}
	if err := stopDockerForwardUnit(); err != nil {
		return err
	}
	return removeDockerForwardUnit()
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
	if err != nil || !version.exists || !hasManagedConfig(string(version.content)) {
		return false, err
	}
	content := string(version.content)
	updated, changed, err := configureDockerForwardMark(content, false)
	if err != nil {
		return false, err
	}
	if changed {
		saved, err := saveAndApplyConfig(version, updated)
		if err != nil && saved {
			err = fmt.Errorf("转发标记已从配置删除，但未成功加载；可重新选择关闭重试: %w", err)
		}
		return err == nil, err
	}
	live, err := liveDockerForwardConfig()
	if err != nil {
		return false, err
	}
	_, marked, err := configureDockerForwardMark(live, false)
	if err != nil || !marked {
		return false, err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return false, fmt.Errorf("转发标记已从配置删除，但未成功加载；可重新选择关闭重试: %w", err)
	}
	return true, nil
}

func prepareDockerForwardConfig() error {
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	if !version.exists || !hasManagedConfig(string(version.content)) || !strings.Contains(string(version.content), "hook forward") {
		return fmt.Errorf("请先在防火墙管理中选择 d 生成本工具防火墙配置，再开启 Docker 放行标记")
	}
	updated, changed, err := withDockerForwardMark(string(version.content))
	if err != nil {
		return err
	}
	if !strings.Contains(updated, dockerForwardMarkExpr) {
		return fmt.Errorf("本工具防火墙配置没有可设置标记的转发规则")
	}
	if changed {
		saved, err := saveAndApplyConfig(version, updated)
		if err != nil && saved {
			err = fmt.Errorf("转发标记已保存，但未成功加载；可重新选择开启重试: %w", err)
		}
		return err
	}
	if err := applyConfig([]byte(updated)); err != nil {
		return fmt.Errorf("转发标记已保存，但未成功加载；可重新选择开启重试: %w", err)
	}
	return nil
}

// Existing installations can still have the broad connection-state mark.
func migrateDockerForwardConfig() error {
	version, err := readConfigVersion()
	if err != nil || !version.exists {
		return err
	}
	content := string(version.content)
	if !hasManagedConfig(content) || !strings.Contains(content, dockerForwardMarkExpr) {
		return nil
	}
	updated, changed, err := withDockerForwardMark(content)
	if err != nil || !changed {
		return err
	}
	_, err = saveAndApplyConfig(version, updated)
	return err
}

func enableDockerCompat() error {
	release, err := lockDockerForward()
	if err != nil {
		return err
	}
	defer release()
	if err := requireDockerPresent(); err != nil {
		return err
	}
	if !commandExists("iptables") || !commandExists("systemctl") {
		return fmt.Errorf("Docker 放行标记需要 iptables 和 systemctl")
	}
	backend, backendErr := dockerFirewallBackend()
	if backend != "" && backend != "iptables" {
		return fmt.Errorf("当前 Docker 防火墙后端是 %s，无法使用 DOCKER-USER", backend)
	}
	attached, err := dockerUserAttached("iptables")
	if err != nil {
		return err
	}
	if !attached && backend != "iptables" {
		state, _ := commandOutput("systemctl", "is-active", "docker.service")
		switch strings.TrimSpace(state) {
		case "inactive", "activating", "failed":
			// The boot helper will check the backend and chain before writing.
		default:
			if backendErr != nil {
				return backendErr
			}
			return fmt.Errorf("本机 iptables FORWARD 未跳转到 DOCKER-USER，无法确认 Docker 防火墙后端")
		}
	}
	if err := prepareDockerForwardConfig(); err != nil {
		return err
	}
	if err := installDockerForwardUnit(); err != nil {
		return err
	}
	return syncDockerUserRules()
}

func disableDockerCompat() (bool, error) {
	release, err := lockDockerForward()
	if err != nil {
		return false, err
	}
	defer release()
	removed := false
	// Stop any running boot helper before removing rules it could reinstall.
	hasUnit := fileExists(dockerForwardUnitPath)
	if hasUnit {
		if err := stopDockerForwardUnit(); err != nil {
			return false, err
		}
	}
	for _, command := range []string{"iptables", "ip6tables"} {
		done, err := removeDockerUserAccept(command)
		if err != nil {
			return removed, err
		}
		removed = removed || done
	}
	cleared, err := clearDockerForwardMarks()
	if err != nil || !hasUnit {
		return removed || cleared, err
	}
	return true, removeDockerForwardUnit()
}

func lockDockerForward() (func(), error) {
	guard := tempguard.Guard{Path: fmt.Sprintf("/tmp/.servertool-docker-forward-%d.lock", os.Geteuid())}
	release, err := guard.Cleanup()
	if err != nil {
		return nil, fmt.Errorf("无法锁定 Docker 放行标记操作，请稍后重试: %w", err)
	}
	return release, nil
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
		ui.ClearScreen()
		ui.MenuTitle("防火墙管理", "转发规则管理", "Docker 放行标记")
		ui.PrintInfoCard("Docker 放行标记",
			dockerCompatStateField(),
			ui.CardField{Label: "作用", Value: "让 Docker 接受命中本工具转发端口或来源放行的连接"},
			ui.CardField{Label: "做法", Value: "带标记的连接先经过 DOCKER-USER 中已有的过滤规则"},
			ui.CardField{Label: "开机", Value: "Docker 每次启动后自动补上这条规则"},
			ui.CardField{Label: "不影响", Value: "没有标记的流量仍走 Docker 原来的规则"},
			ui.CardField{Label: "前提", Value: "需要 Docker 的 iptables 后端"},
		)
		ui.MenuOption("1", "开启")
		ui.MenuOption("2", "关闭")
		ui.MenuExit("0/q", "返回转发规则管理")
		choice, err := askRuleChoice(view, "请选择", "", map[string]string{"1": "on", "2": "off"})
		if err != nil {
			return err
		}
		if choice == "on" {
			if err := requireDockerPresent(); err != nil {
				return err
			}
			confirmed, err := confirmMenuChange(view, "确认开启 Docker 放行标记？会更新防火墙转发规则并加载。", "转发规则管理")
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
					ui.CardField{Label: "当前规则", Value: "等待 FORWARD 跳转到 DOCKER-USER，Docker 启动后会尝试同步"},
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
		confirmed, err := confirmMenuChange(view, "确认关闭 Docker 放行标记？会移除打标规则和 Docker 中对应的接收规则。", "转发规则管理")
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
