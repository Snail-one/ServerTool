package firewall

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

const configBegin = "# ===== BEGIN SNAIL NFTABLES CONFIG ====="
const configEnd = "# ===== END SNAIL NFTABLES CONFIG ====="

func Cleanup(_ *system.Account) error {
	version, err := readConfigVersion()
	if err != nil {
		return err
	}
	content := string(version.content)
	managed := strings.Contains(content, configBegin) || strings.Contains(content, configEnd)
	if managed && (strings.Count(content, configBegin) != 1 || strings.Count(content, configEnd) != 1 || strings.Index(content, configBegin) > strings.Index(content, configEnd)) {
		return fmt.Errorf("防火墙托管标记损坏，未清理配置或规则")
	}
	var loaded bool
	if commandExists("nft") {
		live := inspectLiveRules()
		if live.err != nil {
			return fmt.Errorf("检查防火墙规则失败，未清理: %w", live.err)
		}
		loaded = live.snailLoaded
	} else if managed {
		return fmt.Errorf("未找到 nft，无法清理已加载防火墙规则，已保留配置")
	}
	if managed {
		if err := checkConfigVersion(version); err != nil {
			return err
		}
		cleaned := shared.NormalizeCleanedContent(shared.RemoveManagedBlock(content, configBegin, configEnd))
		if err := shared.AtomicWriteFile(configPath, []byte(cleaned), shared.AtomicWriteOptions{Mode: 0644}); err != nil {
			return err
		}
		log.Info("已移除防火墙托管配置")
	}
	if loaded {
		if output, err := commandOutput("nft", "delete", "table", "inet", "snail"); err != nil {
			return fmt.Errorf("删除 inet snail 表失败: %w: %s", err, strings.TrimSpace(output))
		}
		log.Info("已删除 inet snail 表")
	}
	return nil
}
