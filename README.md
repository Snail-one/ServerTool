# snail_tool

`snail_tool` 是由原 `snail_tool.sh` 重写而来的 Go 版本，保留原有交互式菜单，并按功能模块拆分，方便后续扩展和维护。

## 功能

- 批量扫描并更新运行中的 Docker Compose 应用，检测到 `build:` 的项目会直接跳过，默认扫描 `/docker`、`/opt/docker`、`/opt/apps`、用户目录、用户目录下的 `docker`，更新完成后可选择 Docker 清理策略
- 管理当前用户 SSH 公钥（查看、添加、删除）
- 集中写入配置文件：SSH 随机端口与禁用密码登录等安全配置、Vim `~/.vimrc`、Bash 环境、HTTP/HTTPS 代理环境变量、UPS(NUT) 配置
- 清理本工具写入的 SSH、Vim、Bash、代理配置，支持一键清理或按项清理

一键下载安装

```bash
sudo -E wget -O /usr/local/sbin/snailtool https://github.com/Snail-one/ServerTool/releases/latest/download/snailtool_linux_amd64 && sudo chmod +x /usr/local/sbin/snailtool
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

## 结构

```text
cmd/snail_tool      程序入口
internal/app        交互菜单和流程编排
internal/config     SSH、Vim、Bash、Proxy、UPS 等功能模块
internal/system     系统命令、用户、端口、文件辅助能力
internal/ui         输入、确认、暂停等交互封装
internal/log        彩色日志输出
```
