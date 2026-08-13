# snail_tool

`snail_tool` 是由原 `snail_tool.sh` 重写而来的 Go 版本，保留原有交互式菜单，并按功能模块拆分，方便后续扩展和维护。

命令、参数、环境变量、权限要求和退出状态详见 [`docs/CLI.md`](docs/CLI.md)。

## 功能

- 容器管理：检测 Docker/Podman，并在二者并存时明确优先使用 Docker；容器操作以真实子命令显示，支持 `start`、`stop`、`restart`、`pause`/`unpause`、`inspect`、`logs`、`logs -f`、`exec`、Compose `down` 和非强制 `rm`；Compose 项目支持 `up -d`、`stop`、`restart`、不删除卷的 `down`，以及项目扫描、批量更新和重建；Docker 服务配置支持代理和日志轮转；资源清理按影响展示各类 prune 命令并逐次确认；卸载运行时可选择保留数据，完全卸载则经过强确认后永久删除对应数据
- 一键配置：按顺序完成 SSH 公钥添加、SSH 安全策略、Vim 和 Bash 配置；没有已有公钥时必须成功添加一把公钥才会继续安全加固
- SSH 管理：管理当前用户 SSH 公钥（查看、添加、删除）、写入 SSH 随机端口与禁用密码登录等安全配置、查看当前 SSH 生效安全配置
- 系统与用户配置：集中管理 Vim `~/.vimrc`、Bash 和 HTTP/HTTPS 代理环境变量；普通用户的 Bash 提示符使用紫色用户名，root 使用纯橘色 `#FF7F00`，当前目录均为蓝色
- 开发环境：从 Go 官方 API 获取全部稳定版本，在 `/opt/go` 安装、更新、切换和卸载 amd64/arm64 Go，并为目标用户配置 PATH
- 清理配置：支持按项清理 SSH、Vim、Bash、代理配置，或在最后一项清理全部
- 系统工具：配置和管理 UPS（NUT）等独立服务器工具

主菜单使用固定状态列的彩色徽标显示工具版本、容器运行时、SSH 配置、系统与用户配置数量和当前 Go 版本。所有界面使用统一的橙色主视觉、`ServerTool › 功能 › 子功能` 路径标题、对齐的彩色快捷键和中文选择提示，并以 `0/q 返回`（同时兼容 `exit`）退出当前菜单；主菜单使用 `0/q 退出`。操作名称保持主文字，命令或影响说明统一显示为灰色 `-- 说明`，日志统一使用 `[信息]`、`[警告]` 和 `[错误]`。菜单和日志配色在非交互输出、`TERM=dumb` 或设置 `NO_COLOR` 时会自动关闭，空输入或无效输入不会执行容器清理。

## 一键安装或更新

跨项目统一采用的完整流程、命名、安全要求和验收清单见 [`docs/INSTALL_UPDATE_STANDARD.md`](docs/INSTALL_UPDATE_STANDARD.md)。

脚本会自动识别 Linux amd64/arm64，校验 Release 提供的 SHA-256 后安装到 `/usr/local/sbin/snail`。首次安装和后续更新使用同一条命令：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh
```

系统没有 curl 时：

```bash
wget -qO- https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh
```

直接安装指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh -s -- v1.2.0
```

也可以下载脚本后安装指定版本：

```bash
sudo sh scripts/install.sh v1.2.0
```

已安装后，可直接通过程序调用仓库中的安装脚本更新到最新版本：

```bash
sudo snail update
```

主程序也可以调用仓库中的安装脚本完成卸载。卸载只删除 `/usr/local/sbin/snail` 程序文件，不会回退通过本工具完成的 SSH、容器服务或用户环境配置：

```bash
sudo snail uninstall
```

也可以直接调用本地或远程安装脚本：

```bash
sudo sh scripts/install.sh --uninstall
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh -s -- --uninstall
```

更新时会比较当前版本并使用 Release 的 SHA-256 校验本地程序；版本和文件校验均一致时直接退出，文件损坏或内容不一致时会自动重新下载修复。GitHub Releases API 不可用时，安装脚本会通过最新 Release 的 `checksums.txt` 中的版本化二进制文件名解析目标版本，作为更新兜底。

