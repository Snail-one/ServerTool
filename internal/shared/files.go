package shared

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	if _, _, err := validateAtomicTarget(path); err != nil {
		return err
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

func EnsureFile(path string) error {
	return EnsureFileWithOptions(path, AtomicWriteOptions{Mode: 0644})
}

func EnsureFileWithOptions(path string, options AtomicWriteOptions) error {
	_, exists, err := validateAtomicTarget(path)
	if err != nil || exists {
		return err
	}
	return AtomicWriteFile(path, nil, options)
}

func RemoveManagedBlock(content, begin, end string) string {
	re := regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(begin) + `.*?` + regexp.QuoteMeta(end) + `\n?`)
	return re.ReplaceAllString(content, "")
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
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	content := string(data)
	cleaned := content
	for _, block := range blocks {
		cleaned = RemoveManagedBlock(cleaned, block.Begin, block.End)
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

func FileContains(path, wanted string) bool {
	return strings.Contains(ReadFileString(path), wanted)
}

func FileContainsNonEmptyContent(path string) bool {
	return strings.TrimSpace(ReadFileString(path)) != ""
}

func ReadFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
