package golang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	officialRoot = "/usr/local/go"
	pathBegin    = "# ===== BEGIN SNAIL GO ENVIRONMENT ====="
	pathEnd      = "# ===== END SNAIL GO ENVIRONMENT ====="
	pathBody     = `export PATH="/opt/go/current/bin:$PATH"`
)

func confirmOfficialInstallRemoval(view *ui.UI) (bool, error) {
	detected, err := officialMigrationState()
	if err != nil {
		return false, err
	}
	if !detected {
		return false, nil
	}
	confirmed, err := view.Confirm("检测到 /usr/local/go 或 ~/.bashrc 中的官方 Go 环境变量，是否清理并改用本工具安装？(y/N)：")
	if err != nil {
		return false, err
	}
	if !confirmed {
		log.Info("已取消操作，未修改 /usr/local/go 和 ~/.bashrc")
		return false, shared.ErrReturnToMenu
	}
	return true, nil
}

func finishOfficialInstallRemoval(remove bool) error {
	if !remove {
		return nil
	}
	return removeOfficialGoAndEnv()
}

func removeOfficialGoAndEnv() error {
	if err := removeOfficialInstall(officialRoot); err != nil {
		return err
	}
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	changed, err := cleanupOfficialGoBashrc(filepath.Join(account.Home, ".bashrc"))
	if err != nil {
		return err
	}
	if changed {
		if err := system.ChownPath(filepath.Join(account.Home, ".bashrc"), account, false); err != nil {
			return err
		}
		log.Info("已清理 ~/.bashrc 中引用 /usr/local/go 的 PATH 和 GOROOT")
	}
	ui.PrintSuccessCard("官方位置 Go 卸载完成",
		ui.CardField{Label: "安装位置", Value: officialRoot},
		ui.CardField{Label: "环境变量", Value: ui.Badge("已清理", true)},
	)
	return nil
}

func officialMigrationDetected() bool {
	detected, err := officialMigrationState()
	return err == nil && detected
}

func officialMigrationState() (bool, error) {
	if officialInstallDetected(officialRoot) {
		return true, nil
	}
	account, err := system.CurrentTargetUser()
	if err != nil {
		return false, err
	}
	content, err := os.ReadFile(filepath.Join(account.Home, ".bashrc"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return hasOfficialGoEnv(string(content)), nil
}

func hasOfficialGoEnv(content string) bool {
	_, changed := removeOfficialGoEnv(content)
	return changed
}

func cleanupOfficialGoBashrc(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cleaned, changed := removeOfficialGoEnv(string(data))
	if !changed {
		return false, nil
	}
	if err := shared.AtomicWriteFile(path, []byte(cleaned), shared.AtomicWriteOptions{Mode: 0644}); err != nil {
		return false, err
	}
	return true, nil
}

func removeOfficialGoEnv(content string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		candidate := trimmed
		if len(candidate) > len("export") && strings.HasPrefix(candidate, "export") &&
			(candidate[len("export")] == ' ' || candidate[len("export")] == '\t') {
			candidate = strings.TrimSpace(candidate[len("export"):])
		}
		equals := strings.IndexByte(candidate, '=')
		variable := ""
		if equals >= 0 {
			variable = strings.TrimSpace(candidate[:equals])
		}
		isGoAssignment := variable == "PATH" || variable == "GOROOT"
		if !strings.HasPrefix(trimmed, "#") && isGoAssignment && strings.Contains(candidate, "/usr/local/go") {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, ""), changed
}

func officialInstallDetected(root string) bool {
	return system.FileExists(filepath.Join(root, "bin", "go"))
}

func removeOfficialInstall(root string) error {
	if filepath.Clean(root) == string(os.PathSeparator) {
		return errors.New("拒绝删除根目录")
	}
	if !officialInstallDetected(root) {
		return nil
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("卸载官方位置 Go 失败: %w", err)
	}
	return nil
}

func configureTargetUserPath() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	bashrc := filepath.Join(account.Home, ".bashrc")
	if err := shared.EnsureFileWithOptions(bashrc, shared.AtomicWriteOptions{
		Mode: 0644, Owner: &shared.FileOwner{UID: account.UID, GID: account.GID},
	}); err != nil {
		return err
	}
	if err := writeManagedPath(bashrc); err != nil {
		return err
	}
	return system.ChownPath(bashrc, account, false)
}

func writeManagedPath(bashrc string) error {
	data, err := os.ReadFile(bashrc)
	if err != nil {
		return err
	}
	content := shared.RemoveManagedBlock(string(data), pathBegin, pathEnd)
	block := shared.FormatManagedBlock(pathBegin, pathBody, pathEnd)
	if err := shared.AtomicWriteFile(bashrc, []byte(shared.AppendBlock(content, block)), shared.AtomicWriteOptions{Mode: 0644}); err != nil {
		return err
	}
	return nil
}

func cleanupTargetUserPath() error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}
	bashrc := filepath.Join(account.Home, ".bashrc")
	changed, err := cleanupManagedPath(bashrc)
	if err != nil || !changed {
		return err
	}
	return system.ChownPath(bashrc, account, false)
}

func cleanupManagedPath(bashrc string) (bool, error) {
	return shared.CleanupManagedBlocks(bashrc, shared.BlockMarker{Begin: pathBegin, End: pathEnd})
}
