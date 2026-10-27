package input

import (
	"errors"
	"strings"
	"testing"
)

func TestTextPreservesReadableInput(t *testing.T) {
	for _, value := range []string{"", "203.0.113.10:8080", "中文 UPS 描述", "/docker/项目 空格", "user\tcomment", "密码@$%:!", strings.Repeat("a", MaxLength)} {
		if err := Text(value); err != nil {
			t.Fatalf("Text(%q): %v", value, err)
		}
	}
}

func TestTextRejectsCorruptionAndInvisibleCharacters(t *testing.T) {
	for _, value := range []string{"bad\xff", "bad\xef\xbf", "bad\ufffd", "bad\x00", "bad\x1b[31m", "bad\x7f", "bad\r", "bad\n", "bad\v", "bad\u0085", "bad\u200b", "bad\ufeff", "bad\u202e", "bad\u2028", "bad\u2029", strings.Repeat("a", MaxLength+1)} {
		if err := Text(value); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid text accepted: %q: %v", value, err)
		}
	}
}

func TestNumberRequiresDecimalDigitsAndRange(t *testing.T) {
	for _, value := range []string{"1", "65535", " 00022\t"} {
		if _, err := Number(value, 1, 65535); err != nil {
			t.Fatalf("valid number %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "0", "65536", "+22", "-22", "２２", "22.0", "2e2", "2 2", "22\r", "22\u200b", strings.Repeat("9", 100)} {
		if _, err := Number(value, 1, 65535); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid number accepted: %q: %v", value, err)
		}
	}
}
