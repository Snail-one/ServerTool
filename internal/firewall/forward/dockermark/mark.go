package dockermark

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/storage"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

const (
	dockerForwardMark    = "0x00800000"
	MarkExpression       = "ct mark set ct mark or 0x00800000"
	dockerForwardUnit    = "snail-docker-forward.service"
	dockerForwardComment = "snail-docker-forward"
)

type Service struct {
	Store            configuration.Store
	UnitPath         string
	IsRoot           func() bool
	CommandExists    func(string) bool
	HasManagedConfig func(string) bool
}

const DefaultUnitPath = "/etc/systemd/system/" + dockerForwardUnit

var (
	dockerForwardAttempts = 30
	dockerForwardWait     = func() { time.Sleep(time.Second) }
	errDockerUserMissing  = errors.New("DOCKER-USER 链还不存在")
)

func WithoutMark(content string) string {
	return strings.ReplaceAll(content, MarkExpression+" ", "")
}

func MarkManagedAccepts(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.Contains(line, MarkExpression) {
			continue
		}
		switch {
		case strings.Contains(line, "counter accept"):
			lines[i] = strings.Replace(line, "counter accept", MarkExpression+" counter accept", 1)
		case strings.Contains(line, " accept"):
			lines[i] = strings.Replace(line, " accept", " "+MarkExpression+" accept", 1)
		}
	}
	return strings.Join(lines, "\n")
}

func MarkForwardAccepts(content string) (string, bool, error) {
	return configureDockerForwardMark(content, true)
}

