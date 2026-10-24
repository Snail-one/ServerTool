package clientproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var managedStore = shared.BackupStore{Root: shared.DataDir, Module: "docker"}

var proxyKeys = []string{"httpProxy", "httpsProxy", "noProxy"}

type promptReader interface {
	Ask(string) (string, error)
	Confirm(string) (bool, error)
}

type document struct {
	root, proxies, defaults map[string]json.RawMessage
}

// ConfigPath follows the invoking user's home, including when run with sudo.
// An explicit DOCKER_CONFIG takes precedence, as it does for the Docker CLI.
func ConfigPath(account *system.Account) (string, error) {
	dir := os.Getenv("DOCKER_CONFIG")
	if dir == "" {
		dir = filepath.Join(account.Home, ".docker")
	}
	return filepath.Abs(filepath.Join(dir, "config.json"))
}

func Run(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	return configure(view, account)
}

func configure(view promptReader, account *system.Account) error {
	path, err := ConfigPath(account)
	if err != nil {
		return err
	}
	doc, _, _, err := readDocument(path)
	if err != nil {
		return err
	}
	ui.PrintInfoCard("Docker 构建与容器代理",
		ui.CardField{Label: "目标用户", Value: account.Name},
		ui.CardField{Label: "配置文件", Value: path},
		ui.CardField{Label: "当前代理", Value: doc.summary()},
	)
	fmt.Println("保存后自动用于后续 docker build 和新建容器，无需重启 Docker。")
	fmt.Println("请使用构建环境和容器可访问的代理地址；127.0.0.1 通常指向容器自身。")
	fmt.Println("支持 ip:port 或 username:password@ip:port 格式。")
	raw, err := view.Ask("请输入构建与容器代理地址：")
	if err != nil {
		return err
	}
	if shared.IsReturnChoice(raw) {
		return shared.ErrReturnToMenu
	}
	proxyURL, err := commonproxy.NormalizeProxy(raw)
	if err != nil {
		return err
	}
	backup, err := writeConfig(path, account, proxyURL, func() (bool, error) {
		return view.Confirm("已存在默认 HTTP/HTTPS 代理或绕过列表，是否覆盖？(y/N)：")
	})
	if err != nil {
		return err
	}
	fields := []ui.CardField{
		{Label: "目标用户", Value: account.Name},
		{Label: "配置文件", Value: path},
		{Label: "代理地址", Value: commonproxy.MaskProxyURL(proxyURL)},
		{Label: "生效范围", Value: "后续构建及新建容器；无需重启 Docker"},
	}
	if backup != "" {
		fields = append(fields, ui.CardField{Label: "备份文件", Value: backup})
	}
	ui.PrintSuccessCard("Docker 构建与容器代理配置完成", fields...)
	return nil
}

func readObject(raw []byte, label string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("Docker 客户端配置 %s 不是有效 JSON 对象，未修改", label)
	}
	return object, nil
}

func readDocument(path string) (document, []byte, bool, error) {
	doc := document{root: map[string]json.RawMessage{}, proxies: map[string]json.RawMessage{}, defaults: map[string]json.RawMessage{}}
	data, exists, err := shared.ReadRegularFile(path)
	if err != nil || !exists {
		return doc, data, exists, err
	}
	doc.root, err = readObject(data, "config.json")
	if err == nil && doc.root["proxies"] != nil {
		doc.proxies, err = readObject(doc.root["proxies"], "proxies")
	}
	if err == nil && doc.proxies["default"] != nil {
		doc.defaults, err = readObject(doc.proxies["default"], "proxies.default")
	}
	return doc, data, exists, err
}

func (doc document) hasProxy() bool {
	for _, key := range proxyKeys {
		if _, ok := doc.defaults[key]; ok {
			return true
		}
	}
	return false
}

func (doc document) marshal() ([]byte, error) {
	// Each object consists only of validated RawMessages, so marshaling these
	// nested maps cannot fail. Keeping RawMessages preserves unrelated numbers.
	if len(doc.defaults) == 0 {
		delete(doc.proxies, "default")
	} else {
		doc.proxies["default"], _ = json.Marshal(doc.defaults)
	}
	if len(doc.proxies) == 0 {
		delete(doc.root, "proxies")
	} else {
		doc.root["proxies"], _ = json.Marshal(doc.proxies)
	}
	data, err := json.MarshalIndent(doc.root, "", "  ")
	return append(data, '\n'), err
}

