import os
from pathlib import Path

from core.logger import log_info, log_error
from core.system import get_real_user, get_user_home, backup_file, command_exists, run_command


VIMRC_CONTENT = """\" --- 第一步：加载官方默认设置 ---
if !exists('g:skip_defaults_vim')
  source $VIMRUNTIME/defaults.vim
endif

\" --- 第二步：写你自己的『覆盖』命令 ---
set mouse=
set pastetoggle=<F2>
nnoremap <F3> :set number!<CR>
"""


def install_vim_if_needed():
    print("检查 vim 是否安装...")

    if command_exists("vim"):
        log_info("vim 已安装")
        return True

    log_info("vim 未安装，正在安装...")

    if command_exists("apt"):
        run_command("apt update && apt install -y vim")
    elif command_exists("dnf"):
        run_command("dnf install -y vim")
    elif command_exists("yum"):
        run_command("yum install -y vim")
    elif command_exists("pacman"):
        run_command("pacman -Sy --noconfirm vim")
    else:
        log_error("无法识别包管理器，请手动安装 vim")
        return False

    return True


def config_vim():
    real_user = get_real_user()
    user_home = get_user_home(real_user)
    vimrc = Path(user_home) / ".vimrc"

    if not install_vim_if_needed():
        return False

    print()

    if vimrc.exists() and vimrc.stat().st_size > 0:
        print(f"检测到已有 Vim 配置：{vimrc}")

        confirm = input("是否覆盖现有配置？(y/N): ").strip()

        if confirm.lower() != "y":
            print("已取消覆盖")
            return True

        backup_path = backup_file(str(vimrc))

        if backup_path:
            print(f"已备份原配置：{backup_path}")
            print()

    print("写入 ~/.vimrc ...")

    vimrc.write_text(VIMRC_CONTENT, encoding="utf-8")
    run_command(f"chown {real_user}:{real_user} {vimrc}")
    os.chmod(vimrc, 0o644)

    print()
    print("Vim 配置完成")
    print(f"配置文件：{vimrc}")

    return True
