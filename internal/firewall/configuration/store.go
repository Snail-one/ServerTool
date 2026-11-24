package configuration

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/storage"
	"snail_tool/internal/tempguard"
)

// Store holds the environment required by firewall configuration operations.
type Store struct {
	Path             string
	DataDir          string
	CommandOutput    func(string, ...string) (string, error)
	PreserveOriginal func() error
}

type Version struct {
	Content []byte
	Exists  bool
}

func (store Store) Read() (Version, error) {
	data, exists, err := storage.ReadRegularFile(store.Path)
	return Version{Content: data, Exists: exists}, err
}

func (store Store) Check(expected Version) error {
	latest, err := store.Read()
	if err != nil {
		return err
	}
	if latest.Exists != expected.Exists || !bytes.Equal(latest.Content, expected.Content) {
		return fmt.Errorf("配置文件已被其他操作修改，未覆盖，请返回后重新操作")
	}
	return nil
}

// Give each firewall edit a distinct backup, including edits in the same second.
func (store Store) Backup() (string, error) {
	version, err := store.Read()
	if err != nil {
		return "", err
	}
	if !version.Exists {
		return "", fmt.Errorf("备份失败：配置文件不存在")
	}
	return (storage.BackupStore{Root: store.DataDir, Module: "firewall"}).Snapshot(store.Path)
}

func (store Store) Validate(version Version) error {
	if !version.Exists {
		return fmt.Errorf("未找到 %s，请先生成或编辑配置", store.Path)
	}
	if len(bytes.TrimSpace(version.Content)) == 0 {
		return fmt.Errorf("防火墙配置文件为空，请先生成或编辑配置")
	}
	if err := store.Check(version); err != nil {
		return err
	}
	log.Info("正在校验最新防火墙配置：", store.Path)
	if out, err := store.CommandOutput("nft", "-c", "-f", store.Path); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未加载规则: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (store Store) Write(content string, versions ...Version) error {
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()

	if info, err := os.Lstat(store.Path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("拒绝覆盖非普通文件或软链接: %s", store.Path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(store.Path), ".snail-nftables-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if out, err := store.CommandOutput("nft", "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未写入正式配置: %w: %s", err, strings.TrimSpace(out))
	}
	if len(versions) > 0 {
		if err := store.Check(versions[0]); err != nil {
			return err
		}
	}
	if err := store.PreserveOriginal(); err != nil {
		return err
	}
	if len(versions) > 0 {
		if err := store.Check(versions[0]); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(store.Path); err == nil {
		backup, err := store.Backup()
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	return storage.AtomicWrite(store.Path, storage.AtomicWriteOptions{Mode: 0644}, func(w io.Writer) error {
		if _, err := io.WriteString(w, content); err != nil {
			return err
		}
		if len(versions) > 0 {
			return store.Check(versions[0])
		}
		return nil
	})
}

// SaveAndApply reports whether the file was written even if loading fails.
func (store Store) SaveAndApply(version Version, content string) (bool, error) {
	if err := store.Write(content, version); err != nil {
		return storage.AtomicWriteReplaced(err), err
	}
	return true, store.Apply([]byte(content))
}

func (store Store) Apply(expected []byte) error {
	latest, err := os.ReadFile(store.Path)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, latest) {
		return fmt.Errorf("配置文件已变化，未加载规则，请重新选择校验加载配置")
	}
	if err := store.PreserveOriginal(); err != nil {
		return err
	}
	if out, err := store.CommandOutput("nft", "-f", store.Path); err != nil {
		return fmt.Errorf("nftables 配置加载失败: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}
