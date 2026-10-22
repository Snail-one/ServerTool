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
- 工具持久数据统一使用 `/var/lib/snailtool/`，防火墙、SSH、Docker 和 UPS 的备份分别归入 `backups/` 下对应模块目录，并保留来源路径。目录按需创建；备份和恢复仅使用新位置，不迁移旧备份。旧位置备份可通过“清理备份 → 清理旧备份”查看完整清单，确认后删除。系统服务配置仍保留在各自原位置。
- 防火墙、SSH、Docker 对每个来源配置文件保留最近 10 份历史备份，新备份保存成功后自动淘汰超额旧备份；UPS 首次备份单独保留。

```bash
snail --help
snail --version
```

## 功能概览

| 模块 | 主要能力 |
| --- | --- |
| 防火墙管理 | 管理 nftables 配置及端口、来源 IP、来源 IP + 端口放行规则，列出、编辑、调整顺序和精确删除规则；控制外部 ICMP / ICMPv6 回显请求（Ping）；实时查看完整规则及流量计数，支持切换规则表；支持开关规则中的数据包及字节统计；汇总已加载端口及来源例外；自动联动 SSH 端口放行；一键移除本工具规则并恢复首次快照中的配置和开机自启 |
| SSH 管理 | 查看、添加和删除公钥，配置随机端口、禁用密码登录，查看生效配置 |
| 配置管理 | 一键初始化 SSH 公钥、安全策略、Vim 和 Bash；单独管理 Vim、Bash、Bash 自动补全、HTTP/HTTPS 代理和 tmux 状态栏 |
| 系统工具 | 单独或批量安装常用命令行工具，安装、更新、切换、修复和卸载 Go 官方稳定版本并管理用户 PATH，配置 UPS（NUT），查看本次启动信息 |
| 容器管理 | 管理 Docker/Podman 容器和 Compose 项目，配置 Docker 代理与日志轮转，清理资源 |
| 清理配置 | 按项或全部清理 SSH、Vim、Bash、代理、tmux、防火墙、UPS、Docker、Go 环境配置，支持统一备份及旧备份清理 |

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

**卸载时询问是否删除整个 `/var/lib/snailtool` 数据目录，默认保留，然后删除程序文件。** 选择删除会一并移除全部备份、首次快照、锁文件、隐藏文件及子目录；目录删除失败时停止卸载并保留程序。选择保留或未收到回答时，数据及备份均不删除；输入 `0/q` 取消整个卸载。实际生效的 SSH、容器服务、用户环境和 UPS 配置保持原状。需要清理本工具写入的配置时，可先使用菜单中的“清理配置”；“清理全部本工具配置”确认后也会清理全部备份。

“清理全部本工具配置”涵盖 SSH、Vim、Bash、代理、tmux、防火墙、UPS、Docker 和 Go 环境配置。确认页面展示服务与网络影响；配置清理全部成功后才删除统一备份。软件、Go 安装版本和容器数据使用各自独立的卸载或清理入口；旧位置备份需查看清单并单独确认。

“清理配置 → 13 删除软件数据目录”可在确认后直接删除整个 `/var/lib/snailtool`，包含备份、首次快照、锁文件、隐藏文件和子目录。现有系统配置、主程序和其他软件保持原状；需要恢复配置时应先执行配置清理。

更多安装方式、环境变量和退出状态见 [CLI 参数说明](docs/CLI.md)。

## 使用说明

- **一键初始化**：没有已有 SSH 公钥时，必须成功添加一把公钥才会继续安全加固。
- **容器运行时**：Docker 和 Podman 同时存在时，容器操作优先使用 Docker，资源清理可选择运行时。资源清理逐次确认，数据卷需单独选择清理并确认永久删除卷内数据；完全卸载运行时并删除数据需要强确认。Docker 完全卸载还会删除 docker 用户组及成员关系，保留用户账号；仅卸载运行时则保留该组。
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
go build -o dist/snail ./cmd/snail_tool
sudo ./dist/snail
```

源码构建产物 `./dist/snail` 与安装后的 `snail` 使用相同参数。

### 构建脚本

```bash
bash ./scripts/build_linux.sh
sudo ./dist/snail
```

脚本使用当前 Go 环境的架构构建 Linux 二进制，固定输出为 `dist/snail`；每次构建更新同一个文件，版本信息可通过 `./dist/snail --version` 查看。

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
internal/quicksetup/  一键初始化
internal/ssh/         SSH 公钥与安全配置
internal/common/      配置管理菜单及 Vim、Bash、代理、tmux、UPS 配置
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
