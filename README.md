# snail_tool

`snail_tool` 是由原 `snail_tool.sh` 重写而来的 Go 版本，保留原有交互式菜单，并按功能模块拆分，方便后续扩展和维护。

## 功能

- 容器管理：检测 Docker/Podman，缺失时可选择安装 Docker 或 Podman；容器列表和 Compose 项目列表显示当前状态，并支持启动、停止、重启、查看日志和实时日志；批量扫描并更新运行中的 Docker Compose 应用，检测到 `build:` 的项目会直接跳过；支持容器无用资源一键清理和按容器、网络、镜像、构建缓存单项清理
- SSH 管理：管理当前用户 SSH 公钥（查看、添加、删除）、写入 SSH 随机端口与禁用密码登录等安全配置、查看当前 SSH 生效安全配置
- 集中写入配置文件：Vim `~/.vimrc`、Bash 环境、HTTP/HTTPS 代理环境变量、UPS(NUT) 配置
- 清理本工具写入的 SSH、Vim、Bash、代理配置，支持一键清理或按项清理

一键下载安装

```bash
sudo wget -O /usr/local/sbin/snailtool https://github.com/Snail-one/ServerTool/releases/latest/download/snailtool_linux_amd64 && sudo chmod +x /usr/local/sbin/snailtool
```

```bash
sudo curl -L -o /usr/local/sbin/snailtool https://github.com/Snail-one/ServerTool/releases/latest/download/snailtool_linux_amd64 && sudo chmod +x /usr/local/sbin/snailtool
```

## 构建

```bash
go build -o snail_tool ./cmd/snail_tool
```

### 一键编译

Windows：

```powershell
.\build_windows.ps1
```

默认会交叉编译出 Linux 二进制，输出为 `dist/snail_tool_linux_amd64`。

Linux：

```bash
bash ./build_linux.sh
```

默认输出到 `dist/` 目录。

## 自动发布

在 GitHub 上推送 `v*` 标签后，Actions 会自动交叉编译 Linux 版本并发布到仓库 Release。

示例：

```bash
git tag v1.0.0
git push origin v1.0.0
```

也可以在 GitHub Actions 页面手动触发，并填写 `tag_name` 后发布。

## 运行

```bash
sudo ./snail_tool
```

查看版本不需要 root：

```bash
./snail_tool --version
```

## 结构

```text
cmd/snail_tool      程序入口
internal/app        交互菜单和流程编排
internal/container  容器管理：更新 Compose 应用、清理容器资源、安装运行时
internal/ssh        SSH 管理：公钥、安全配置、生效安全配置查看
internal/common     常用配置：Vim、Bash、HTTP/HTTPS 代理、UPS
internal/cleanup    清理配置：一键清理或按项清理本工具写入的配置
internal/status     菜单状态检测汇总
internal/shared     跨菜单复用的小型辅助能力
internal/system     系统命令、用户、端口、文件辅助能力
internal/ui         输入、确认、暂停等交互封装
internal/log        彩色日志输出
```
