package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

type authorizedKeyEntry struct {
	index   int
	line    string
	managed bool
}

const (
	sshAuthorizedKeysBegin = "# ===== BEGIN SNAIL SSH AUTHORIZED KEYS ====="
	sshAuthorizedKeysEnd   = "# ===== END SNAIL SSH AUTHORIZED KEYS ====="
)

var (
	runEditor       = system.Run
	editorAvailable = system.CommandExists
	pubkeyEditors   = []string{"vim", "nano", "vi"}
)

var authorizedKeyTypes = map[string]struct{}{
	"ssh-ed25519":                              {},
	"ssh-rsa":                                  {},
	"ecdsa-sha2-nistp256":                      {},
	"ecdsa-sha2-nistp384":                      {},
	"ecdsa-sha2-nistp521":                      {},
	"sk-ssh-ed25519@openssh.com":               {},
	"sk-ecdsa-sha2-nistp256@openssh.com":       {},
	"ssh-ed25519-cert-v01@openssh.com":         {},
	"ssh-rsa-cert-v01@openssh.com":             {},
	"ecdsa-sha2-nistp256-cert-v01@openssh.com": {},
	"ecdsa-sha2-nistp384-cert-v01@openssh.com": {},
	"ecdsa-sha2-nistp521-cert-v01@openssh.com": {},
}

func Markers() (string, string) {
	return sshAuthorizedKeysBegin, sshAuthorizedKeysEnd
}

func Run(view *ui.UI) error {
	return ConfigureSSH(view)
}

func ConfigureSSH(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	log.Info("当前配置用户：", account.Name)
	fmt.Println()
	return configureSSHAuthorizedKeys(view, account)
}

// EnsureSSHAuthorizedKeys requires at least one authorized key before a setup
// workflow is allowed to continue to SSH hardening.
func EnsureSSHAuthorizedKeys(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	return ensureSSHAuthorizedKeys(view, account)
}

func ensureSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	log.Info("当前配置用户：", account.Name)
	if err := printAuthorizedKeys(account); err != nil {
		return err
	}
	fmt.Println()

	if IsConfigured(account) {
		log.Info("已存在 SSH 公钥，可安全进入下一步")
		return nil
	}

	log.Warn("一键配置必须先添加至少一把有效 SSH 公钥")
	for {
		if IsConfigured(account) {
			return nil
		}

		ui.MenuSection("请选择添加方式")
		ui.MenuOption("1", "粘贴公钥")
		ui.MenuOptionHint("2", "使用编辑器打开", "vim / nano / vi")
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if shared.IsReturnChoice(choice) {
			log.Info("已取消一键配置")
			return shared.ErrReturnToMenu
		}

		switch strings.ToLower(choice) {
		case "1":
			if err := pasteRequiredSSHAuthorizedKey(view, account); err != nil {
				return err
			}
		case "2":
			if _, err := openAuthorizedKeysWithChosenEditor(view, account); err != nil {
				return err
			}
			if !IsConfigured(account) {
				log.Warn("仍未检测到有效 SSH 公钥")
			}
		default:
			ui.InvalidChoice()
		}
		fmt.Println()
	}
}

func pasteRequiredSSHAuthorizedKey(view *ui.UI, account *system.Account) error {
	for {
		pubkey, err := view.Ask("请粘贴 SSH 公钥（必填，输入 q 返回）：")
		if err != nil {
			return err
		}
		if shared.IsReturnChoice(pubkey) {
			return nil
		}
		if strings.TrimSpace(pubkey) == "" {
			log.Warn("SSH 公钥不能为空；未添加公钥不会继续配置 SSH 安全策略")
			continue
		}
		if err := system.ValidateSSHPublicKey(pubkey); err != nil {
			log.Warn(err)
			continue
		}
		if err := installAuthorizedKey(account, pubkey); err != nil {
			return err
		}
		if !IsConfigured(account) {
			return fmt.Errorf("SSH 公钥写入后校验失败")
		}
		return nil
	}
}

