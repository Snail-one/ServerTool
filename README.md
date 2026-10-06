# ServerTool

ServerTool 是一个面向 Linux 服务器的交互式管理工具，提供 SSH 配置、容器管理、Go 多版本管理和常用系统工具安装。使用 Go 编写，由原 `snail_tool.sh` 重写而来，保留中文交互菜单，并按功能模块组织代码。

支持 Linux **amd64 / arm64**。安装后的命令为 `snail`，通过 `sudo snail` 进入菜单。

## 快速开始

### 安装并启动

安装或更新到最新正式版：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh
```

系统没有 curl 时：

```bash
wget -qO- https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh
```

安装脚本会自动识别架构，校验 Release 提供的 SHA-256，并安装到 `/usr/local/sbin/snail`。

启动交互菜单：

```bash
sudo snail
```

### 使用要求

- 交互菜单、安装、更新和卸载需要 root 权限；帮助和版本查询无需 root。
- 通过 `sudo` 启动时，写入用户目录的功能以 `SUDO_USER` 为目标用户。
- 菜单使用 `0/q` 返回或退出，同时兼容 `exit`。

```bash
snail --help
snail --version
```

## 功能概览

| 模块 | 主要能力 |
| --- | --- |
| 一键配置 | 按顺序添加 SSH 公钥、配置 SSH 安全策略、Vim 和 Bash |
| SSH 管理 | 查看、添加和删除公钥，配置随机端口、禁用密码登录，查看生效配置 |
| 通用配置 | 管理 Vim、Bash 和 HTTP/HTTPS 代理环境变量 |
| 系统工具 | 配置 UPS（NUT），单独或批量安装常用命令行工具，查看本次启动信息 |
| 开发环境 | 安装、更新、切换、修复和卸载 Go 官方稳定版本，管理用户 PATH |
| 容器管理 | 管理 Docker/Podman 容器和 Compose 项目，配置 Docker 代理与日志轮转，清理资源 |
| 清理配置 | 按项或全部清理本工具写入的 SSH、Vim、Bash 和代理配置 |

详细操作与行为说明见 [使用指南](docs/USAGE.md)。

## 更新与卸载

更新到最新正式版：

```bash
sudo snail update
```

安装指定版本（以下以 `v1.2.0` 为例，请替换为需要的 Release 标签）：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh -s -- v1.2.0
```

卸载程序：

```bash
sudo snail uninstall
```

**卸载仅删除程序文件，不会回退通过本工具完成的 SSH、容器服务、用户环境或 UPS 配置。** 需要清理本工具写入的配置时，可先使用菜单中的“清理配置”。

更多安装方式、环境变量和退出状态见 [CLI 参数说明](docs/CLI.md)。

## 使用说明

- **一键配置**：没有已有 SSH 公钥时，必须成功添加一把公钥才会继续安全加固。
- **容器运行时**：Docker 和 Podman 同时存在时优先使用 Docker。资源清理逐次确认；完全卸载运行时并删除数据需要强确认。
- **Go 环境**：各版本保存在 `/opt/go/goX.Y.Z`，由 `/opt/go/current` 指向当前版本，旧版本会保留。安装或切换后需重新登录或执行 `source ~/.bashrc`；命令软链接可在 Go 菜单中手动创建。
- **状态检测**：启动检测仅执行一次，首页状态使用启动时结果。“系统工具 → 查看本次启动信息”可查看检测结果与耗时；工具安装页面提供独立的安装状态刷新。

Go 迁移、PATH、软链接、工具安装状态及无色输出等细节见 [使用指南](docs/USAGE.md)。

## 文档导航

| 文档 | 内容 |
| --- | --- |
| [使用指南](docs/USAGE.md) | 功能细节、菜单交互、启动检测、Go 环境与下载行为 |
| [CLI 参数说明](docs/CLI.md) | 命令、安装脚本参数、环境变量、权限和退出状态 |
| [安装更新规范](docs/INSTALL_UPDATE_STANDARD.md) | 安装更新流程、命名、安全要求和验收清单 |
| [CI/CD 规范](docs/CICD_STANDARD.md) | Job 依赖、版本注入、构建矩阵、发布权限和失败恢复 |

## 开发与构建

源码构建需要 Go 1.22 或更高版本。

### 本地构建

```bash
go build -o snail_tool ./cmd/snail_tool
sudo ./snail_tool
```

源码构建产物 `./snail_tool` 与安装后的 `snail` 使用相同参数。

### 构建脚本

Linux：

```bash
bash ./scripts/build_linux.sh
```

Windows（PowerShell）：

```powershell
.\scripts\build_windows.ps1
```

两个脚本均输出 Linux 二进制到 `dist/`，文件名为 `snailtool_linux_<架构>_<版本>`。Linux 脚本使用当前 Go 环境的架构，Windows 脚本默认使用 amd64，可通过 `-GoArch arm64` 切换。

### 验证

```bash
go test ./...
```

### 自动发布

推送 `v*` 标签后，GitHub Actions 会先运行测试，再构建 Linux amd64/arm64 二进制并发布到 GitHub Releases，同时提供 `checksums.txt`。

```bash
git tag v1.0.0
git push origin v1.0.0
```

也可在 GitHub Actions 页面手动触发，填写 `tag_name` 发布。发布说明将直接推送的提交整理为 Markdown 列表，并结合 GitHub 自动生成的 PR、贡献者与完整变更链接。完整流程见 [CI/CD 规范](docs/CICD_STANDARD.md)。

### 项目结构

```text
cmd/snail_tool/       程序入口
internal/app/         交互菜单与流程编排
internal/quicksetup/  一键配置
internal/ssh/         SSH 公钥与安全配置
internal/common/      Vim、Bash、代理及 UPS 配置
internal/container/   容器与 Compose 管理
internal/environment/ Go 环境管理
internal/toolbox/     系统工具与启动信息
internal/cleanup/     清理本工具配置
internal/status/      菜单状态检测
internal/startup/     本次启动信息记录
internal/selfupdate/  程序更新与卸载入口
internal/shared/      跨模块辅助能力
internal/system/      系统命令、用户、端口与文件操作
internal/ui/          菜单、输入、确认与进度显示
internal/log/         日志输出
internal/version/     版本与构建信息
scripts/              安装、更新、构建与发布说明脚本
docs/                 使用说明与开发规范
```

### 开发计划

[首页“常用”与服务器体检方案](docs/SERVER_HEALTH_DEVELOPMENT.md)为待开发设计，不代表当前已实现的功能。
