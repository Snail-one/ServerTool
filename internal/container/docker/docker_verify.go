package runtime

import (
	"fmt"
	"strings"

	"snail_tool/internal/log"
)

type dockerOutputFunc func(string, ...string) (string, error)

func verifyDockerInstallation(output dockerOutputFunc) error {
	checks := []struct {
		stage string
		args  []string
	}{
		{stage: "Docker Engine/CLI", args: []string{"version"}},
		{stage: "Docker daemon", args: []string{"info"}},
		{stage: "Docker Compose 插件", args: []string{"compose", "version"}},
		{stage: "Docker Buildx 插件", args: []string{"buildx", "version"}},
	}
	for _, check := range checks {
		log.Info("[Docker 安装/本地验证] docker ", strings.Join(check.args, " "))
		result, err := output("docker", check.args...)
		if err != nil {
			return fmt.Errorf("%s 验证失败: %w（输出：%s）", check.stage, err, strings.TrimSpace(result))
		}
		if strings.TrimSpace(result) == "" {
			return fmt.Errorf("%s 验证失败: 命令未返回版本或状态信息", check.stage)
		}
		fmt.Println(result)
	}
	return nil
}
