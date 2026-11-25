package keys

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"unicode"
)

func isAuthorizedKeyLine(line string) bool {
	_, keyType, _, _ := authorizedKeyParts(line)
	return keyType != ""
}

// An authorized_keys line begins with an optional list of login options.
// Whitespace inside a quoted option belongs to the option, not the key.
func authorizedKeyParts(line string) (options, keyType, body, comment string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", "", ""
	}
	fields := strings.Fields(line)
	if _, ok := authorizedKeyTypes[fields[0]]; !ok {
		end := authorizedOptionsEnd(line)
		if end == len(line) {
			return "", "", "", ""
		}
		options = line[:end]
		fields = strings.Fields(line[end:])
	}
	if len(fields) < 2 {
		return "", "", "", ""
	}
	if _, ok := authorizedKeyTypes[fields[0]]; !ok {
		return "", "", "", ""
	}
	return options, fields[0], fields[1], strings.Join(fields[2:], " ")
}

func authorizedOptionsEnd(line string) int {
	quoted, escaped := false, false
	for index, char := range line {
		switch {
		case escaped:
			escaped = false
		case quoted && char == '\\':
			escaped = true
		case char == '"':
			quoted = !quoted
		case !quoted && unicode.IsSpace(char):
			return index
		}
	}
	return len(line)
}

func summarizeAuthorizedKey(line string) string {
	options, keyType, body, comment := authorizedKeyParts(line)
	if keyType == "" {
		return truncateString(safeKeyText(strings.TrimSpace(line)), 120)
	}
	summary := keyType
	if blob, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(body, "=")); err == nil && len(blob) > 0 {
		sum := sha256.Sum256(blob)
		if strings.Contains(keyType, "-cert-") {
			summary += " · 证书摘要"
		}
		summary += " SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	} else {
		summary += " " + truncateString(safeKeyText(body), 28)
	}
	if options != "" {
		summary += " · 含登录选项"
	}
	if comment != "" {
		summary += " · " + truncateString(safeKeyText(comment), 48)
	}
	return summary
}

func safeKeyText(value string) string {
	return strings.Map(func(char rune) rune {
		if !unicode.IsPrint(char) {
			return '?'
		}
		return char
	}, value)
}

func truncateString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
