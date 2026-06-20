package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"snail_tool/internal/log"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const defaultComposeRoot = "/opt/apps"

var composeFilenames = map[string]struct{}{
	"docker-compose.yml":  {},
	"docker-compose.yaml": {},
	"compose.yml":         {},
	"compose.yaml":        {},
}

type composeCommand struct {
	name    string
	args    []string
	display string
}

func UpdateDockerComposeApps(view *ui.UI) error {
	log.Info("批量更新运行中的 Docker Compose 应用")
	fmt.Println()

	rawRoot, err := view.Ask(fmt.Sprintf("请输入扫描目录（直接回车使用 %s）: ", defaultComposeRoot))
	if err != nil {
		return err
	}
	root := strings.TrimSpace(rawRoot)
	if root == "" {
		root = defaultComposeRoot
	}
	root = filepath.Clean(root)

	if !system.DirExists(root) {
		return fmt.Errorf("扫描目录不存在或不是目录: %s", root)
	}

	compose, err := detectComposeCommand()
	if err != nil {
		return err
	}

	dirs, err := findComposeDirs(root)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		log.Warn("未找到 Docker Compose 配置文件")
		return nil
	}

	fmt.Printf("扫描目录：%s\n", root)
	fmt.Printf("Compose 命令：%s\n", compose.display)
	fmt.Printf("找到 %d 个 Compose 目录：\n", len(dirs))
	for _, dir := range dirs {
		fmt.Printf("- %s\n", dir)
	}
	fmt.Println()

	confirmed, err := view.Confirm("将只更新运行中的项目，并在结束后清理无用镜像，是否继续？(y/N): ")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println("已取消更新")
		return nil
	}

	updated, skipped := 0, 0
	for _, dir := range dirs {
		ran, err := updateComposeDir(compose, dir)
		if err != nil {
			return err
		}
		if ran {
			updated++
		} else {
			skipped++
		}
	}

	fmt.Println()
	log.Info("清理无用镜像")
	if err := system.Run("docker", "image", "prune", "-f"); err != nil {
		return fmt.Errorf("清理无用镜像失败: %w", err)
	}

	fmt.Println()
	log.Info("完成")
	fmt.Printf("已更新：%d，已跳过：%d\n", updated, skipped)
	return nil
}

func detectComposeCommand() (composeCommand, error) {
	if err := exec.Command("docker", "compose", "version").Run(); err == nil {
		return composeCommand{name: "docker", args: []string{"compose"}, display: "docker compose"}, nil
	}
	if system.CommandExists("docker-compose") {
		return composeCommand{name: "docker-compose", display: "docker-compose"}, nil
	}
	return composeCommand{}, fmt.Errorf("未找到 docker compose 或 docker-compose")
}

func findComposeDirs(root string) ([]string, error) {
	root = filepath.Clean(root)
	dirs := make(map[string]struct{})

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		depth, err := pathDepth(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if depth >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if depth != 2 || !isRegularFile(entry) {
			return nil
		}

		if _, ok := composeFilenames[entry.Name()]; !ok {
			return nil
		}
		dirs[filepath.Dir(path)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(dirs))
	for dir := range dirs {
		result = append(result, dir)
	}
	sort.Strings(result)
	return result, nil
}

func pathDepth(root, path string) (int, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, err
	}
	if rel == "." {
		return 0, nil
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1, nil
}

func isRegularFile(entry os.DirEntry) bool {
	if entry.Type().IsRegular() {
		return true
	}
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular()
}

func updateComposeDir(compose composeCommand, dir string) (bool, error) {
	running, err := composeProjectRunning(compose, dir)
	if err != nil {
		return false, err
	}
	if !running {
		log.Info("跳过（未运行）: ", dir)
		return false, nil
	}

	log.Info("更新（运行中）: ", dir)
	if err := runCompose(compose, dir, "pull"); err != nil {
		return true, fmt.Errorf("%s pull 失败: %w", dir, err)
	}
	if err := runCompose(compose, dir, "up", "-d", "--remove-orphans"); err != nil {
		return true, fmt.Errorf("%s up 失败: %w", dir, err)
	}
	return true, nil
}

func composeProjectRunning(compose composeCommand, dir string) (bool, error) {
	output, err := composeOutput(compose, dir, "ps", "--status", "running", "-q")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(output) != "", nil
}

func runCompose(compose composeCommand, dir string, args ...string) error {
	cmd := exec.Command(compose.name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func composeOutput(compose composeCommand, dir string, args ...string) (string, error) {
	cmd := exec.Command(compose.name, composeArgs(compose, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func composeArgs(compose composeCommand, args ...string) []string {
	allArgs := append([]string{}, compose.args...)
	return append(allArgs, args...)
}
