# snail_tool

`snail_tool` 是由原 `snail_tool.sh` 重写而来的 Go 版本，保留原有交互式菜单，并按功能模块拆分，方便后续扩展和维护。

## 功能

- 容器管理：检测 Docker/Podman，并在二者并存时明确优先使用 Docker；容器操作以真实子命令显示，支持 `start`、`stop`、`restart`、`pause`/`unpause`、`inspect`、`logs`、`logs -f`、`exec`、Compose `down` 和非强制 `rm`；Compose 项目支持 `up -d`、`stop`、`restart`、不删除卷的 `down`，以及项目扫描、批量更新和重建；Docker 服务配置支持代理和日志轮转；资源清理按影响展示各类 prune 命令并逐次确认；卸载运行时可选择保留数据，完全卸载则经过强确认后永久删除对应数据
- SSH 管理：管理当前用户 SSH 公钥（查看、添加、删除）、写入 SSH 随机端口与禁用密码登录等安全配置、查看当前 SSH 生效安全配置
- 系统与用户配置：集中管理 Vim `~/.vimrc`、Bash、HTTP/HTTPS 代理环境变量和 UPS（NUT）配置
- 开发环境管理：从 Go 官方 API 获取全部稳定版本，在 `/opt/go` 安装、更新、切换和卸载 amd64/arm64 Go，并为目标用户配置 PATH
- 清理本工具配置：支持按项清理 SSH、Vim、Bash、代理配置，或在最后一项清理全部

主菜单会显示容器运行时、SSH 配置、系统与用户配置数量和当前 Go 版本。所有二级菜单使用 `ServerTool > 功能 > 子功能` 路径标题，并统一以 `0/q) 返回`（同时兼容 `exit`）退出当前菜单；主菜单使用 `0/q) 退出`。直接对应 CLI 的维护操作采用“命令 — 影响说明”格式，空输入或无效输入不会执行容器清理。

一键下载安装

```bash
sudo wget -O /usr/local/sbin/snail https://github.com/Snail-one/ServerTool/releases/latest/download/snailtool_linux_amd64 && sudo chmod +x /usr/local/sbin/snail
```

```bash
sudo curl -L -o /usr/local/sbin/snail https://github.com/Snail-one/ServerTool/releases/latest/download/snailtool_linux_amd64 && sudo chmod +x /usr/local/sbin/snail
```

## Go 环境管理

从主菜单进入“开发环境管理 → Go 语言”后，可以安装任意官方稳定版本、更新到最新稳定版、切换当前版本、卸载指定版本、重新下载安装当前版本并修复 PATH，或清理异常中断遗留的下载、解压、修复和备份文件。安装版本列表每页显示 10 个，可翻页选择；当前支持 Linux amd64 和 arm64。

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
internal/container  容器管理：容器列表与操作、Compose 项目、Docker 服务配置、清理容器资源、安装运行时
internal/ssh        SSH 管理：公钥、安全配置、生效安全配置查看
internal/common     系统与用户配置：Vim、Bash、HTTP/HTTPS 代理、UPS（NUT）
internal/environment 开发环境管理：Go 官方多版本安装、更新、切换、卸载及用户 PATH 管理
internal/cleanup    清理本工具配置：按项或全部清理本工具写入的配置
internal/status     菜单状态检测汇总
internal/shared     跨菜单复用的小型辅助能力
internal/system     系统命令、用户、端口、文件辅助能力
internal/ui         输入、确认、暂停等交互封装
internal/log        彩色日志输出
```
