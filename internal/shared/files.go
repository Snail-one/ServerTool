package shared

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"snail_tool/internal/tempguard"
)

type FileOwner struct {
	UID int
	GID int
}

// AtomicWriteOptions controls the metadata of an atomically replaced file.
// Existing files keep their mode and owner unless ForceMode or Owner is set.
type AtomicWriteOptions struct {
	Mode      os.FileMode
	ForceMode bool
	Owner     *FileOwner
}

var syncAtomicFile = func(file *os.File) error { return file.Sync() }
var syncAtomicDir = func(dir *os.File) error { return dir.Sync() }

// ReadRegularFile refuses symlinks in the file and its ancestors. Missing
// files and missing parent directories are reported as absent.
func ReadRegularFile(path string) ([]byte, bool, error) {
	exists, err := validateRemovalTarget(path)
	if err != nil || !exists {
		return nil, false, err
	}
	if _, _, err := validateAtomicTarget(path); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	return data, true, err
}

func AtomicWriteFile(path string, data []byte, options AtomicWriteOptions) error {
	return AtomicWrite(path, options, func(writer io.Writer) error {
		_, err := writer.Write(data)
		return err
	})
}

func AtomicWrite(path string, options AtomicWriteOptions, write func(io.Writer) error) (resultErr error) {
	if write == nil {
		return fmt.Errorf("原子写入 %s 失败: 写入函数不能为空", path)
	}
	if options.Mode.Perm() == 0 {
		return fmt.Errorf("原子写入 %s 失败: 必须指定新文件权限", path)
	}

	path = filepath.Clean(path)
	info, exists, err := validateAtomicTarget(path)
	if err != nil {
		return err
	}

	mode := options.Mode.Perm()
	var owner *FileOwner
	if exists {
		if !options.ForceMode {
			mode = info.Mode().Perm()
		}
		owner, err = fileOwner(info)
		if err != nil {
			return fmt.Errorf("读取目标文件所有者失败 %s: %w", path, err)
		}
	}
	if options.Owner != nil {
		owner = options.Owner
	}

	dir := filepath.Dir(path)
	release, err := tempguard.Use()
	if err != nil {
		return err
	}
	defer release()
	temp, err := os.CreateTemp(dir, ".servertool-atomic-*")
	if err != nil {
		return fmt.Errorf("在目标目录创建临时文件失败 %s: %w", dir, err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		if resultErr != nil {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(0600); err != nil {
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if err := write(temp); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := syncAtomicFile(temp); err != nil {
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("设置目标文件权限失败: %w", err)
	}
	if owner != nil {
		if err := temp.Chown(owner.UID, owner.GID); err != nil {
			return fmt.Errorf("设置目标文件所有者失败: %w", err)
		}
	}
	if err := syncAtomicFile(temp); err != nil {
		return fmt.Errorf("同步目标文件元数据失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	// Recheck immediately before replacement to reject a link introduced while writing.
	latest, latestExists, err := validateAtomicTarget(path)
	if err != nil {
		return err
	}
	if exists != latestExists || (exists && (!os.SameFile(info, latest) || info.Size() != latest.Size() || !info.ModTime().Equal(latest.ModTime()) || info.Mode() != latest.Mode())) {
		return fmt.Errorf("目标文件在写入期间发生变化，未覆盖: %s", path)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("原子替换目标文件失败 %s: %w", path, err)
	}

	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("打开父目录以同步失败 %s: %w", dir, err)
	}
	defer directory.Close()
	if err := syncAtomicDir(directory); err != nil {
		return fmt.Errorf("同步父目录失败 %s: %w", dir, err)
	}
	return nil
}

func validateAtomicTarget(path string) (os.FileInfo, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, false, fmt.Errorf("解析目标路径失败 %s: %w", path, err)
	}

	current := filepath.VolumeName(abs) + string(os.PathSeparator)
	parts := strings.Split(strings.TrimPrefix(abs, current), string(os.PathSeparator))
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		isTarget := index == len(parts)-1
		if os.IsNotExist(statErr) {
			if isTarget {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("目标文件父目录不存在 %s", current)
		}
		if statErr != nil {
			return nil, false, fmt.Errorf("检查路径失败 %s: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("拒绝通过软链接写入配置: %s", current)
		}
		if isTarget {
			if !info.Mode().IsRegular() {
				return nil, false, fmt.Errorf("拒绝写入非普通文件: %s", path)
			}
			return info, true, nil
		}
		if !info.IsDir() {
			return nil, false, fmt.Errorf("目标路径父级不是目录: %s", current)
		}
	}
	return nil, false, fmt.Errorf("无效的目标文件路径: %s", path)
}

type BlockMarker struct {
	Begin string
	End   string
}

func EnsureFileWithOptions(path string, options AtomicWriteOptions) error {
	_, exists, err := validateAtomicTarget(path)
	if err != nil || exists {
		return err
	}
	return AtomicWriteFile(path, nil, options)
}

// RemoveRegularFile removes a regular file without following symlinks and
// syncs its parent directory. A missing file is treated as already removed.
func RemoveRegularFile(path string) error {
	_, exists, err := validateAtomicTarget(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !exists {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("删除文件失败 %s: %w", path, err)
	}
	dir := filepath.Dir(filepath.Clean(path))
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("打开父目录以同步失败 %s: %w", dir, err)
	}
	defer directory.Close()
	if err := syncAtomicDir(directory); err != nil {
		return fmt.Errorf("同步父目录失败 %s: %w", dir, err)
	}
	return nil
}

// RemovePathTree recursively removes a regular file or directory tree after
// verifying that neither the target nor any existing parent is a symlink.
func RemovePathTree(path string) error {
	if err := CheckRemovalTree(path); err != nil {
		return err
	}
	exists, err := validateRemovalTarget(path)
	if err != nil || !exists {
		return err
	}
	path = filepath.Clean(path)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("递归删除路径失败 %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("打开父目录以同步失败 %s: %w", dir, err)
	}
	defer directory.Close()
	if err := syncAtomicDir(directory); err != nil {
		return fmt.Errorf("同步父目录失败 %s: %w", dir, err)
	}
	return nil
}

// CheckRemovalTree rejects broad system directories, redirected paths and mount
// points before recursive deletion. A bind mount can expose unrelated data even
// when no symbolic links are present.
func CheckRemovalTree(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == string(os.PathSeparator) {
		return fmt.Errorf("拒绝递归删除不明确或过宽的路径: %s", path)
	}
	switch path {
	case "/var/lib", "/var/log", "/var/cache", "/usr/local", "/usr/local/bin", "/usr/local/sbin", "/etc/systemd", "/etc/systemd/system":
		return fmt.Errorf("拒绝递归删除系统公共目录: %s", path)
	}
	exists, err := validateRemovalTarget(path)
	if err != nil || !exists {
		return err
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("无法检查删除范围内的挂载点，未删除: %w", err)
	}
	return checkRemovalMounts(path, string(data))
}

func checkRemovalMounts(path, mountinfo string) error {
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(mountinfo, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			return fmt.Errorf("挂载点信息不完整，未删除目录")
		}
		mount := unescape.Replace(fields[4])
		if mount == path || strings.HasPrefix(mount, path+string(os.PathSeparator)) {
			return fmt.Errorf("删除范围包含挂载点 %s，未删除，避免影响其他数据", mount)
		}
	}
	return nil
}

func validateRemovalTarget(path string) (bool, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false, fmt.Errorf("解析删除路径失败 %s: %w", path, err)
	}
	current := filepath.VolumeName(abs) + string(os.PathSeparator)
	parts := strings.Split(strings.TrimPrefix(abs, current), string(os.PathSeparator))
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		isTarget := index == len(parts)-1
		if os.IsNotExist(statErr) {
			return false, nil
		}
		if statErr != nil {
			return false, fmt.Errorf("检查删除路径失败 %s: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("拒绝通过软链接删除路径: %s", current)
		}
		if isTarget {
			if !info.IsDir() && !info.Mode().IsRegular() {
				return false, fmt.Errorf("拒绝删除非普通文件或目录: %s", path)
			}
			return true, nil
		}
		if !info.IsDir() {
			return false, fmt.Errorf("删除路径父级不是目录: %s", current)
		}
	}
	return false, fmt.Errorf("无效的删除路径: %s", path)
}

func RemoveManagedBlock(content, begin, end string) string {
	re := regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(begin) + `.*?` + regexp.QuoteMeta(end) + `\n?`)
	return re.ReplaceAllString(content, "")
}

// RemoveManagedBlockChecked refuses ambiguous markers instead of deleting the
// text between an accidental first begin and a later end.
func RemoveManagedBlockChecked(content, begin, end string) (string, error) {
	if begin == "" || end == "" || begin == end {
		return "", fmt.Errorf("托管区块标记无效")
	}
	begins, ends := strings.Count(content, begin), strings.Count(content, end)
	if begins == 0 && ends == 0 {
		return content, nil
	}
	if begins != 1 || ends != 1 || strings.Index(content, begin) > strings.Index(content, end) {
		return "", fmt.Errorf("托管区块标记重复、缺失或顺序异常，未清理配置")
	}
	for _, marker := range []string{begin, end} {
		at := strings.Index(content, marker)
		lineStart := strings.LastIndex(content[:at], "\n") + 1
		lineEnd := strings.Index(content[at:], "\n")
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += at
		}
		if strings.TrimSpace(content[lineStart:lineEnd]) != marker {
			return "", fmt.Errorf("托管区块标记未独占一行，未清理配置")
		}
	}
	return RemoveManagedBlock(content, begin, end), nil
}

func ManagedBlockContent(content, begin, end string) (string, bool) {
	re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(begin) + `(.*?)` + regexp.QuoteMeta(end))
	match := re.FindStringSubmatch(content)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func FormatManagedBlock(begin, body, end string) string {
	body = strings.Trim(body, "\n")
	if body == "" {
		return begin + "\n\n" + end + "\n"
	}
	return begin + "\n\n" + body + "\n\n" + end + "\n"
}

func AppendBlock(content, block string) string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return block
	}
	return content + "\n\n" + block
}

func NormalizeCleanedContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return content + "\n"
}

func CleanupManagedBlocks(path string, blocks ...BlockMarker) (bool, error) {
	data, exists, err := ReadRegularFile(path)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	content := string(data)
	cleaned := content
	for _, block := range blocks {
		cleaned, err = RemoveManagedBlockChecked(cleaned, block.Begin, block.End)
		if err != nil {
			return false, err
		}
	}
	if cleaned == content {
		return false, nil
	}

	cleaned = NormalizeCleanedContent(cleaned)
	return true, AtomicWriteFile(path, []byte(cleaned), AtomicWriteOptions{Mode: 0644})
}

func ContainsLine(content, wanted string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == wanted {
			return true
		}
	}
	return false
}

func ReadFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
