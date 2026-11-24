package configuration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"snail_tool/internal/storage"
	"snail_tool/internal/tempguard"
)

type OriginalState struct {
	Version   int                     `json:"version"`
	Source    string                  `json:"source"`
	Config    *storage.OriginalBackup `json:"config,omitempty"`
	Autostart string                  `json:"autostart"`
}

func (store Store) OriginalPath() string {
	return filepath.Join(store.DataDir, "backups", "firewall", "state.original.json")
}

func (store Store) ReadOriginal(hasManagedConfig func(string) bool) (*OriginalState, error) {
	data, exists, err := storage.ReadRegularFile(store.OriginalPath())
	if err != nil || !exists {
		return nil, err
	}
	var state OriginalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("防火墙首次快照损坏，已停止操作: %w", err)
	}
	if state.Version != 1 || state.Source != store.Path || !ValidOriginalAutostart(state.Autostart) {
		return nil, fmt.Errorf("防火墙首次快照元数据无效，已停止操作")
	}
	if state.Config != nil {
		c := state.Config
		if c.Source != store.Path || c.Mode.Perm() == 0 || c.Mode != c.Mode.Perm() || c.Data == nil || hasManagedConfig(string(c.Data)) {
			return nil, fmt.Errorf("防火墙首次配置快照无效，已停止操作")
		}
		if c.Owner != nil && (c.Owner.UID < 0 || c.Owner.GID < 0) {
			return nil, fmt.Errorf("防火墙首次配置所有者无效，已停止操作")
		}
	}
	return &state, nil
}

func ValidOriginalAutostart(state string) bool {
	return state == "" || state == "enabled" || state == "disabled" || state == "enabled-runtime"
}

func (store Store) PreserveInitial(hasManagedConfig func(string) bool, inspect func() (string, error)) error {
	if state, err := store.ReadOriginal(hasManagedConfig); err != nil || state != nil {
		return err
	}
	version, err := store.Read()
	if err != nil {
		return err
	}
	// Existing managed configurations predate the feature. Never label their
	// current contents as the pre-tool state.
	if hasManagedConfig(string(version.Content)) {
		return nil
	}
	autostart, err := inspect()
	if err != nil {
		return err
	}
	if !ValidOriginalAutostart(autostart) {
		return fmt.Errorf("无法完整保存 nftables 自启状态 %q，未修改防火墙", autostart)
	}
	state := OriginalState{Version: 1, Source: store.Path, Autostart: autostart}
	if version.Exists {
		backups := storage.BackupStore{Root: store.DataDir, Module: "firewall"}
		if _, _, err := backups.KeepOriginal(store.Path); err != nil {
			return err
		}
		original, err := backups.ReadOriginal(store.Path)
		if err != nil {
			return err
		}
		if !bytes.Equal(original.Data, version.Content) {
			return fmt.Errorf("已有首次配置备份与当前文件不一致，未修改；请检查首次备份")
		}
		state.Config = &original
	}
	if err := store.Check(version); err != nil {
		return err
	}
	path := store.OriginalPath()
	if err := storage.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".firewall-original-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := storage.AtomicWriteFile(tmp.Name(), data, storage.AtomicWriteOptions{Mode: 0600}); err != nil {
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
	_, err = store.ReadOriginal(hasManagedConfig)
	return err
}
