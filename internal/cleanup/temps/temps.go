package temps

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"snail_tool/internal/environment/golang"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/ui"
)

// Locations are the only directories scanned for interrupted temporary files.
type Locations struct {
	TempDirs []string
	EtcDirs  []string
	GoRoot   string
}

type kind int

const (
	kindFile kind = iota
	kindDirectory
	kindGo
)

// Item is one recognized leftover. Unrelated files are never included.
type Item struct {
	Path string
	Name string
	Root string
	Kind kind
	Info os.FileInfo
}

var (
	tempFiles = []*regexp.Regexp{
		regexp.MustCompile(`^servertool-action-[0-9]+\.sh$`),
		regexp.MustCompile(`^servertool-docker-gpg-[0-9]+$`),
		regexp.MustCompile(`^servertool-get-docker-[0-9]+\.sh$`),
		regexp.MustCompile(`^snail-ssh-key-[0-9]+\.pub$`),
		regexp.MustCompile(`^snail-sshd-[0-9]+\.conf$`),
	}
	tempInstallDir = regexp.MustCompile(`^servertool-install\.[A-Za-z0-9]{6}$`)
	etcFiles       = []*regexp.Regexp{
		regexp.MustCompile(`^\.servertool-atomic-[0-9]+$`),
		regexp.MustCompile(`^\.snail-nftables-[0-9]+\.conf$`),
		regexp.MustCompile(`^\.snail-ssh-ports-[0-9]+\.nft$`),
	}
)

// DefaultLocations scans the system temp directory, /tmp, the /etc directories
// this tool writes, and the Go install root.
func DefaultLocations() Locations {
	return Locations{
		TempDirs: []string{os.TempDir(), "/tmp"},
		EtcDirs: []string{
			"/etc",
			"/etc/ssh",
			"/etc/ssh/sshd_config.d",
			"/etc/docker",
			"/etc/systemd/system/docker.service.d",
			"/etc/nut",
			"/etc/apt/sources.list.d",
			"/etc/apt/keyrings",
			"/etc/yum.repos.d",
			"/etc/pki/rpm-gpg",
		},
		GoRoot: "/opt/go",
	}
}

func Run(view *ui.UI) error {
	return run(view, DefaultLocations())
}

func run(view *ui.UI, locations Locations) error {
	items, err := Scan(locations)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		log.Info("未发现中断留下的临时文件")
		return nil
	}
	containsBackup := false
	for _, item := range items {
		if item.Kind == kindGo && strings.HasPrefix(item.Name, ".backup-") {
			containsBackup = true
		}
	}
	ui.PrintWarningCard("中断临时文件",
		ui.CardField{Label: "扫描位置", Value: "系统临时目录、/tmp、/etc 配置目录、/opt/go"},
		ui.CardField{Label: "数量", Value: fmt.Sprintf("%d 项", len(items))},
		ui.CardField{Label: "删除规则", Value: "只删除名称可识别的本工具残留"},
	)
	fmt.Println(ui.PrimaryBoldText("检测到以下中断临时文件："))
	for _, item := range items {
		fmt.Println("- " + item.Path)
	}
	if containsBackup {
		log.Warn("Go 备份目录可能包含异常中断前的旧版本，删除后无法通过该备份恢复")
	}
	confirmed, err := view.Confirm("确认删除以上中断临时文件？(y/N)：")
	if err != nil {
		if errors.Is(err, io.EOF) {
			log.Info("未收到确认，已保留临时文件")
			return nil
		}
		return err
	}
	if !confirmed {
		log.Info("已取消清理")
		return shared.ErrReturnToMenu
	}
	if err := Delete(items); err != nil {
		return err
	}
	ui.PrintSuccessCard("中断临时文件清理完成",
		ui.CardField{Label: "清理数量", Value: fmt.Sprintf("%d 项", len(items))},
	)
	return nil
}

