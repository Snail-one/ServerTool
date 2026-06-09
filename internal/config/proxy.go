package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

const (
	proxyBegin = "# ===== BEGIN PROXY CONFIG ====="
	proxyEnd   = "# ===== END PROXY CONFIG ====="
	noProxy    = "localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
)

var (
	proxyWithAuth = regexp.MustCompile(`^([^:]+):([^@]+)@([^:]+):([0-9]+)$`)
	proxyPlain    = regexp.MustCompile(`^([^:]+):([0-9]+)$`)
)

func ConfigureProxy(view *ui.UI) error {
	account, err := system.CurrentTargetUser()
	if err != nil {
		return err
	}

	fmt.Println("\033[32m[INFO]\033[0m 配置代理环境变量")
	fmt.Println()
	fmt.Println("\033[32m[INFO]\033[0m ip:port 格式，或 username:password@ip:port 格式")
	fmt.Println()

	raw, err := view.Ask("请输入代理地址: ")
	if err != nil {
		return err
	}

	proxyURL, err := normalizeProxy(raw)
	if err != nil {
		return err
	}

	bashrc := filepath.Join(account.Home, ".bashrc")
	if err := ensureFile(bashrc); err != nil {
		return err
	}
	if err := writeProxyBlock(bashrc, proxyURL); err != nil {
		return err
	}
	if err := system.ChownPath(bashrc, account, false); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("代理配置完成")
	fmt.Println()
	fmt.Printf("代理地址：%s\n", proxyURL)
	fmt.Println()
	fmt.Println("立即生效请执行：")
	fmt.Println("source ~/.bashrc")
	return nil
}

func normalizeProxy(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("错误：代理地址不能为空")
	}

	port := ""
	if match := proxyWithAuth.FindStringSubmatch(raw); match != nil {
		port = match[4]
	} else if match := proxyPlain.FindStringSubmatch(raw); match != nil {
		port = match[2]
	} else {
		return "", fmt.Errorf("错误：代理格式不正确\n\n支持格式：\n127.0.0.1:8888\n192.168.1.1:8888\nadmin:123456@192.168.1.1:8888")
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("错误：代理端口范围必须是 1-65535")
	}

	return "http://" + raw, nil
}

func writeProxyBlock(path, proxyURL string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(proxyBegin) + `.*?` + regexp.QuoteMeta(proxyEnd) + `\n?`)
	content := re.ReplaceAllString(string(data), "")
	block := fmt.Sprintf(`
%s

export http_proxy="%s"
export https_proxy="%s"

export HTTP_PROXY="%s"
export HTTPS_PROXY="%s"

export no_proxy="%s"
export NO_PROXY="%s"

%s
`, proxyBegin, proxyURL, proxyURL, proxyURL, proxyURL, noProxy, noProxy, proxyEnd)

	return os.WriteFile(path, []byte(content+block), 0644)
}
