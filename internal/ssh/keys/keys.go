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

func IsConfigured(account *system.Account) bool {
	authKeys := filepath.Join(account.Home, ".ssh", "authorized_keys")
	return shared.FileContainsNonEmptyContent(authKeys)
}

func configureSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		if err := printAuthorizedKeys(account); err != nil {
			return err
		}

		fmt.Println("请选择公钥操作：")
		fmt.Println("1) 添加公钥")
		fmt.Println("2) 删除公钥")
		fmt.Println("0/q) 返回")
		fmt.Println()

		choice, err := view.Ask("输入选项: ")
		if err != nil {
			return err
		}
		fmt.Println()

		switch strings.ToLower(choice) {
		case "1":
			if err := addSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		case "2":
			if err := deleteSSHAuthorizedKeys(view, account); err != nil {
				return err
			}
		case "0", "q", "exit":
			return shared.ErrReturnToMenu
		default:
			fmt.Println("无效选项，请重新输入")
		}
		fmt.Println()
	}
}

func addSSHAuthorizedKeys(view *ui.UI, account *system.Account) error {
	for {
		pubkey, err := view.Ask("请粘贴 SSH 公钥（直接回车结束）: ")
		if err != nil {
			return err
		}
		if strings.TrimSpace(pubkey) == "" {
			return nil
		}
		if err := system.ValidateSSHPublicKey(pubkey); err != nil {
			return err
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
	rawSelection, err := view.Ask("请输入要删除的编号（多个用逗号或空格分隔，直接回车返回）: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(rawSelection) == "" {
		return nil
	}

	indexes, err := parseAuthorizedKeySelection(rawSelection, len(entries))
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("即将删除以下 SSH 公钥：")
	for _, index := range indexes {
		entry := entries[index-1]
		fmt.Printf("%d) %s\n", entry.index, summarizeAuthorizedKey(entry.line))
	}
	fmt.Println()

	confirmed, err := view.Confirm("确认删除选中的 SSH 公钥？(y/N): ")
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
	if err := os.WriteFile(authKeys, []byte(cleaned), 0600); err != nil {
		return err
	}
	if err := os.Chmod(authKeys, 0600); err != nil {
		return err
	}
	if err := system.ChownPath(authKeys, account, false); err != nil {
		return err
	}

	log.Info("已删除选中的 SSH 公钥")
	return nil
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

func installAuthorizedKey(account *system.Account, pubkey string) error {
	sshDir := filepath.Join(account.Home, ".ssh")
	authKeys := filepath.Join(sshDir, "authorized_keys")

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}
	if err := shared.EnsureFile(authKeys); err != nil {
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

	if err := os.Chmod(sshDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(authKeys, 0600); err != nil {
		return err
	}
	return system.ChownPath(sshDir, account, true)
}

func writeManagedAuthorizedKey(path, content, pubkey string) error {
	keys := managedAuthorizedKeys(content)
	keys = append(keys, pubkey)

	cleaned := shared.RemoveManagedBlock(content, sshAuthorizedKeysBegin, sshAuthorizedKeysEnd)
	block := fmt.Sprintf("%s\n%s\n%s\n", sshAuthorizedKeysBegin, strings.Join(keys, "\n"), sshAuthorizedKeysEnd)
	return os.WriteFile(path, []byte(shared.AppendBlock(cleaned, block)), 0600)
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
	fmt.Println("当前 SSH 公钥：")
	for _, entry := range entries {
		source := "手动"
		if entry.managed {
			source = "本工具"
		}
		fmt.Printf("%d) [%s] %s\n", entry.index, source, summarizeAuthorizedKey(entry.line))
	}
	fmt.Println()
}

func parseAuthorizedKeySelection(raw string, max int) ([]int, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("未选择要删除的 SSH 公钥编号")
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
	return true
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
