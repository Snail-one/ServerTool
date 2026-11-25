package runtime

import (
	"net/url"
	"path/filepath"
	"strings"
)

func dockerRepositoryFilename(name, family string) bool {
	if family == "rpm" {
		return strings.HasSuffix(name, ".repo")
	}
	if !strings.HasSuffix(name, ".list") && !strings.HasSuffix(name, ".sources") {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
			return false
		}
	}
	return true
}

func dockerRepositoryURL(raw, distributionID string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "download.docker.com") {
		return false
	}
	path := "/linux/" + distributionID
	return parsed.Path == path || strings.HasPrefix(parsed.Path, path+"/")
}

func enabledDockerRepository(content, path string, distribution dockerDistribution) bool {
	if distribution.Family == "rpm" {
		return enabledDockerRPMRepository(content, distribution.ID)
	}
	if filepath.Ext(path) == ".sources" {
		return enabledDockerDeb822Repository(content, distribution.ID)
	}
	for _, line := range strings.Split(content, "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "deb" && fields[0] != "deb-src" {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		if strings.HasPrefix(rest, "[") {
			_, optionsEnd, ok := strings.Cut(rest, "]")
			if !ok {
				continue
			}
			rest = optionsEnd
		}
		if urls := strings.Fields(rest); len(urls) > 0 && dockerRepositoryURL(urls[0], distribution.ID) {
			return true
		}
	}
	return false
}

func enabledDockerDeb822Repository(content, distributionID string) bool {
	fields := make(map[string]string)
	var lastField string
	active := func() bool {
		if strings.EqualFold(strings.TrimSpace(fields["enabled"]), "no") {
			return false
		}
		types := strings.Fields(fields["types"])
		if !containsString(types, "deb") && !containsString(types, "deb-src") {
			return false
		}
		for _, uri := range strings.Fields(fields["uris"]) {
			if dockerRepositoryURL(uri, distributionID) {
				return true
			}
		}
		return false
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			if active() {
				return true
			}
			fields = make(map[string]string)
			lastField = ""
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			if lastField != "" {
				fields[lastField] += " " + line
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			lastField = strings.ToLower(key)
			fields[lastField] = strings.TrimSpace(value)
		}
	}
	return active()
}

func enabledDockerRPMRepository(content, distributionID string) bool {
	fields := make(map[string]string)
	var inRepository bool
	var lastField string
	active := func() bool {
		if !inRepository {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(fields["enabled"])) {
		case "0", "no", "false", "off":
			return false
		}
		for _, key := range []string{"baseurl", "metalink", "mirrorlist"} {
			for _, uri := range strings.Fields(fields[key]) {
				if dockerRepositoryURL(uri, distributionID) {
					return true
				}
			}
		}
		return false
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if active() {
				return true
			}
			fields = make(map[string]string)
			inRepository = !strings.EqualFold(line, "[main]")
			lastField = ""
			continue
		}
		if !inRepository {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			lastField = strings.ToLower(strings.TrimSpace(key))
			fields[lastField] = strings.TrimSpace(value)
		} else if lastField != "" && (raw[0] == ' ' || raw[0] == '\t') {
			fields[lastField] += " " + line
		}
	}
	return active()
}
