package system

import "strings"

// ParseOSRelease reads the key-value fields of an os-release file.
func ParseOSRelease(content string) map[string]string {
	values := make(map[string]string)
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return values
}
