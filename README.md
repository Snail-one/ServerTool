# server_setup_tool

这是从原 Bash 脚本拆分出来的 Python 模块化版本。

## 目录结构

```text
server_setup_tool/
├── main.py
├── core/
│   ├── __init__.py
│   ├── logger.py
│   ├── menu.py
│   └── system.py
└── modules/
    ├── __init__.py
    ├── ssh_config.py
    ├── vim_config.py
    ├── bash_config.py
    └── proxy_config.py
```

## 运行方式

```bash
cd server_setup_tool
sudo python3 main.py
```

## 功能说明

- `modules/ssh_config.py`：配置 SSH 公钥登录、禁用密码登录、随机 SSH 端口。
- `modules/vim_config.py`：安装 Vim 并写入 `~/.vimrc`。
- `modules/bash_config.py`：配置 Bash alias。
- `modules/proxy_config.py`：配置 HTTP/HTTPS 代理环境变量。
- `core/logger.py`：彩色日志。
- `core/system.py`：系统工具函数。
- `core/menu.py`：菜单和暂停逻辑。