func writeConfig(path string, account *system.Account, proxyURL string, confirm func() (bool, error)) (string, error) {
	doc, original, exists, err := readDocument(path)
	if err != nil {
		return "", err
	}
	if doc.hasProxy() {
		if confirm == nil {
			return "", fmt.Errorf("已有 Docker 客户端代理配置，未提供覆盖确认")
		}
		ok, err := confirm()
		if err != nil {
			return "", err
		}
		if !ok {
			return "", shared.ErrReturnToMenu
		}
	}
	for key, value := range map[string]string{
		"httpProxy": proxyURL, "httpsProxy": proxyURL, "noProxy": commonproxy.DefaultNoProxy(),
	} {
		doc.defaults[key], _ = json.Marshal(value)
	}
	data, err := doc.marshal()
	if err != nil {
		return "", err
	}
	if err := ensureDirectory(filepath.Dir(path), account); err != nil {
		return "", err
	}
	latest, latestExists, err := shared.ReadRegularFile(path)
	if err != nil {
		return "", err
	}
	if latestExists != exists || !bytes.Equal(latest, original) {
		return "", fmt.Errorf("Docker 客户端配置在操作期间发生变化，未覆盖，请重试: %s", path)
	}
	backup := ""
	if exists {
		backup, err = managedStore.Snapshot(path)
		if err != nil {
			return "", fmt.Errorf("备份 Docker 客户端配置失败: %w", err)
		}
	}
	if err := managedStore.RememberManaged(path, data); err != nil {
		return "", fmt.Errorf("记录 Docker 客户端代理配置归属失败，未写入配置: %w", err)
	}
	err = shared.AtomicWriteFile(path, data, shared.AtomicWriteOptions{
		Mode: 0600, ForceMode: true, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	})
	return backup, err
}

// Create only missing directories and assign them to the target user. Existing
// directories (including a custom DOCKER_CONFIG) retain their metadata.
func ensureDirectory(dir string, account *system.Account) error {
	var missing []string
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("Docker 配置目录不是普通目录: %s", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, current)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0700); err != nil {
			return err
		}
		if err := system.ChownPath(missing[i], account, false); err != nil {
			return err
		}
	}
	return nil
}

func CurrentSummary() string {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return "无法读取"
	}
	path, err := ConfigPath(account)
	if err != nil {
		return "无法读取"
	}
	doc, _, _, err := readDocument(path)
	if err != nil {
		return "无法读取"
	}
	return doc.summary()
}

func (doc document) summary() string {
	var fields []string
	for _, item := range []struct{ key, label string }{{"httpProxy", "HTTP"}, {"httpsProxy", "HTTPS"}} {
		var value string
		if json.Unmarshal(doc.defaults[item.key], &value) == nil && strings.TrimSpace(value) != "" {
			fields = append(fields, item.label+" "+commonproxy.MaskProxyURL(value))
		}
	}
	if len(fields) == 0 {
		if doc.hasProxy() {
			return "未识别代理地址"
		}
		return "未配置"
	}
	return strings.Join(fields, "，")
}

// Cleanup removes only the three default proxy keys written by this module.
// As with daemon settings, manually changed files require manual cleanup.
func Cleanup(account *system.Account) error {
	if account == nil {
		return nil
	}
	path, err := ConfigPath(account)
	if err != nil {
		return err
	}
	doc, content, exists, err := readDocument(path)
	if err != nil || !exists || !doc.hasProxy() {
		return err
	}
	if err := requireManagedConfig(path, content); err != nil {
		return err
	}
	for _, key := range proxyKeys {
		delete(doc.defaults, key)
	}
	data, err := doc.marshal()
	if err != nil {
		return err
	}
	if err := shared.AtomicWriteFile(path, data, shared.AtomicWriteOptions{Mode: 0600}); err != nil {
		return err
	}
	return managedStore.ForgetManaged(path)
}

func requireManagedConfig(path string, content []byte) error {
	managed, err := managedStore.MatchesManaged(path, content)
	if err != nil {
		return err
	}
	if !managed {
		return fmt.Errorf("Docker 客户端配置没有本工具写入记录或已被修改，已保留，请手动检查: %s", path)
	}
	return nil
}
