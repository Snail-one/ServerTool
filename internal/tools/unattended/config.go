package unattended

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"snail_tool/internal/storage"
	"snail_tool/internal/tempguard"
	"snail_tool/internal/ui"
)

const (
	refreshKey         = "APT::Periodic::Update-Package-Lists"
	upgradeKey         = "APT::Periodic::Unattended-Upgrade"
	intervalKey        = "ServerTool::Unattended-Upgrade::Interval"
	rebootKey          = "Unattended-Upgrade::Automatic-Reboot"
	shutdownKey        = "Unattended-Upgrade::InstallOnShutdown"
	shutdownPreference = "ServerTool::Unattended-Upgrade::InstallOnShutdown"
	optionTag          = "; // ServerTool"
)

func activeInterval(value string) bool {
	days, err := strconv.Atoi(value)
	return value == "always" || err == nil && days > 0
}

func updateOptions(previous []byte, values map[string]string) string {
	body := strings.TrimPrefix(string(previous), managedHeader)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	kept := lines[:0]
	for _, line := range lines {
		key, _, _ := strings.Cut(line, " ")
		_, replaced := values[key]
		if replaced && strings.HasSuffix(line, optionTag) {
			continue
		}
		if line != "" || len(kept) > 0 {
			kept = append(kept, line)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		kept = append(kept, fmt.Sprintf("%s %q%s", key, values[key], optionTag))
	}
	return managedHeader + strings.Join(kept, "\n") + "\n"
}

func readManaged(path, header string) ([]byte, bool, error) {
	data, exists, err := storage.ReadRegularFile(path)
	if err == nil && exists && !bytes.HasPrefix(data, []byte(header)) {
		err = fmt.Errorf("无法识别现有配置，请先检查 %s", path)
	}
	return data, exists, err
}

func checkSnapshot(path string, previous []byte, exists bool) error {
	current, present, err := storage.ReadRegularFile(path)
	if err != nil {
		return err
	}
	if present != exists || !bytes.Equal(current, previous) {
		return fmt.Errorf("配置已变化，请重新确认: %s", path)
	}
	return nil
}

func replaceConfig(path string, previous []byte, exists bool, content string) error {
	if content == "" {
		if err := checkSnapshot(path, previous, exists); err != nil || !exists {
			return err
		}
		return storage.RemoveRegularFile(path)
	}
	return storage.AtomicWrite(path, storage.AtomicWriteOptions{Mode: 0644}, func(w io.Writer) error {
		if err := checkSnapshot(path, previous, exists); err != nil {
			return err
		}
		_, err := io.WriteString(w, content)
		return err
	})
}

// publish validates the installed configuration and restores the previous snapshot on failure.
func publish(path string, previous []byte, exists bool, content string, apply func() error) error {
	if err := checkSnapshot(path, previous, exists); err != nil {
		return err
	}
	if string(previous) == content {
		return apply()
	}
	if err := replaceConfig(path, previous, exists, content); err != nil {
		if storage.AtomicWriteReplaced(err) {
			return errors.Join(err, replaceConfig(path, []byte(content), true, string(previous)))
		}
		return err
	}
	if err := apply(); err != nil {
		restore := replaceConfig(path, []byte(content), content != "", string(previous))
		if restore != nil {
			restore = fmt.Errorf("恢复 %s 失败: %w", path, restore)
		}
		return errors.Join(err, restore)
	}
	return nil
}

func (service manager) saveOptions(previous []byte, exists bool, values map[string]string) error {
	content := updateOptions(previous, values)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	args := []string{"shell"}
	for index, key := range keys {
		args = append(args, fmt.Sprintf("OPT%d", index), key)
	}
	return publish(service.path, previous, exists, content, func() error {
		output, err := service.query("apt-config", args...)
		if err != nil {
			return fmt.Errorf("读取生效配置失败: %w", err)
		}
		effective := assignments(output)
		for index, key := range keys {
			if effective[fmt.Sprintf("OPT%d", index)] != values[key] {
				return fmt.Errorf("APT 生效设置 %s 与预期不一致，请检查配置优先级", key)
			}
		}
		return nil
	})
}

func (service manager) editNative(view *ui.UI) error {
	if !service.root() {
		return fmt.Errorf("编辑 APT 配置需要 root 权限")
	}
	previous, exists, err := readManaged(service.path, managedHeader)
	if err != nil {
		return err
	}
	editor := ""
	for _, candidate := range []string{"vim", "vi", "nano"} {
		if service.exists(candidate) {
			editor = candidate
			break
		}
	}
	if editor == "" {
		return fmt.Errorf("未找到 vim、vi 或 nano")
	}
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()
	dir, err := os.MkdirTemp("", "servertool-unattended-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	draft := dir + "/policy.conf"
	initial := previous
	if !exists {
		initial = []byte(managedHeader + "// APT::Periodic::* and Unattended-Upgrade::* options.\n")
	}
	if err := os.WriteFile(draft, initial, 0600); err != nil {
		return err
	}
	if err := service.run(editor, draft); err != nil {
		return err
	}
	edited, present, err := storage.ReadRegularFile(draft)
	if err != nil {
		return err
	}
	if !present || bytes.Equal(initial, edited) {
		return ui.ErrReturnToMenu
	}
	if _, err := service.query("apt-config", "-c", draft, "dump"); err != nil {
		return fmt.Errorf("APT 配置语法校验失败: %w", err)
	}
	if !bytes.HasPrefix(edited, []byte(managedHeader)) {
		edited = append([]byte(managedHeader), edited...)
	}
	confirmed, err := view.Confirm("确认保存原生 APT 配置？(y/N)：")
	if err != nil {
		return err
	}
	if !confirmed {
		return ui.ErrReturnToMenu
	}
	return publish(service.path, previous, exists, string(edited), service.validateAPT)
}

func (service manager) resetNative(view *ui.UI) error {
	if !service.root() {
		return fmt.Errorf("恢复 APT 配置需要 root 权限")
	}
	previous, exists, err := readManaged(service.path, managedHeader)
	if err != nil {
		return err
	}
	if !exists {
		ui.PrintInfoCard("当前使用原有 APT 策略")
		return nil
	}
	if err := confirm(view, "确认恢复系统原有的自动更新策略？(y/N)："); err != nil {
		return err
	}
	return publish(service.path, previous, exists, "", service.validateAPT)
}

func (service manager) validateAPT() error {
	_, err := service.query("apt-config", "dump")
	return err
}
