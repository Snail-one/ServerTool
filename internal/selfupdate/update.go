package selfupdate

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"snail_tool/internal/ui"
)

const (
	InstallScriptURL = "https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh"
	maxScriptSize    = 1024 * 1024
)

// Run downloads the repository installer and delegates the requested action to it.
func Run(arguments ...string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	return run(client, InstallScriptURL, os.Stdin, os.Stdout, os.Stderr, arguments...)
}

func run(client *http.Client, scriptURL string, stdin io.Reader, stdout, stderr io.Writer, arguments ...string) error {
	response, err := client.Get(scriptURL)
	if err != nil {
		return fmt.Errorf("下载程序管理脚本失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载程序管理脚本失败: HTTP %d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}

	var downloaded bytes.Buffer
	_, err = ui.CopyWithProgress(
		&downloaded,
		io.LimitReader(response.Body, maxScriptSize+1),
		stderr,
		"下载 ServerTool 更新脚本",
		response.ContentLength,
	)
	if err != nil {
		return fmt.Errorf("读取程序管理脚本失败: %w", err)
	}
	data := downloaded.Bytes()
	if len(data) > maxScriptSize {
		return fmt.Errorf("程序管理脚本超过 %d 字节限制", maxScriptSize)
	}
	if !bytes.HasPrefix(data, []byte("#!/bin/sh")) {
		return fmt.Errorf("下载内容不是有效的 ServerTool 安装脚本")
	}

	temporary, err := os.CreateTemp("", "servertool-action-*.sh")
	if err != nil {
		return fmt.Errorf("创建程序管理脚本临时文件失败: %w", err)
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置程序管理脚本权限失败: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("保存程序管理脚本失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("保存程序管理脚本失败: %w", err)
	}

	command := exec.Command("sh", append([]string{path}, arguments...)...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("程序管理脚本执行失败: %w", err)
	}
	return nil
}
