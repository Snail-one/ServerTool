package shared

import (
	"os"
	"regexp"
	"strings"
)

type BlockMarker struct {
	Begin string
	End   string
}

func EnsureFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	return file.Close()
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
	return true, os.WriteFile(path, []byte(cleaned), 0644)
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
