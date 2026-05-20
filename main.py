#!/usr/bin/env python3

import os
import sys

from core.logger import log_error
from core.menu import show_menu, pause
from modules.ssh_config import config_ssh
from modules.vim_config import config_vim
from modules.bash_config import config_bash
from modules.proxy_config import config_proxy


def is_debian_or_ubuntu():
    os_release_paths = ["/etc/os-release", "/usr/lib/os-release"]

    for path in os_release_paths:
        if not os.path.exists(path):
            continue

        os_id = ""
        os_like = ""

        with open(path, "r", encoding="utf-8", errors="ignore") as file:
            for line in file:
                line = line.strip()

                if not line or line.startswith("#") or "=" not in line:
                    continue

                key, value = line.split("=", 1)
                value = value.strip().strip('"').lower()

                if key == "ID":
                    os_id = value
                elif key == "ID_LIKE":
                    os_like = value

        if os_id in {"debian", "ubuntu"}:
            return True

        if "debian" in os_like.split() or "ubuntu" in os_like.split():
            return True

    return False


def main():
    if os.geteuid() != 0:
        log_error("请使用 sudo 或 root 运行此脚本")
        sys.exit(1)

    if not is_debian_or_ubuntu():
        log_error("当前系统不是 Debian 或 Ubuntu，脚本已退出")
        sys.exit(1)

    while True:
        os.system("clear")

        show_menu()

        choice = input("\n输入选项: ").strip()

        if choice == "1":
            config_ssh()
            pause()

        elif choice == "2":
            config_vim()
            pause()

        elif choice == "3":
            config_bash()
            pause()

        elif choice == "4":
            config_proxy()
            pause()

        elif choice.lower() in ["q", "exit"]:
            print("已退出")
            break

        else:
            print("无效选项，请重新输入")
            pause()


if __name__ == "__main__":
    main()