ServerTool 发起的文件下载会显示实时进度、已接收大小和速度；服务器未返回文件总大小时，则显示已接收大小和速度。系统包管理器与 Docker Compose 的下载沿用其自身的实时进度输出。

Release 文件名会在末尾包含版本号，例如：

```text
snailtool_linux_amd64_v1.2.0
snailtool_linux_arm64_v1.2.0
checksums.txt
```

## Go 环境管理

从主菜单进入“开发环境管理 → Go 语言”后，可以安装任意官方稳定版本、更新到最新稳定版、切换当前版本、卸载指定版本、重新下载安装当前版本并修复 PATH，或清理异常中断遗留的下载、解压、修复和备份文件。Go 归档在安装、更新和修复时都会显示实时下载进度。安装版本列表每页显示 10 个，可翻页选择；当前支持 Linux amd64 和 arm64。

各版本保存在 `/opt/go/goX.Y.Z`，`/opt/go/current` 指向当前版本。旧版本会保留，卸载当前版本后会自动切换到剩余版本中版本号最高的一个。除下述经用户确认的迁移外，工具只管理 `/opt/go` 下的版本。

如果检测到 `/usr/local/go/bin/go`，或目标用户 `~/.bashrc` 中存在引用 `/usr/local/go` 的 `PATH`、`GOROOT` 赋值，安装或更新时会提示迁移。只有用户确认且 `/opt/go` 安装成功后，才会删除 `/usr/local/go` 及这些环境变量行；也可以从卸载列表直接选择“官方位置 Go”单独清理。注释和其他 Bash 配置保持不变，系统包管理器安装的 Go 不会被自动卸载。

PATH 配置写入 sudo 发起用户的 `~/.bashrc`。安装或切换后请重新登录，或者执行：

```bash
source ~/.bashrc
```

## 构建

```bash
go build -o snail_tool ./cmd/snail_tool
```

### 一键编译

Windows：

```powershell
.\scripts\build_windows.ps1
```

默认会交叉编译出 Linux 二进制，输出为 `dist/snailtool_linux_amd64_<版本>`。

Linux：

```bash
bash ./scripts/build_linux.sh
```

默认输出到 `dist/` 目录。

## 自动发布

完整的 CI/CD Job 依赖、版本注入、构建矩阵、发布权限、失败恢复和验收规范见 [`docs/CICD_STANDARD.md`](docs/CICD_STANDARD.md)。

在 GitHub 上推送 `v*` 标签后，Actions 会自动交叉编译 Linux 版本并发布到仓库 Release。发布流程先通过 `git log` 将相邻版本间直接推送的提交整理成 Markdown 列表，再由 GitHub 原生 Automatically generated release notes 补充合并的 PR、贡献者及完整变更链接，适用于个人直接维护和 PR 两种工作方式。

示例：

```bash
git tag v1.0.0
git push origin v1.0.0
```

也可以在 GitHub Actions 页面手动触发，并填写 `tag_name` 后发布。

## 运行

完整 CLI 参数说明见 [`docs/CLI.md`](docs/CLI.md)。

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
internal/container  容器管理：容器列表与操作、Compose 项目、Docker 服务配置、清理容器资源、安装运行时
internal/ssh        SSH 管理：公钥、安全配置、生效安全配置查看
internal/common     系统与用户配置：Vim、Bash、HTTP/HTTPS 代理
internal/environment 开发环境管理：Go 官方多版本安装、更新、切换、卸载及用户 PATH 管理
internal/cleanup    清理本工具配置：按项或全部清理本工具写入的配置
internal/toolbox    系统工具菜单：UPS（NUT）等服务器工具
internal/status     菜单状态检测汇总
internal/shared     跨菜单复用的小型辅助能力
internal/system     系统命令、用户、端口、文件辅助能力
internal/ui         输入、确认、暂停等交互封装
internal/log        彩色日志输出
scripts             安装、更新及跨平台构建脚本
```
