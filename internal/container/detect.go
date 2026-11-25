package container

import (
	"fmt"
	"strings"

	"snail_tool/internal/container/podman"
	"snail_tool/internal/system"
)

type Runtime struct {
	Name    string
	Display string
}

func DetectAllWithProgress(step func(string, func())) []Runtime {
	return probe(system.CommandExists, system.Output, step)
}

func probe(commandExists func(string) bool, output func(string, ...string) (string, error), step func(string, func())) []Runtime {
	if step == nil {
		step = func(_ string, detect func()) { detect() }
	}
	var hasPodman, hasDocker bool
	step("Docker / Podman 命令", func() {
		hasPodman = podman.Detect(commandExists)
		hasDocker = commandExists("docker")
	})
	var runtimes []Runtime
	if hasDocker {
		display, shim := probeDocker(output, step)
		if shim {
			hasPodman = true
		} else {
			runtimes = append(runtimes, Runtime{Name: "docker", Display: display})
		}
	}
	if hasPodman {
		runtimes = append(runtimes, Runtime{Name: "podman", Display: "Podman"})
	}
	return runtimes
}

func probeDocker(output func(string, ...string) (string, error), step func(string, func())) (display string, shim bool) {
	var version string
	var versionErr error
	step("Docker 版本（docker --version）", func() {
		version, versionErr = output("docker", "--version")
	})
	if strings.Contains(strings.ToLower(version), "podman") {
		return "", true
	}
	display = "Docker"
	if versionErr != nil {
		return "Docker（CLI 异常）", false
	}
	var info string
	var infoErr error
	step("Docker 服务（docker info）", func() {
		info, infoErr = output("docker", "info")
	})
	if infoErr != nil {
		detail := firstOutputLine(info)
		if detail == "" {
			detail = "daemon 不可达"
		}
		display = "Docker（服务异常：" + detail + "）"
	}
	return display, false
}

func ensureAbsent(commandExists func(string) bool, output func(string, ...string) (string, error)) error {
	return checkInstallAllowed(probe(commandExists, output, nil))
}

func checkInstallAllowed(runtimes []Runtime) error {
	if len(runtimes) == 0 {
		return nil
	}
	installed := make([]string, 0, len(runtimes))
	for _, item := range runtimes {
		installed = append(installed, item.Display)
	}
	return fmt.Errorf("安装前需先卸载当前运行时：%s", strings.Join(installed, "、"))
}

func firstOutputLine(value string) string {
	value = strings.TrimSpace(value)
	if line, _, ok := strings.Cut(value, "\n"); ok {
		value = strings.TrimSpace(line)
	}
	const maxRunes = 160
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return value
}