func configureDockerForwardMark(content string, enabled bool) (string, bool, error) {
	tokens, err := configuration.Tokens(content)
	if err != nil {
		return "", false, err
	}
	type edit struct {
		start, end  int
		replacement string
	}
	var edits []edit
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].Text != "table" || tokens[i+1].Text != "inet" || tokens[i+2].Text != "snail" || tokens[i+3].Text != "{" {
			continue
		}
		close, err := configuration.ClosingBrace(tokens, i+3)
		if err != nil {
			return "", false, err
		}
		for j := i + 4; j+2 < close; j++ {
			if tokens[j].Text != "chain" || tokens[j+2].Text != "{" {
				continue
			}
			end, err := configuration.ClosingBrace(tokens, j+2)
			if err != nil {
				return "", false, err
			}
			chain := tokens[j+1].Text
			if chain == "forward" || !enabled && dockerForwardPolicyChain(chain) {
				// Older configs also marked the generic established/related rule.
				start, stop := tokens[j+2].End, tokens[end].Start
				body := WithoutMark(content[start:stop])
				if body != content[start:stop] {
					edits = append(edits, edit{start, stop, body})
				}
			}
			if enabled && dockerForwardPolicyChain(chain) {
				for _, at := range dockerForwardMarkPositions(content, tokens[j+3:end]) {
					edits = append(edits, edit{at, at, MarkExpression + " "})
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
func dockerForwardMarkPositions(content string, tokens []configuration.Token) []int {
	var positions []int
	ruleStart, depth := 0, 0
	for i, token := range tokens {
		if depth == 0 && i > 0 {
			prior := tokens[i-1]
			if prior.Text == ";" || prior.Text != "\\" && strings.Contains(content[prior.End:token.Start], "\n") {
				ruleStart = i
			}
		}
		switch token.Text {
		case "{":
			depth++
		case "}":
			depth--
		case "accept":
			if depth != 0 {
				continue
			}
			prior := i - 1
			for prior >= ruleStart && tokens[prior].Text == "\\" {
				prior--
			}
			if (prior >= ruleStart && tokens[prior].Text == "policy") || hasDockerForwardMark(tokens[ruleStart:i]) {
				continue
			}
			at := i
			for at-2 >= ruleStart && (tokens[at-2].Text == "packets" || tokens[at-2].Text == "bytes") {
				at -= 2
			}
			if at > ruleStart && tokens[at-1].Text == "counter" {
				at--
			} else {
				at = i
			}
			positions = append(positions, tokens[at].Start)
		}
	}
	return positions
}

func hasDockerForwardMark(tokens []configuration.Token) bool {
	mark := strings.Fields(MarkExpression)
	for i := 0; i+len(mark) <= len(tokens); i++ {
		if slices.EqualFunc(tokens[i:i+len(mark)], mark, func(token configuration.Token, word string) bool {
			return token.Text == word
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

func (service Service) dockerUserAttached(command string) (bool, error) {
	output, err := service.Store.CommandOutput(command, "-S", "FORWARD")
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
func (service Service) dockerUserRules(command string) (rules [][]string, err error) {
	output, err := service.Store.CommandOutput(command, "-S", "DOCKER-USER")
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

func (service Service) deleteDockerUserRules(command string, rules [][]string) (bool, error) {
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
		if output, err := service.Store.CommandOutput(command, append([]string{"-D", "DOCKER-USER"}, args...)...); err != nil {
			return removed, fmt.Errorf("移除 %s DOCKER-USER 规则失败: %w: %s", command, err, strings.TrimSpace(output))
		}
		removed = true
	}
	return removed, nil
}

func (service Service) ensureDockerUserAccept(command string) error {
	rules, err := service.dockerUserRules(command)
	if err != nil {
		return err
	}
	position, ready := dockerUserRulePosition(rules)
	if ready {
		return nil
	}
	if _, err := service.deleteDockerUserRules(command, rules); err != nil {
		return err
	}
	args := append([]string{"-I", "DOCKER-USER", strconv.Itoa(position)}, dockerForwardRuleArgs()...)
	if output, err := service.Store.CommandOutput(command, args...); err != nil {
		return fmt.Errorf("写入 %s DOCKER-USER 失败: %w: %s", command, err, strings.TrimSpace(output))
	}
	return nil
}

func (service Service) removeDockerUserAccept(command string) (bool, error) {
	if !service.CommandExists(command) {
		return false, nil
	}
	output, err := service.Store.CommandOutput(command, "-S")
	if err != nil {
		return false, fmt.Errorf("读取 %s filter 表失败: %w: %s", command, err, strings.TrimSpace(output))
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "-N DOCKER-USER" {
			rules, err := service.dockerUserRules(command)
			if err != nil {
				return false, err
			}
			return service.deleteDockerUserRules(command, rules)
		}
	}
	return false, nil
}

func (service Service) syncDockerUserRules() error {
	attached, err := service.dockerUserAttached("iptables")
	if err != nil {
		return err
	}
	if !attached {
		return errDockerUserMissing
	}
	if err := service.ensureDockerUserAccept("iptables"); err != nil {
		return err
	}
	if service.CommandExists("ip6tables") {
		attached, err := service.dockerUserAttached("ip6tables")
		if err != nil {
			return err
		}
		if attached {
			return service.ensureDockerUserAccept("ip6tables")
		}
	}
	return nil
}

func (service Service) Sync() error {
	release, err := lockDockerForward()
	if err != nil {
		return err
	}
	defer release()
	enabled, err := service.dockerCompatEnabled()
	if err != nil || !enabled {
		return err
	}
	backend, backendErr := service.dockerFirewallBackend()
	if backend != "" && backend != "iptables" {
		return fmt.Errorf("当前 Docker 防火墙后端是 %s，无法同步 DOCKER-USER", backend)
	}
	if err := service.migrateDockerForwardConfig(); err != nil {
		return err
	}
	for attempt := 0; attempt < dockerForwardAttempts; attempt++ {
		err := service.syncDockerUserRules()
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

func (service Service) dockerPresent() bool {
	return service.CommandExists("docker") || service.CommandExists("dockerd")
}

func (service Service) RequireDocker() error {
	if service.dockerPresent() {
		return nil
	}
	return fmt.Errorf("未安装 Docker，不能开启")
}

func (service Service) dockerFirewallBackend() (string, error) {
	if !service.CommandExists("docker") {
		return "", nil
	}
	output, err := service.Store.CommandOutput("docker", "--host", "unix:///var/run/docker.sock", "info")
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

func (service Service) dockerCompatEnabled() (bool, error) {
	info, err := os.Lstat(service.UnitPath)
	if os.IsNotExist(err) || (err == nil && !info.Mode().IsRegular()) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !service.CommandExists("systemctl") {
		return false, fmt.Errorf("找到兼容服务文件，但系统没有 systemctl")
	}
	output, err := service.Store.CommandOutput("systemctl", "is-enabled", dockerForwardUnit)
	state := strings.TrimSpace(output)
	if state == "enabled" || state == "enabled-runtime" {
		return true, nil
	}
	if err != nil && state != "disabled" && state != "static" && state != "indirect" && state != "not-found" {
		return false, fmt.Errorf("读取 Docker 放行标记状态失败: %w: %s", err, state)
	}
	return false, nil
}

func (service Service) liveDockerForwardConfig() (string, error) {
	output, err := service.Store.CommandOutput("nft", "-nn", "list", "table", "inet", "snail")
	if err != nil {
		if strings.Contains(output, "No such file or directory") {
			return "", nil
		}
		return "", fmt.Errorf("读取内核转发标记失败: %w: %s", err, strings.TrimSpace(output))
	}
	// nft prints the symbolic 'or' operator as a vertical bar.
	return strings.ReplaceAll(output, "ct mark set ct mark | 0x00800000", MarkExpression), nil
}

func dockerForwardMarksLoaded(expected, live string) bool {
	if !strings.Contains(live, "hook forward") || strings.Contains(live, "flags dormant") {
		return false
	}
	wanted := strings.Count(expected, MarkExpression)
	if wanted == 0 || wanted != strings.Count(live, MarkExpression) {
		return false
	}
	_, changed, err := MarkForwardAccepts(live)
	return err == nil && !changed
}

func (service Service) StateField() ui.CardField {
	field := ui.CardField{Label: "Docker 放行标记"}
	label, err := service.dockerCompatStatus()
	field.Value = label
	if err != nil {
		field.Detail = err.Error()
	}
	return field
}

func (service Service) dockerCompatStatus() (string, error) {
	enabled, err := service.dockerCompatEnabled()
	if err != nil {
		return "无法判断", err
	}
	version, err := service.Store.Read()
	if err != nil {
		return "无法判断", err
	}
	saved := string(version.Content)
	if !enabled {
		if fileExists(service.UnitPath) {
			return "规则未同步", nil
		}
		if strings.Contains(saved, MarkExpression) {
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
	if backend, _ := service.dockerFirewallBackend(); backend != "" && backend != "iptables" {
		return "后端不兼容（" + backend + "）", nil
	}
	expected, changed, err := MarkForwardAccepts(saved)
	if err != nil {
		return "无法判断", err
	}
	if changed || !strings.Contains(expected, MarkExpression) {
		return "规则未同步", nil
	}
	live, err := service.liveDockerForwardConfig()
	if err != nil {
		return "无法判断", err
	}
	if !dockerForwardMarksLoaded(expected, live) {
		return "规则未同步", nil
	}
	for _, command := range []string{"iptables", "ip6tables"} {
		if !service.CommandExists(command) {
			if command == "iptables" {
				return "规则未同步", fmt.Errorf("未找到 iptables")
			}
			continue
		}
		attached, err := service.dockerUserAttached(command)
		if err != nil {
			return "无法判断", err
		}
		if !attached {
			if command == "iptables" {
				return "等待 Docker 转发链", nil
			}
			continue
		}
		rules, err := service.dockerUserRules(command)
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

func (service Service) installDockerForwardUnit() error {
	if !service.CommandExists("systemctl") {
		return fmt.Errorf("当前系统没有 systemctl，无法在 Docker 启动后补上兼容规则")
	}
	text, err := dockerForwardUnitText()
	if err != nil {
		return err
	}
	if err := os.WriteFile(service.UnitPath, []byte(text), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", service.UnitPath, err)
	}
	if output, err := service.Store.CommandOutput("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w: %s", err, strings.TrimSpace(output))
	}
	if output, err := service.Store.CommandOutput("systemctl", "enable", dockerForwardUnit); err != nil {
		return fmt.Errorf("启用 %s 失败: %w: %s", dockerForwardUnit, err, strings.TrimSpace(output))
	}
	return nil
}

func (service Service) stopDockerForwardUnit() error {
	if !service.CommandExists("systemctl") {
		return fmt.Errorf("未找到 systemctl，无法停止 Docker 放行标记服务")
	}
	if output, err := service.Store.CommandOutput("systemctl", "disable", "--now", dockerForwardUnit); err != nil {
		return fmt.Errorf("关闭 %s 失败: %w: %s", dockerForwardUnit, err, strings.TrimSpace(output))
	}
	return nil
}

func (service Service) removeDockerForwardUnit() error {
	if err := storage.RemoveRegularFile(service.UnitPath); err != nil {
		return err
	}
	if output, err := service.Store.CommandOutput("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// RemoveDockerForwardService removes the boot helper before Docker is uninstalled.
func (service Service) RemoveService() error {
	release, err := lockDockerForward()
	if err != nil {
		return err
	}
	defer release()
	if !fileExists(service.UnitPath) {
		return nil
	}
	if err := service.stopDockerForwardUnit(); err != nil {
		return err
	}
	return service.removeDockerForwardUnit()
}

func (service Service) ApplyIfEnabled(content string) (string, error) {
	enabled, err := service.dockerCompatEnabled()
	if err != nil || !enabled {
		return content, err
	}
	updated, _, err := MarkForwardAccepts(content)
	return updated, err
}

func (service Service) clearDockerForwardMarks() (bool, error) {
	version, err := service.Store.Read()
	if err != nil || !version.Exists || !service.HasManagedConfig(string(version.Content)) {
		return false, err
	}
	content := string(version.Content)
	updated, changed, err := configureDockerForwardMark(content, false)
	if err != nil {
		return false, err
	}
	if changed {
		saved, err := service.Store.SaveAndApply(version, updated)
		if err != nil && saved {
			err = fmt.Errorf("转发标记已从配置删除，但未成功加载；可重新选择关闭重试: %w", err)
		}
		return err == nil, err
	}
	live, err := service.liveDockerForwardConfig()
	if err != nil {
		return false, err
	}
	_, marked, err := configureDockerForwardMark(live, false)
	if err != nil || !marked {
		return false, err
	}
	if err := service.Store.Apply([]byte(updated)); err != nil {
		return false, fmt.Errorf("转发标记已从配置删除，但未成功加载；可重新选择关闭重试: %w", err)
	}
	return true, nil
}

func (service Service) prepareDockerForwardConfig() error {
	version, err := service.Store.Read()
	if err != nil {
		return err
	}
	if !version.Exists || !service.HasManagedConfig(string(version.Content)) || !strings.Contains(string(version.Content), "hook forward") {
		return fmt.Errorf("请先在防火墙管理中选择 d 生成本工具防火墙配置，再开启 Docker 放行标记")
	}
	updated, changed, err := MarkForwardAccepts(string(version.Content))
	if err != nil {
		return err
	}
	if !strings.Contains(updated, MarkExpression) {
		return fmt.Errorf("本工具防火墙配置没有可设置标记的转发规则")
	}
	if changed {
		saved, err := service.Store.SaveAndApply(version, updated)
		if err != nil && saved {
			err = fmt.Errorf("转发标记已保存，但未成功加载；可重新选择开启重试: %w", err)
		}
		return err
	}
	if err := service.Store.Apply([]byte(updated)); err != nil {
		return fmt.Errorf("转发标记已保存，但未成功加载；可重新选择开启重试: %w", err)
	}
	return nil
}

// Existing installations can still have the broad connection-state mark.
func (service Service) migrateDockerForwardConfig() error {
	version, err := service.Store.Read()
	if err != nil || !version.Exists {
		return err
	}
	content := string(version.Content)
	if !service.HasManagedConfig(content) || !strings.Contains(content, MarkExpression) {
		return nil
	}
	updated, changed, err := MarkForwardAccepts(content)
	if err != nil || !changed {
		return err
	}
	_, err = service.Store.SaveAndApply(version, updated)
	return err
}

func (service Service) enableDockerCompat() error {
	release, err := lockDockerForward()
	if err != nil {
		return err
	}
	defer release()
	if err := service.RequireDocker(); err != nil {
		return err
	}
	if !service.CommandExists("iptables") || !service.CommandExists("systemctl") {
		return fmt.Errorf("Docker 放行标记需要 iptables 和 systemctl")
	}
	backend, backendErr := service.dockerFirewallBackend()
	if backend != "" && backend != "iptables" {
		return fmt.Errorf("当前 Docker 防火墙后端是 %s，无法使用 DOCKER-USER", backend)
	}
	attached, err := service.dockerUserAttached("iptables")
	if err != nil {
		return err
	}
	if !attached && backend != "iptables" {
		state, _ := service.Store.CommandOutput("systemctl", "is-active", "docker.service")
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
	if err := service.prepareDockerForwardConfig(); err != nil {
		return err
	}
	if err := service.installDockerForwardUnit(); err != nil {
		return err
	}
	return service.syncDockerUserRules()
}

func (service Service) disableDockerCompat() (bool, error) {
	release, err := lockDockerForward()
	if err != nil {
		return false, err
	}
	defer release()
	removed := false
	// Stop any running boot helper before removing rules it could reinstall.
	hasUnit := fileExists(service.UnitPath)
	if hasUnit {
		if err := service.stopDockerForwardUnit(); err != nil {
			return false, err
		}
	}
	for _, command := range []string{"iptables", "ip6tables"} {
		done, err := service.removeDockerUserAccept(command)
		if err != nil {
			return removed, err
		}
		removed = removed || done
	}
	cleared, err := service.clearDockerForwardMarks()
	if err != nil || !hasUnit {
		return removed || cleared, err
	}
	return true, service.removeDockerForwardUnit()
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

func (service Service) Run(view *ui.UI) error {
	if !service.IsRoot() {
		return fmt.Errorf("设置 Docker 放行标记需要 root 权限，请使用 sudo 运行本工具")
	}
	return run(view, Actions{
		State:         service.StateField,
		RequireDocker: service.RequireDocker,
		Enable: func() (bool, error) {
			err := service.enableDockerCompat()
			if errors.Is(err, errDockerUserMissing) {
				return true, nil
			}
			return false, err
		},
		Disable: service.disableDockerCompat,
		Unit:    dockerForwardUnit,
		Mark:    dockerForwardMark,
	})
}
