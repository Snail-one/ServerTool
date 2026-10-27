// Package input validates user-supplied text before it reaches configuration
// writers, command arguments, or terminal output.
package input

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxLength = 64 * 1024

var ErrInvalid = errors.New("输入无效")

// Text accepts a single line of readable UTF-8. Tabs remain available as field
// separators; invisible format characters and embedded line breaks are rejected.
// Never repair corrupted text silently: doing so may change a password or path.
func Text(value string) error {
	if len(value) > MaxLength {
		return fmt.Errorf("%w：单行内容不能超过 64 KiB", ErrInvalid)
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, utf8.RuneError) {
		return fmt.Errorf("%w：包含乱码或无效编码，请清空后重新输入", ErrInvalid)
	}
	for _, char := range value {
		if char != '\t' && (unicode.IsControl(char) || unicode.Is(unicode.Cf, char) || unicode.Is(unicode.Zl, char) || unicode.Is(unicode.Zp, char)) {
			return fmt.Errorf("%w：包含控制字符或不可见字符 U+%04X，请清空后重新输入", ErrInvalid, char)
		}
	}
	return nil
}

// Number accepts ASCII decimal digits, optional surrounding whitespace, and an
// inclusive range. Signs, fractions and implicit numeric conversions are refused.
func Number(raw string, min, max int) (int, error) {
	if err := Text(raw); err != nil {
		return 0, err
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("%w：请输入数字", ErrInvalid)
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("%w：只能输入 0-9 的十进制数字", ErrInvalid)
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < min || number > max {
		return 0, fmt.Errorf("%w：数字范围必须是 %d-%d", ErrInvalid, min, max)
	}
	return number, nil
}