func IsConfigured(account *system.Account) bool {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	data, err := os.ReadFile(authKeys)
	return err == nil && len(authorizedKeyEntries(string(data))) > 0
}

func configureSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("SSH 管理", "SSH 公钥")
		if err := printAuthorizedKeys(account); err != nil {
			return err
		}

		ui.MenuSection("请选择公钥操作")
		ui.MenuOption("1", "添加公钥")
		ui.MenuOptionHint("2", "修改公钥", "按编号替换或用编辑器打开")
		ui.MenuOption("3", "删除公钥")
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()

		if shared.IsReturnChoice(choice) {
			return shared.ErrReturnToMenu
		}
		switch strings.ToLower(choice) {
		case "1":
			if err := addSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		case "2":
			if err := editSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		case "3":
			if err := deleteSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		default:
			ui.InvalidChoice()
			view.Pause()
		}
		fmt.Println()
	}
}

func editSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		ui.ClearScreen()
		ui.MenuTitle("SSH 管理", "SSH 公钥", "修改公钥")
		if err := printAuthorizedKeys(account); err != nil {
			return err
		}

		ui.MenuSection("请选择修改方式")
		ui.MenuOption("1", "按编号替换公钥")
		ui.MenuOptionHint("2", "使用编辑器打开", "vim / nano / vi")
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if shared.IsReturnChoice(choice) {
			return nil
		}

		switch strings.ToLower(choice) {
		case "1":
			return replaceSSHAuthorizedKey(view, account)
		case "2":
			opened, err := openAuthorizedKeysWithChosenEditor(view, account)
			if err != nil {
				return err
			}
			if opened {
				return nil
			}
		default:
			ui.InvalidChoice()
			view.Pause()
		}
	}
}

func replaceSSHAuthorizedKey(view *ui.UI, account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		log.Info("未发现 SSH authorized_keys，跳过")
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	content := string(data)
	entries := authorizedKeyEntries(content)
	if len(entries) == 0 {
		log.Info("未发现可修改的 SSH 公钥")
		return nil
	}

	printAuthorizedKeyEntries(entries)
	for {
		rawSelection, err := view.Ask("请输入要修改的编号（直接回车返回）：")
		if err != nil {
			return err
		}
		if strings.TrimSpace(rawSelection) == "" {
			return nil
		}

		index, err := parseSingleAuthorizedKeyIndex(rawSelection, len(entries))
		if err != nil {
			log.Warn(err)
			continue
		}

		entry := entries[index-1]
		fmt.Println()
		fmt.Println(ui.PrimaryBoldText("当前公钥："))
		fmt.Println(summarizeAuthorizedKey(entry.line))
		fmt.Println()

		for {
			pubkey, err := view.Ask("请粘贴新的 SSH 公钥（直接回车返回）：")
			if err != nil {
				return err
			}
			if strings.TrimSpace(pubkey) == "" {
				return nil
			}
			if err := system.ValidateSSHPublicKey(pubkey); err != nil {
				log.Warn(err)
				continue
			}
			if strings.TrimSpace(pubkey) == strings.TrimSpace(entry.line) {
				log.Info("公钥未变化")
				return nil
			}
			if shared.ContainsLine(content, pubkey) {
				log.Warn("该 SSH 公钥已存在")
				continue
			}

			fmt.Println()
			fmt.Println("即将替换：")
			fmt.Printf("%d) %s\n", entry.index, summarizeAuthorizedKey(entry.line))
			fmt.Println("替换为：")
			fmt.Println(summarizeAuthorizedKey(pubkey))
			fmt.Println()

			confirmed, err := view.Confirm("确认替换选中的 SSH 公钥？(y/N)：")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("已取消修改")
				return nil
			}

			updated := replaceAuthorizedKeyIndex(content, index, pubkey)
			if err := shared.AtomicWriteFile(authKeys, []byte(updated), shared.AtomicWriteOptions{Mode: 0600, ForceMode: true}); err != nil {
				return err
			}

			ui.PrintSuccessCard("SSH 公钥修改完成",
				ui.CardField{Label: "编号", Value: strconv.Itoa(entry.index)},
				ui.CardField{Label: "原公钥", Value: summarizeAuthorizedKey(entry.line)},
				ui.CardField{Label: "新公钥", Value: summarizeAuthorizedKey(pubkey)},
				ui.CardField{Label: "配置文件", Value: authKeys},
			)
			return nil
		}
	}
}

func addSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		pubkey, err := view.Ask("请粘贴 SSH 公钥（直接回车结束）：")
		if err != nil {
			return err
		}
		if strings.TrimSpace(pubkey) == "" {
			return nil
		}
		if err := system.ValidateSSHPublicKey(pubkey); err != nil {
			log.Warn(err)
			continue
		}

		if err := installAuthorizedKey(account, pubkey); err != nil {
			return err
		}
		fmt.Println()
	}
}

func deleteSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		log.Info("未发现 SSH authorized_keys，跳过")
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	content := string(data)
	entries := authorizedKeyEntries(content)
	if len(entries) == 0 {
		log.Info("未发现可删除的 SSH 公钥")
		return nil
	}

	printAuthorizedKeyEntries(entries)
	for {
		rawSelection, err := view.Ask("请输入要删除的编号（多个用逗号或空格分隔，直接回车返回）：")
		if err != nil {
			return err
		}
		if strings.TrimSpace(rawSelection) == "" {
			return nil
		}

		indexes, err := parseAuthorizedKeySelection(rawSelection, len(entries))
		if err != nil {
			log.Warn(err)
			continue
		}

		fmt.Println()
		fmt.Println("即将删除以下 SSH 公钥：")
		for _, index := range indexes {
			entry := entries[index-1]
			fmt.Printf("%d) %s\n", entry.index, summarizeAuthorizedKey(entry.line))
		}
		fmt.Println()

		confirmed, err := view.Confirm("确认删除选中的 SSH 公钥？(y/N)：")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("已取消删除")
			return nil
		}

		selected := make(map[int]struct{}, len(indexes))
		for _, index := range indexes {
			selected[index] = struct{}{}
		}
		cleaned := removeAuthorizedKeyIndexes(content, selected)
		if err := shared.AtomicWriteFile(authKeys, []byte(cleaned), shared.AtomicWriteOptions{Mode: 0600, ForceMode: true}); err != nil {
			return err
		}

		ui.PrintSuccessCard("SSH 公钥删除完成",
			ui.CardField{Label: "删除数量", Value: fmt.Sprintf("%d 把", len(indexes))},
			ui.CardField{Label: "配置文件", Value: authKeys},
		)
		return nil
	}
}

func printAuthorizedKeys(account *system.Account) error {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	if !system.FileExists(authKeys) {
		log.Info("未发现 SSH authorized_keys")
		return nil
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}

	entries := authorizedKeyEntries(string(data))
	if len(entries) == 0 {
		log.Info("未发现 SSH 公钥")
		return nil
	}

	printAuthorizedKeyEntries(entries)
	return nil
}

func openAuthorizedKeysWithChosenEditor(view *ui.UI, account *system.Account) (bool, error) {
	editor, err := chooseAuthorizedKeyEditor(view)
	if err != nil || editor == "" {
		return false, err
	}
	return true, openAuthorizedKeysWithEditor(view, account, editor)
}

