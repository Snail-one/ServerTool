package golang

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

const downloadBase = "https://go.dev/dl/"

var downloadClient = &http.Client{Timeout: 30 * time.Minute}

func downloadArchive(url, expectedSHA string) (string, error) {
	log.Info("访问官方下载地址：", url)
	response, err := downloadClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("下载 Go 归档失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 Go 归档返回 HTTP %d", response.StatusCode)
	}
	log.Info("官方下载响应成功：", response.Status, "，开始接收文件...")
	file, err := os.CreateTemp(installRoot, ".download-*.tar.gz")
	if err != nil {
		return "", err
	}
	path := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	written, err := ui.CopyWithProgress(
		io.MultiWriter(file, hash),
		response.Body,
		os.Stdout,
		"下载 Go "+filepath.Base(url),
		response.ContentLength,
	)
	if err != nil {
		return "", fmt.Errorf("保存 Go 归档失败: %w", err)
	}
	log.Info("下载完成，接收字节数：", written)
	if err := file.Close(); err != nil {
		return "", err
	}
	log.Info("计算并核对官方 SHA-256...")
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expectedSHA) {
		return "", fmt.Errorf("Go 归档 SHA-256 校验失败：期望 %s，实际 %s", expectedSHA, actual)
	}
	log.Info("SHA-256 校验通过：", actual)
	keep = true
	return path, nil
}

func extractGoArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	cleanRoot := filepath.Clean(destination) + string(os.PathSeparator)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, cleanRoot) {
			return fmt.Errorf("归档包含不安全路径：%s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, reader)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			linkTarget := filepath.Clean(filepath.Join(filepath.Dir(target), header.Linkname))
			if !strings.HasPrefix(linkTarget, cleanRoot) {
				return fmt.Errorf("归档包含不安全软链接：%s", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
}
