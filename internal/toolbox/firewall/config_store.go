package firewall

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/tempguard"
)

type configVersion struct {
	content []byte
	exists  bool
}

func readConfigVersion() (configVersion, error) {
	data, exists, err := shared.ReadRegularFile(configPath)
	return configVersion{content: data, exists: exists}, err
}

func checkConfigVersion(expected configVersion) error {
	latest, err := readConfigVersion()
	if err != nil {
		return err
	}
	if latest.exists != expected.exists || !bytes.Equal(latest.content, expected.content) {
		return fmt.Errorf("配置文件已被其他操作修改，未覆盖，请返回后重新操作")
	}
	return nil
}

// Give each firewall edit a distinct backup, including edits in the same second.
func backupFirewallConfig() (string, error) {
	version, err := readConfigVersion()
	if err != nil {
		return "", err
	}
	if !version.exists {
		return "", fmt.Errorf("备份失败：配置文件不存在")
	}
	return (shared.BackupStore{Root: dataDir, Module: "firewall"}).Snapshot(configPath)
}

func validateSavedConfig(version configVersion) error {
	if !version.exists {
		return fmt.Errorf("未找到 %s，请先生成或编辑配置", configPath)
	}
	if len(bytes.TrimSpace(version.content)) == 0 {
		return fmt.Errorf("防火墙配置文件为空，请先生成或编辑配置")
	}
	if err := checkConfigVersion(version); err != nil {
		return err
	}
	log.Info("正在校验最新防火墙配置：", configPath)
	if out, err := commandOutput("nft", "-c", "-f", configPath); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未加载规则: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func writeConfig(content string, versions ...configVersion) error {
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()

	if info, err := os.Lstat(configPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("拒绝覆盖非普通文件或软链接: %s", configPath)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".snail-nftables-*.conf")
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
	if out, err := commandOutput("nft", "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("nftables 配置校验失败，未写入正式配置: %w: %s", err, strings.TrimSpace(out))
	}
	if len(versions) > 0 {
		if err := checkConfigVersion(versions[0]); err != nil {
			return err
		}
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	if len(versions) > 0 {
		if err := checkConfigVersion(versions[0]); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(configPath); err == nil {
		backup, err := backupFirewallConfig()
		if err != nil {
			return err
		}
		log.Info("已备份原防火墙配置：", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	return shared.AtomicWrite(configPath, shared.AtomicWriteOptions{Mode: 0644}, func(w io.Writer) error {
		if _, err := io.WriteString(w, content); err != nil {
			return err
		}
		if len(versions) > 0 {
			return checkConfigVersion(versions[0])
		}
		return nil
	})
}

// saveAndApplyConfig reports whether the file was written even if loading fails.
func saveAndApplyConfig(version configVersion, content string) (bool, error) {
	if err := writeConfig(content, version); err != nil {
		return shared.AtomicWriteReplaced(err), err
	}
	return true, applyConfig([]byte(content))
}

func applyConfig(expected []byte) error {
	latest, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, latest) {
		return fmt.Errorf("配置文件已变化，未加载规则，请重新选择校验加载配置")
	}
	if err := preserveOriginal(); err != nil {
		return err
	}
	if out, err := commandOutput("nft", "-f", configPath); err != nil {
		return fmt.Errorf("nftables 配置加载失败: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}