func chooseAuthorizedKeyEditor(view *ui.UI) (string, error) {
	for {
		ui.MenuSection("请选择编辑器")
		for index, editor := range pubkeyEditors {
			ui.MenuOptionHint(strconv.Itoa(index+1), editor, editorChoiceHint(editor, index == 0))
		}
		ui.MenuExit("0/q", "返回")
		fmt.Println()

		raw, err := view.Ask("请选择（直接回车默认 1）：")
		if err != nil {
			return "", err
		}
		fmt.Println()
		if shared.IsReturnChoice(raw) {
			return "", nil
		}

		editor, ok := editorFromChoice(raw)
		if !ok {
			ui.InvalidChoice()
			fmt.Println()
			continue
		}
		if !editorAvailable(editor) {
			log.Warn("未找到 ", editor, "，请先安装或改用其他编辑器")
			fmt.Println()
			continue
		}
		return editor, nil
	}
}

func editorFromChoice(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "1":
		return "vim", true
	case "2":
		return "nano", true
	case "3":
		return "vi", true
	default:
		return "", false
	}
}

func editorChoiceHint(editor string, def bool) string {
	switch {
	case def && !editorAvailable(editor):
		return "默认，未安装"
	case def:
		return "默认"
	case !editorAvailable(editor):
		return "未安装"
	default:
		return ""
	}
}

func openAuthorizedKeysWithEditor(view *ui.UI, account *system.Account, editor string) error {
	if !editorAvailable(editor) {
		log.Warn("未找到 ", editor, "，请先安装或改用其他方式")
		return nil
	}

	authKeys, err := ensureAuthorizedKeysFile(account)
	if err != nil {
		return err
	}

	log.Info("正在使用 ", editor, " 打开：", authKeys)
	if err := runEditor(editor, authKeys); err != nil {
		if system.IsInterrupted(err) {
			log.Info("已取消编辑")
			return nil
		}
		log.Warn("打开 ", editor, " 失败：", err)
		return nil
	}

	if err := secureAuthorizedKeysFile(account, authKeys); err != nil {
		return err
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}
	invalid := unrecognizedAuthorizedKeyLines(string(data))
	if len(invalid) == 0 {
		return nil
	}
	for _, line := range invalid {
		log.Warn("无效 SSH 公钥：", truncateString(line, 80))
	}
	view.Pause()
	return nil
}

func unrecognizedAuthorizedKeyLines(content string) []string {
	var lines []string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := system.ValidateSSHPublicKey(line); err != nil {
			lines = append(lines, line)
		}
	}
	return lines
}

func ensureAuthorizedKeysFile(account *system.Account) (string, error) {
	sshDir := filepath.Join(account.Home, ".ssh")
	authKeys := filepath.Join(sshDir, "authorized_keys")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", err
	}
	if err := shared.EnsureFileWithOptions(authKeys, shared.AtomicWriteOptions{
		Mode: 0600, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}); err != nil {
		return "", err
	}
	return authKeys, nil
}

func secureAuthorizedKeysFile(account *system.Account, authKeys string) error {
	sshDir := filepath.Dir(authKeys)
	if err := os.Chmod(sshDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(authKeys, 0600); err != nil {
		return err
	}
	return system.ChownPath(sshDir, account, true)
}

func installAuthorizedKey(account *system.Account, pubkey string) error {
	authKeys, err := ensureAuthorizedKeysFile(account)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(authKeys)
	if err != nil {
		return err
	}
	if !shared.ContainsLine(string(data), pubkey) {
		if err := writeManagedAuthorizedKey(authKeys, string(data), pubkey); err != nil {
			return err
		}
		log.Info("已添加 SSH 公钥")
	} else {
		log.Info("SSH 公钥已存在，跳过添加")
	}

	return secureAuthorizedKeysFile(account, authKeys)
}

func writeManagedAuthorizedKey(path, content, pubkey string) error {
	keys := managedAuthorizedKeys(content)
	keys = append(keys, pubkey)

	cleaned := shared.RemoveManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	block := shared.FormatManagedBlock(sshAuthorizedKeysBegin, strings.Join(keys, "\n"), sshAuthorizedKeysEnd)
	return shared.AtomicWriteFile(path, []byte(shared.AppendBlock(cleaned, block)), shared.AtomicWriteOptions{Mode: 0600, ForceMode: true})
}

func managedAuthorizedKeys(content string) []string {
	block, ok := shared.ManagedBlockContent(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if !ok {
		return nil
	}

	var keys []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}

func authorizedKeyEntries(content string) []authorizedKeyEntry {
	var entries []authorizedKeyEntry
	managed := false
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case sshAuthorizedKeysBegin:
			managed = true
			continue
		case sshAuthorizedKeysEnd:
			managed = false
			continue
		}
		if !isAuthorizedKeyLine(trimmed) {
			continue
		}
		entries = append(entries, authorizedKeyEntry{
			index:   len(entries) + 1,
			line:    line,
			managed: managed,
		})
	}
	return entries
}

