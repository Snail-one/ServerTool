package firewall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/shared"
)

// Preserve the first pre-tool state before changing configuration or services.
var preserveOriginal = saveOriginalState

type originalState struct {
	Version   int                    `json:"version"`
	Source    string                 `json:"source"`
	Config    *shared.OriginalBackup `json:"config,omitempty"`
	Autostart string                 `json:"autostart"`
}

func originalStatePath() string {
	return filepath.Join(dataDir, "backups", "firewall", "state.original.json")
}

func readOriginalState() (*originalState, error) {
	data, exists, err := shared.ReadRegularFile(originalStatePath())
	if err != nil || !exists {
		return nil, err
	}
	var state originalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("防火墙首次快照损坏，已停止操作: %w", err)
	}
	if state.Version != 1 || state.Source != configPath || !validOriginalAutostart(state.Autostart) {
		return nil, fmt.Errorf("防火墙首次快照元数据无效，已停止操作")
	}
	if state.Config != nil {
		c := state.Config
		if c.Source != configPath || c.Mode.Perm() == 0 || c.Mode != c.Mode.Perm() || c.Data == nil || hasManagedConfig(string(c.Data)) {
			return nil, fmt.Errorf("防火墙首次配置快照无效，已停止操作")
		}
		if c.Owner != nil && (c.Owner.UID < 0 || c.Owner.GID < 0) {
			return nil, fmt.Errorf("防火墙首次配置所有者无效，已停止操作")
		}
	}
	return &state, nil
}

func validOriginalAutostart(state string) bool {
	return state == "" || state == "enabled" || state == "disabled" || state == "enabled-runtime"
}

func hasManagedConfig(content string) bool {
	for _, marker := range []string{configBegin, configEnd, rulesBegin, rulesEnd, sshPortsBegin, sshPortsEnd, pingBegin, pingEnd} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

func saveOriginalState() error {
	if state, err := readOriginalState(); err != nil || state != nil {
		return err
	}
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	// Existing managed configurations predate the feature. Never label their
	// current contents as the pre-tool state.
	if hasManagedConfig(string(version.content)) {
		return nil
	}
	if commandExists("nft") {
		live := inspectLiveRules()
		if live.err != nil {
			return fmt.Errorf("无法保存接入前防火墙状态: %w", live.err)
		}
		if live.snailLoaded {
			return fmt.Errorf("已有 inet snail 表但缺少本工具托管标记，拒绝接管或覆盖原有规则")
		}
	}
	state := originalState{Version: 1, Source: configPath}
	if commandExists("systemctl") {
		status := inspectAutostart()
		if status.err != nil && status.label != "服务不可用" {
			return status.err
		}
		if status.err == nil {
			if !validOriginalAutostart(status.state) {
				return fmt.Errorf("无法完整保存 nftables 自启状态 %q，未修改防火墙", status.state)
			}
			state.Autostart = status.state
		}
	}
	if version.exists {
		store := shared.BackupStore{Root: dataDir, Module: "firewall"}
		if _, _, err := store.KeepOriginal(configPath); err != nil {
			return err
		}
		original, err := store.ReadOriginal(configPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(original.Data, version.content) {
			return fmt.Errorf("已有首次配置备份与当前文件不一致，未修改；请检查首次备份")
		}
		state.Config = &original
	}
	if err := checkConfigVersion(version); err != nil {
		return err
	}
	path := originalStatePath()
	if err := shared.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".firewall-original-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := shared.AtomicWriteFile(tmp.Name(), data, shared.AtomicWriteOptions{Mode: 0600}); err != nil {
		return err
	}
	// Publish once without replacing another process's first snapshot.
	if err := os.Link(tmp.Name(), path); err != nil && !os.IsExist(err) {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	_, err = readOriginalState()
	return err
}
