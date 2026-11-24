package unattended

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const debianStableReleaseURL = "https://deb.debian.org/debian/dists/stable/Release"

func localDebianMajor() (int, error) {
	data, err := os.ReadFile("/etc/os-release")
	if os.IsNotExist(err) {
		data, err = os.ReadFile("/usr/lib/os-release")
	}
	if err != nil {
		return 0, err
	}
	fields := system.ParseOSRelease(string(data))
	if fields["ID"] != "debian" {
		return 0, fmt.Errorf("此设置适用于 Debian")
	}
	return majorVersion(fields["VERSION_ID"])
}

func majorVersion(version string) (int, error) {
	major, _, _ := strings.Cut(version, ".")
	value, err := strconv.Atoi(major)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("无法识别 Debian 版本 %q", version)
	}
	return value, nil
}

func debianReleaseNotice() ui.CardField {
	field := ui.CardField{Label: "Debian 大版本"}
	current, err := localDebianMajor()
	if err != nil {
		field.Value, field.Detail = "检测失败", err.Error()
		return field
	}
	client := &http.Client{Timeout: 4 * time.Second}
	stable, codename, err := fetchStableRelease(client, debianStableReleaseURL)
	if err != nil {
		field.Value, field.Detail = "检查失败", err.Error()
	} else if stable > current {
		field.Value = fmt.Sprintf("Debian %d（%s）可用", stable, codename)
		field.Detail = fmt.Sprintf("当前版本：Debian %d", current)
	} else {
		field.Value = "暂无更新的大版本"
	}
	return field
}

func fetchStableRelease(client *http.Client, url string) (int, string, error) {
	response, err := client.Get(url)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("读取 Debian 发布信息失败：HTTP %d", response.StatusCode)
	}
	return parseStableRelease(io.LimitReader(response.Body, 16*1024))
}

func parseStableRelease(source io.Reader) (int, string, error) {
	fields := make(map[string]string)
	scanner := bufio.NewScanner(source)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, " ") {
			break
		}
		if key, value, ok := strings.Cut(line, ":"); ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, "", err
	}
	codename := fields["Codename"]
	if fields["Origin"] != "Debian" || fields["Suite"] != "stable" || !validCodename(codename) {
		return 0, "", fmt.Errorf("Debian 稳定版发布信息格式异常")
	}
	version, err := majorVersion(fields["Version"])
	return version, codename, err
}

func validCodename(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < 'a' || char > 'z' {
			return false
		}
	}
	return true
}