func printAuthorizedKeyEntries(entries []authorizedKeyEntry) {
	fmt.Println(ui.PrimaryBoldText("当前 SSH 公钥："))
	for _, entry := range entries {
		source := "手动"
		if entry.managed {
			source = "本工具"
		}
		ui.MenuOption(strconv.Itoa(entry.index), fmt.Sprintf("[%s] %s", source, summarizeAuthorizedKey(entry.line)))
	}
	fmt.Println()
}

func parseSingleAuthorizedKeyIndex(raw string, max int) (int, error) {
	indexes, err := parseAuthorizedKeySelection(raw, max)
	if err != nil {
		return 0, err
	}
	if len(indexes) != 1 {
		return 0, fmt.Errorf("修改公钥一次只能选择一个编号")
	}
	return indexes[0], nil
}

func replaceAuthorizedKeyIndex(content string, index int, pubkey string) string {
	lines := strings.SplitAfter(content, "\n")
	var builder strings.Builder
	current := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if isAuthorizedKeyLine(trimmed) {
			current++
			if current == index {
				newline := ""
				switch {
				case strings.HasSuffix(line, "\r\n"):
					newline = "\r\n"
				case strings.HasSuffix(line, "\n"):
					newline = "\n"
				}
				builder.WriteString(pubkey)
				builder.WriteString(newline)
				continue
			}
		}
		builder.WriteString(line)
	}
	return builder.String()
}

func parseAuthorizedKeySelection(raw string, max int) ([]int, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("未选择 SSH 公钥编号")
	}

	indexes := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		index, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("无效公钥编号: %s", part)
		}
		if index < 1 || index > max {
			return nil, fmt.Errorf("公钥编号超出范围: %d", index)
		}
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		indexes = append(indexes, index)
	}
	return indexes, nil
}

func removeAuthorizedKeyIndexes(content string, selected map[int]struct{}) string {
	lines := strings.SplitAfter(content, "\n")
	var builder strings.Builder
	index := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if isAuthorizedKeyLine(trimmed) {
			index++
			if _, ok := selected[index]; ok {
				continue
			}
		}
		builder.WriteString(line)
	}

	cleaned := removeEmptyManagedAuthorizedKeyBlock(builder.String())
	return shared.NormalizeCleanedContent(cleaned)
}

func removeEmptyManagedAuthorizedKeyBlock(content string) string {
	block, ok := shared.ManagedBlockContent(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if ok && len(authorizedKeyEntries(block)) == 0 {
		return shared.RemoveManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	}
	return content
}

func isAuthorizedKeyLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	fields := strings.Fields(trimmed)
	for index, field := range fields {
		if _, ok := authorizedKeyTypes[field]; ok && index+1 < len(fields) && fields[index+1] != "" {
			return true
		}
	}
	return false
}

func summarizeAuthorizedKey(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return truncateString(strings.TrimSpace(line), 90)
	}

	keyBody := fields[1]
	if len(keyBody) > 24 {
		keyBody = keyBody[:16] + "..." + keyBody[len(keyBody)-8:]
	}

	summary := fields[0] + " " + keyBody
	if len(fields) > 2 {
		summary += " " + strings.Join(fields[2:], " ")
	}
	return truncateString(summary, 120)
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}
