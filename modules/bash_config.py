import os
import re
from pathlib import Path

from core.logger import log_info
from core.system import get_real_user, get_user_home, run_command


ROOT_BASHRC = """# ~/.bashrc: executed by bash(1) for non-login shells.

PS1='${debian_chroot:+($debian_chroot)}\\u@\\h:\\w\\$ '
PS1='\\[\\033[38;5;196m\\]\\u@\\h\\[\\033[00m\\]:\\[\\033[01;34m\\]\\w\\[\\033[00m\\]\\$ '

export LS_OPTIONS='--color=auto'
eval "$(dircolors)"
alias ls='ls $LS_OPTIONS'
alias ll='ls $LS_OPTIONS -l'
alias l='ls $LS_OPTIONS -lA'

alias rm='rm -i'
alias cp='cp -i'
alias mv='mv -i'
"""


def replace_or_append_alias(content, alias_name, alias_value):
    pattern = rf"^[ \t]*#?[ \t]*alias {re.escape(alias_name)}=.*$"
    replacement = f"alias {alias_name}='{alias_value}'"

    if re.search(pattern, content, flags=re.MULTILINE):
        return re.sub(pattern, replacement, content, flags=re.MULTILINE)

    return content.rstrip() + "\n" + replacement + "\n"


def config_bash():
    real_user = get_real_user()
    user_home = get_user_home(real_user)
    bashrc = Path(user_home) / ".bashrc"

    log_info("配置 Bash 环境...")
    print(f"当前用户：{real_user}")
    print(f"配置文件：{bashrc}")

    bashrc.touch(exist_ok=True)

    if real_user == "root":
        bashrc.write_text(ROOT_BASHRC, encoding="utf-8")
    else:
        content = bashrc.read_text(encoding="utf-8", errors="ignore")

        content = replace_or_append_alias(content, "ll", "ls -l")
        content = replace_or_append_alias(content, "la", "ls -A")
        content = replace_or_append_alias(content, "l", "ls -lahF")

        bashrc.write_text(content, encoding="utf-8")

    run_command(f"chown {real_user}:{real_user} {bashrc}")
    os.chmod(bashrc, 0o644)

    print()
    print("已经修改 ~/.bashrc，新的别名配置如下：")
    print("alias ll='ls -l'")
    print("alias la='ls -A'")
    print("alias l='ls -lahF'")
    print("Bash 配置完成")

    return True
