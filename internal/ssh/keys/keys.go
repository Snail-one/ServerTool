package keys

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"snail_tool/internal/input"
	"snail_tool/internal/log"
	"snail_tool/internal/storage"
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
	pubkeyEditors   = []string{"vim", "vi", "nano"}
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

func Run(view *ui.UI) error {
	return ConfigureSSH(view)
}

func ConfigureSSH(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

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

	log.Warn("一键初始化必须先添加至少一把有效 SSH 公钥")
	for {
		if IsConfigured(account) {
			return nil
		}

		ui.MenuSection("请选择添加方式")
		ui.MenuOption("1", "粘贴公钥")
		ui.MenuOptionHint("2", "使用编辑器打开", "自动选择 vim / vi / nano")
		ui.MenuExit("0/q", "返回")

		choice, err := view.Ask("请选择：")
		if err != nil {
			return err
		}
		fmt.Println()
		if ui.IsReturnChoice(choice) {
			log.Info("已取消一键初始化")
			return ui.ErrReturnToMenu
		}

		switch strings.ToLower(choice) {
		case "1":
			if err := pasteRequiredSSHAuthorizedKey(view, account); err != nil {
				return err
			}
		case "2":
			if _, err := openAuthorizedKeysWithPreferredEditor(view, account); err != nil {
				if errors.Is(err, ui.ErrReturnToMenu) {
					continue
				}
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
		pubkey, err := view.Ask("请粘贴 SSH 公钥（必填，0/q 返回）：")
		if err != nil {
			return err
		}
		if ui.IsReturnChoice(pubkey) {
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
	authKeys := authorizedKeysPath(account)
	data, err := os.ReadFile(authKeys)
	return err == nil && len(authorizedKeyEntries(string(data))) > 0
}

func configureSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	choices := map[string]string{"1": "add", "2": "replace", "3": "edit", "4": "delete"}
	for {
		ui.ClearScreen()
		authKeys := authorizedKeysPath(account)
		data, err := os.ReadFile(authKeys)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("读取 SSH 公钥文件失败 %s: %w", authKeys, err)
		}
		entries := authorizedKeyEntries(string(data))
		draw := func() {
			ui.MenuTitle("SSH 管理", "SSH 公钥")
			ui.PrintInfoCard("公钥配置",
				ui.CardField{Label: "配置用户", Value: account.Name},
				ui.CardField{Label: "配置文件", Value: authKeys},
				ui.CardField{Label: "公钥数量", Value: fmt.Sprintf("%d 把", len(entries))},
			)
			fmt.Println()
			if len(entries) > 0 {
				printAuthorizedKeyEntries(entries)
			}
			ui.MenuSection("公钥操作")
			ui.MenuOptionHint("1", "添加公钥", "每行粘贴一把，回车结束")
			hint := "按编号选择"
			if len(entries) == 0 {
				hint = "暂无公钥"
			}
			ui.MenuOptionHint("2", "替换公钥", hint)
			ui.MenuOptionHint("3", "编辑文件", "自动选择 vim / vi / nano")
			ui.MenuOptionHint("4", "删除公钥", hint)
			ui.MenuExit("0/q", "返回 SSH 管理")
		}

		choice, err := ui.AskMenuChoice(view, "请选择", "", choices, draw)
		if err != nil {
			return err
		}
		ui.RunAction(view, "公钥操作失败，已返回 SSH 公钥管理", func() error {
			switch choice {
			case "add":
				return addSSHAuthorizedKeys(view, account)
			case "replace":
				return replaceSSHAuthorizedKey(view, account)
			case "edit":
				_, err := openAuthorizedKeysWithPreferredEditor(view, account)
				return err
			case "delete":
				return deleteSSHAuthorizedKeys(view, account)
			}
			return nil
		})
	}
}

func authorizedKeysPath(account *system.Account) string {
	return filepath.Join(account.Home, ".ssh", "authorized_keys")
}

func replaceSSHAuthorizedKey(view *ui.UI, account *system.Account) error {
	authKeys := authorizedKeysPath(account)
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

	ui.ClearScreen()
	ui.MenuTitle("SSH 管理", "SSH 公钥", "替换公钥")
	printAuthorizedKeyEntries(entries)
selectKey:
	for {
		rawSelection, err := view.Ask("请选择要替换的公钥编号（回车或 0/q 返回）：")
		if err != nil {
			return err
		}
		if strings.TrimSpace(rawSelection) == "" || ui.IsReturnChoice(rawSelection) {
			return ui.ErrReturnToMenu
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
			pubkey, err := view.Ask("请粘贴新的 SSH 公钥（回车或 0/q 返回）：")
			if err != nil {
				return err
			}
			if strings.TrimSpace(pubkey) == "" || ui.IsReturnChoice(pubkey) {
				continue selectKey
			}
			if err := system.ValidateSSHPublicKey(pubkey); err != nil {
				log.Warn(err)
				continue
			}
			originalOptions, _, _, _ := authorizedKeyParts(entry.line)
			newOptions, _, _, _ := authorizedKeyParts(pubkey)
			keptOptions := originalOptions != "" && newOptions == ""
			if keptOptions {
				pubkey = originalOptions + " " + pubkey
			}
			if strings.TrimSpace(pubkey) == strings.TrimSpace(entry.line) {
				log.Info("公钥未变化")
				return nil
			}
			if storage.ContainsLine(content, pubkey) {
				log.Warn("该 SSH 公钥已存在")
				continue
			}

			fields := []ui.CardField{
				{Label: "编号", Value: strconv.Itoa(entry.index)},
				{Label: "原公钥", Value: summarizeAuthorizedKey(entry.line)},
				{Label: "新公钥", Value: summarizeAuthorizedKey(pubkey), Changed: true},
			}
			if keptOptions {
				fields = append(fields, ui.CardField{Label: "登录选项", Value: "沿用原公钥设置"})
			} else if originalOptions != newOptions {
				fields = append(fields, ui.CardField{Label: "登录选项", Value: "采用新公钥附带的设置", Changed: true})
			}
			ui.PrintInfoCard("确认替换 SSH 公钥", fields...)

			confirmed, err := view.Confirm("确认替换选中的 SSH 公钥？(y/N，0/q 返回粘贴)：")
			if errors.Is(err, ui.ErrReturnToMenu) {
				continue
			}
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("已取消修改")
				continue selectKey
			}

			updated := replaceAuthorizedKeyIndex(content, index, pubkey)
			if err := checkAuthorizedKeysVersion(authKeys, content); err != nil {
				return err
			}
			if err := storage.AtomicWriteFile(authKeys, []byte(updated), storage.AtomicWriteOptions{Mode: 0600, ForceMode: true}); err != nil {
				return err
			}

			ui.PrintSuccessCard("SSH 公钥修改完成",
				ui.CardField{Label: "编号", Value: strconv.Itoa(entry.index)},
				ui.CardField{Label: "原公钥", Value: summarizeAuthorizedKey(entry.line)},
				ui.CardField{Label: "新公钥", Value: summarizeAuthorizedKey(pubkey), Changed: true},
				ui.CardField{Label: "配置文件", Value: authKeys},
			)
			return nil
		}
	}
}

func addSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	ui.ClearScreen()
	ui.MenuTitle("SSH 管理", "SSH 公钥", "添加公钥")
	for {
		pubkey, err := view.Ask("请粘贴 SSH 公钥（回车或 0/q 结束）：")
		if err != nil {
			return err
		}
		if strings.TrimSpace(pubkey) == "" || ui.IsReturnChoice(pubkey) {
			return ui.ErrReturnToMenu
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
	authKeys := authorizedKeysPath(account)
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

	ui.ClearScreen()
	ui.MenuTitle("SSH 管理", "SSH 公钥", "删除公钥")
	printAuthorizedKeyEntries(entries)
	for {
		rawSelection, err := view.Ask("请选择要删除的公钥编号（可用逗号或空格多选，回车或 0/q 返回）：")
		if err != nil {
			return err
		}
		if strings.TrimSpace(rawSelection) == "" || ui.IsReturnChoice(rawSelection) {
			return ui.ErrReturnToMenu
		}

		indexes, err := parseAuthorizedKeySelection(rawSelection, len(entries))
		if err != nil {
			log.Warn(err)
			continue
		}

		remaining := len(entries) - len(indexes)
		fields := make([]ui.CardField, 0, len(indexes)+1)
		for _, index := range indexes {
			entry := entries[index-1]
			fields = append(fields, ui.CardField{Label: "公钥 " + strconv.Itoa(index), Value: summarizeAuthorizedKey(entry.line)})
		}
		if remaining == 0 {
			fields = append(fields, ui.CardField{Label: "删除后", Value: "此文件将没有可识别的 SSH 公钥", Changed: true})
			ui.PrintWarningCard("即将删除此文件中的全部公钥", fields...)
		} else {
			fields = append(fields, ui.CardField{Label: "保留数量", Value: fmt.Sprintf("%d 把", remaining)})
			ui.PrintInfoCard("确认删除 SSH 公钥", fields...)
		}

		confirmed, err := view.Confirm("确认删除选中的 SSH 公钥？(y/N，0/q 返回选择)：")
		if errors.Is(err, ui.ErrReturnToMenu) {
			continue
		}
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("已取消删除")
			continue
		}

		selected := make(map[int]struct{}, len(indexes))
		for _, index := range indexes {
			selected[index] = struct{}{}
		}
		if err := removeSelectedAuthorizedKeys(authKeys, content, selected); err != nil {
			return err
		}

		ui.PrintSuccessCard("SSH 公钥删除完成",
			ui.CardField{Label: "删除数量", Value: fmt.Sprintf("%d 把", len(indexes))},
			ui.CardField{Label: "配置文件", Value: authKeys},
		)
		return nil
	}
}

func removeSelectedAuthorizedKeys(path, original string, selected map[int]struct{}) error {
	if err := checkAuthorizedKeysVersion(path, original); err != nil {
		return err
	}
	cleaned := removeAuthorizedKeyIndexes(original, selected)
	return storage.AtomicWriteFile(path, []byte(cleaned), storage.AtomicWriteOptions{Mode: 0600, ForceMode: true})
}

func checkAuthorizedKeysVersion(path, original string) error {
	if _, err := storage.RemoveManagedBlockChecked(original, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd); err != nil {
		return err
	}
	latest, exists, err := storage.ReadRegularFile(path)
	if err != nil {
		return err
	}
	if !exists || string(latest) != original {
		return fmt.Errorf("SSH 公钥文件在选择期间已变化，请重新选择")
	}
	return nil
}

func printAuthorizedKeys(account *system.Account) error {
	authKeys := authorizedKeysPath(account)
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

func openAuthorizedKeysWithPreferredEditor(view *ui.UI, account *system.Account) (bool, error) {
	for _, editor := range pubkeyEditors {
		if editorAvailable(editor) {
			return true, openAuthorizedKeysWithEditor(view, account, editor)
		}
	}
	log.Warn("未找到 vim、vi、nano 中的任意工具，请先安装其中一个编辑器")
	return false, nil
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
			return ui.ErrReturnToMenu
		}
		return fmt.Errorf("打开 %s 失败: %w", editor, err)
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
		log.Warn("无效 SSH 公钥：", truncateString(safeKeyText(line), 80))
	}
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
	if err := storage.EnsureFileWithOptions(authKeys, storage.AtomicWriteOptions{
		Mode: 0600, Owner: &storage.FileOwner{UID: account.UID, GID: account.GID},
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
	if !storage.ContainsLine(string(data), pubkey) {
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
	cleaned, err := storage.RemoveManagedBlockChecked(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if err != nil {
		return err
	}
	keys := managedAuthorizedKeys(content)
	keys = append(keys, pubkey)

	block := storage.FormatManagedBlock(sshAuthorizedKeysBegin, strings.Join(keys, "\n"), sshAuthorizedKeysEnd)
	return storage.AtomicWriteFile(path, []byte(storage.AppendBlock(cleaned, block)), storage.AtomicWriteOptions{Mode: 0600, ForceMode: true})
}

func managedAuthorizedKeys(content string) []string {
	block, ok := storage.ManagedBlockContent(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
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
		return 0, fmt.Errorf("选择替换一次只能选择一个编号")
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
	if err := input.Text(raw); err != nil {
		return nil, err
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("未选择 SSH 公钥编号")
	}

	indexes := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		index, err := input.Number(part, 1, max)
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
	return storage.NormalizeCleanedContent(cleaned)
}

func removeEmptyManagedAuthorizedKeyBlock(content string) string {
	block, ok := storage.ManagedBlockContent(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	if ok && len(authorizedKeyEntries(block)) == 0 {
		return storage.RemoveManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	}
	return content
}
