# ServerTool CLI 参数说明

本文说明 ServerTool 主程序和安装脚本支持的命令、参数、环境变量与退出状态。

安装后的默认命令名是 `snail`；直接运行仓库构建产物时，命令通常是 `./snail_tool`。下文以 `snail` 表示，两者参数相同。如果通过 `SERVERTOOL_BINARY_NAME` 修改了安装名称，请替换示例中的 `snail`。

## 1. 主程序

### 1.1 命令格式

```text
snail
snail <command>
```

### 1.2 命令一览

| 命令 | 别名 | 是否需要 root | 说明 |
| --- | --- | --- | --- |
| `snail` | 无 | 是 | 启动交互式服务器管理菜单 |
| `snail help` | `--help`、`-h` | 否 | 显示命令行用法 |
| `snail version` | `--version`、`-v` | 否 | 显示版本、提交、构建时间、Go 版本和目标平台 |
| `snail update` | 无 | 是 | 下载仓库安装脚本并安装或更新到最新正式版 |
| `snail uninstall` | `--uninstall` | 是 | 下载仓库安装脚本并卸载主程序 |

未知命令会显示错误和用法，并以状态码 `2` 退出。

### 1.3 交互式菜单

```bash
sudo snail
```

不带参数时启动交互式菜单。菜单中的系统配置、软件安装和服务管理操作需要 root 权限；通过 `sudo` 启动时，需要写入用户目录的功能会以 `SUDO_USER` 作为目标用户。

### 1.4 查看帮助

以下命令等价：

```bash
snail help
snail --help
snail -h
```

帮助命令不需要 root 权限。

### 1.5 查看版本

以下命令等价：

```bash
snail version
snail --version
snail -v
```

输出格式：

```text
snailtool v1.2.0
commit: abcdef0
build date: 2026-08-14T01:02:03Z
go: go1.26.0
platform: linux/amd64
```

版本命令不需要 root 权限。

### 1.6 更新

```bash
sudo snail update
```

更新命令执行以下流程：

1. 从仓库主分支下载 `scripts/install.sh`。
2. 优先通过 GitHub Releases API 获取最新正式版本。
3. API 不可用时，通过最新 Release 的 `checksums.txt` 解析版本。
4. 比较当前版本及本地程序 SHA-256。
5. 需要更新或修复时，下载当前平台的发布文件。
6. 校验 SHA-256 和程序版本后原子替换当前命令。

`snail update` 只更新到最新正式版，不接受版本参数。安装指定版本请使用安装脚本。

### 1.7 卸载

以下命令等价：

```bash
sudo snail uninstall
sudo snail --uninstall
```

卸载只删除安装后的主程序文件，默认位置为 `/usr/local/sbin/snail`。它不会删除通过 ServerTool 配置的 SSH、容器服务、用户环境或 UPS 配置。

## 2. 安装脚本

### 2.1 参数格式

本地脚本：

```text
sudo sh scripts/install.sh [VERSION]
sudo sh scripts/install.sh uninstall
sudo sh scripts/install.sh --uninstall
sh scripts/install.sh --help
```

远程脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh | sudo sh
```

### 2.2 安装或更新到最新版本

```bash
sudo sh scripts/install.sh
```

不提供版本参数时，默认安装或更新到 `latest`。

### 2.3 安装指定版本

```bash
sudo sh scripts/install.sh v1.2.0
```

通过管道执行时：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh \
  | sudo sh -s -- v1.2.0
```

版本仅允许字母、数字、点、下划线和连字符。指定版本参数的优先级高于 `SERVERTOOL_VERSION` 环境变量。

### 2.4 卸载

以下参数等价：

```bash
sudo sh scripts/install.sh uninstall
sudo sh scripts/install.sh --uninstall
```

远程执行：

```bash
curl -fsSL https://raw.githubusercontent.com/Snail-one/ServerTool/main/scripts/install.sh \
  | sudo sh -s -- --uninstall
```

### 2.5 帮助

```bash
sh scripts/install.sh --help
sh scripts/install.sh -h
```

查看帮助不需要 root 权限。

## 3. 安装脚本环境变量

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVERTOOL_VERSION` | `latest` | 未提供位置参数时要安装的 Release 标签 |
| `SERVERTOOL_INSTALL_DIR` | `/usr/local/sbin` | 安装目录，必须是绝对路径 |
| `SERVERTOOL_BINARY_NAME` | `snail` | 安装后的命令名，不能为空且不能包含 `/` |

示例：

```bash
sudo SERVERTOOL_VERSION=v1.2.0 \
  SERVERTOOL_INSTALL_DIR=/opt/servertool/bin \
  SERVERTOOL_BINARY_NAME=servertool \
  sh scripts/install.sh
```

安装结果为：

```text
/opt/servertool/bin/servertool
```

### 3.1 代理变量

下载遵循常见代理环境变量：

```text
HTTPS_PROXY  https_proxy
HTTP_PROXY   http_proxy
ALL_PROXY    all_proxy
```

通过代理访问 GitHub 失败时，脚本会尝试直连一次。使用 `sudo` 时是否保留代理变量取决于系统的 sudo 配置；必要时可以显式传入：

```bash
sudo HTTPS_PROXY=http://127.0.0.1:7890 sh scripts/install.sh
```

### 3.2 输出颜色

| 环境变量 | 效果 |
| --- | --- |
| `NO_COLOR` | 禁用 ANSI 颜色输出 |
| `FORCE_COLOR=1` | 即使输出不是终端也强制保留 ANSI 颜色 |

`NO_COLOR` 的优先级高于 `FORCE_COLOR=1`。

## 4. 权限与退出状态

| 状态码 | 含义 |
| --- | --- |
| `0` | 命令成功完成 |
| `1` | 权限不足、下载失败、校验失败、安装失败或其他运行错误 |
| `2` | 主程序收到未知命令 |

`help` 和 `version` 不需要 root；交互菜单、`update`、`uninstall` 以及安装脚本的安装和卸载操作需要 root。

在自动化脚本中应检查退出状态，例如：

```bash
if sudo snail update; then
    echo "ServerTool 更新成功"
else
    echo "ServerTool 更新失败" >&2
    exit 1
fi
```
