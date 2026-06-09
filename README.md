# snail_tool

`snail_tool` 是由原 `snail_tool.sh` 重写而来的 Go 版本，保留原有交互式菜单，并按功能模块拆分，方便后续扩展和维护。

## 功能

- 配置当前用户 SSH 公钥登录、禁用密码登录、设置 SSH 端口
- 配置当前用户 Vim `~/.vimrc`
- 配置当前用户 Bash 环境
- 配置当前用户 HTTP/HTTPS 代理环境变量

## 构建

```bash
go build -o snail_tool ./cmd/snail_tool
```

## 运行

```bash
sudo ./snail_tool
```

## 结构

```text
cmd/snail_tool      程序入口
internal/app        交互菜单和流程编排
internal/config     SSH、Vim、Bash、Proxy 等功能模块
internal/system     系统命令、用户、端口、文件辅助能力
internal/ui         输入、确认、暂停等交互封装
internal/log        彩色日志输出
```
