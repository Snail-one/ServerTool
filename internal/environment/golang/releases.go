package golang

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"snail_tool/internal/log"
)

const releasesURL = "https://go.dev/dl/?mode=json&include=all"

type release struct {
	Version string        `json:"version"`
	Stable  bool          `json:"stable"`
	Files   []releaseFile `json:"files"`
}

type releaseFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

var apiClient = &http.Client{Timeout: 30 * time.Second}

func fetchReleases() ([]release, error) {
	log.Info("请求 Go 官方版本 API：", releasesURL)
	response, err := apiClient.Get(releasesURL)
	if err != nil {
		return nil, fmt.Errorf("请求 Go 官方版本 API 失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Go 官方版本 API 返回 HTTP %d", response.StatusCode)
	}
	log.Info("Go 官方版本 API 响应成功：", response.Status)
	log.Info("解析官方版本数据...")
	var releases []release
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 Go 官方版本 API 失败: %w", err)
	}
	log.Info("官方版本数据解析完成，发行记录：", len(releases), " 条")
	return releases, nil
}

func availableReleases(input []release, arch string) []release {
	result := make([]release, 0, len(input))
	seen := make(map[string]bool)
	for _, item := range input {
		if !item.Stable || seen[item.Version] || !validVersion(item.Version) {
			continue
		}
		if _, ok := archiveFor(item, arch); !ok {
			continue
		}
		seen[item.Version] = true
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return compareVersions(result[i].Version, result[j].Version) > 0
	})
	return result
}

func archiveFor(item release, arch string) (releaseFile, bool) {
	for _, file := range item.Files {
		if file.OS == "linux" && file.Arch == arch && file.Kind == "archive" && file.SHA256 != "" {
			return file, true
		}
	}
	return releaseFile{}, false
}

func supportedArch(arch string) (string, error) {
	switch arch {
	case "amd64", "arm64":
		return arch, nil
	default:
		return "", fmt.Errorf("暂不支持 Linux %s 架构，仅支持 amd64 和 arm64", arch)
	}
}

func validVersion(version string) bool {
	parts, ok := versionParts(version)
	return ok && len(parts) >= 2
}

func versionParts(version string) ([]int, bool) {
	if !strings.HasPrefix(version, "go") {
		return nil, false
	}
	raw := strings.TrimPrefix(version, "go")
	pieces := strings.Split(raw, ".")
	if len(pieces) < 2 || len(pieces) > 3 {
		return nil, false
	}
	result := make([]int, len(pieces))
	for i, piece := range pieces {
		if piece == "" || (len(piece) > 1 && piece[0] == '0') {
			return nil, false
		}
		value, err := strconv.Atoi(piece)
		if err != nil || value < 0 {
			return nil, false
		}
		result[i] = value
	}
	return result, true
}

func compareVersions(left, right string) int {
	a, _ := versionParts(left)
	b, _ := versionParts(right)
	for len(a) < 3 {
		a = append(a, 0)
	}
	for len(b) < 3 {
		b = append(b, 0)
	}
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}