// Scan lists recognizable leftovers. A matching name with an unexpected type stops the scan.
func Scan(locations Locations) ([]Item, error) {
	items := make([]Item, 0)
	for _, dir := range uniquePaths(locations.TempDirs) {
		found, err := scanNamed(dir, classifyTempName)
		if err != nil {
			return nil, err
		}
		items = append(items, found...)
	}
	for _, dir := range uniquePaths(locations.EtcDirs) {
		found, err := scanNamed(dir, classifyEtcName)
		if err != nil {
			return nil, err
		}
		items = append(items, found...)
	}
	if root := strings.TrimSpace(locations.GoRoot); root != "" {
		artifacts, err := golang.ListInstallArtifacts(root)
		if err != nil {
			return nil, err
		}
		for _, artifact := range artifacts {
			items = append(items, Item{
				Path: filepath.Join(artifact.Root, artifact.Name),
				Name: artifact.Name,
				Root: artifact.Root,
				Kind: kindGo,
				Info: artifact.Info,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

// Delete rechecks every item before removing any of them.
func Delete(items []Item) error {
	for _, item := range items {
		if err := validateItem(item); err != nil {
			return err
		}
	}
	for _, item := range items {
		if err := removeItem(item); err != nil {
			return err
		}
		log.Info("已清理：", item.Path)
	}
	return nil
}

func scanNamed(dir string, classify func(name string) (kind, bool)) ([]Item, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("检查扫描目录失败 %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("扫描目录是软链接，已停止: %s", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("扫描目录不是目录，已停止: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取扫描目录失败 %s: %w", dir, err)
	}
	items := make([]Item, 0)
	for _, entry := range entries {
		path, err := childPath(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		itemInfo, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("检查临时文件失败 %s: %w", path, err)
		}
		itemKind, ok := classify(entry.Name())
		if !ok {
			continue
		}
		if err := requireKind(path, itemKind, itemInfo); err != nil {
			return nil, err
		}
		items = append(items, Item{Path: path, Name: entry.Name(), Kind: itemKind, Info: itemInfo})
	}
	return items, nil
}

func classifyTempName(name string) (kind, bool) {
	if tempInstallDir.MatchString(name) {
		return kindDirectory, true
	}
	for _, pattern := range tempFiles {
		if pattern.MatchString(name) {
			return kindFile, true
		}
	}
	return 0, false
}

func classifyEtcName(name string) (kind, bool) {
	for _, pattern := range etcFiles {
		if pattern.MatchString(name) {
			return kindFile, true
		}
	}
	return 0, false
}

func requireKind(path string, kind kind, info os.FileInfo) error {
	switch kind {
	case kindFile:
		if info.Mode().IsRegular() {
			return nil
		}
	case kindDirectory:
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
	default:
		return fmt.Errorf("临时文件类型未知，已停止清理: %s", path)
	}
	return fmt.Errorf("临时文件类型异常，已停止清理: %s", path)
}

func validateItem(item Item) error {
	latest, err := os.Lstat(item.Path)
	if err != nil || !sameIdentity(item.Info, latest) {
		return fmt.Errorf("临时文件在确认期间变化，未删除: %s", item.Path)
	}
	switch item.Kind {
	case kindFile:
		return requireKind(item.Path, kindFile, latest)
	case kindDirectory:
		if err := requireKind(item.Path, kindDirectory, latest); err != nil {
			return err
		}
		return shared.CheckRemovalTree(item.Path)
	case kindGo:
		if filepath.Join(item.Root, item.Name) != item.Path || filepath.Base(item.Name) != item.Name {
			return fmt.Errorf("Go 安装残留路径无效，未删除: %s", item.Path)
		}
		return nil
	default:
		return fmt.Errorf("临时文件类型未知，已停止清理: %s", item.Path)
	}
}

func removeItem(item Item) error {
	switch item.Kind {
	case kindFile:
		return shared.RemoveRegularFile(item.Path)
	case kindDirectory:
		return shared.RemovePathTree(item.Path)
	case kindGo:
		return golang.DeleteInstallArtifact(golang.InstallArtifact{Root: item.Root, Name: item.Name, Info: item.Info})
	default:
		return fmt.Errorf("临时文件类型未知，已停止清理: %s", item.Path)
	}
}

func sameIdentity(expected, latest os.FileInfo) bool {
	return expected != nil && latest != nil && os.SameFile(expected, latest) &&
		expected.Size() == latest.Size() && expected.ModTime().Equal(latest.ModTime()) && expected.Mode() == latest.Mode()
}

func childPath(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf("临时文件名无效: %s", name)
	}
	dir = filepath.Clean(dir)
	path := filepath.Join(dir, name)
	if filepath.Dir(path) != dir {
		return "", fmt.Errorf("临时文件不在扫描目录内: %s", path)
	}
	return path, nil
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		unique = append(unique, path)
	}
	return unique
}
