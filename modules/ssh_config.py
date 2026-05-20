import os
import random
import subprocess
from pathlib import Path

from core.logger import log_info, log_warn, log_error
from core.system import (
    get_real_user,
    get_user_home,
    user_in_sudo_group,
    backup_file,
    port_in_use,
    run_command,
)


def validate_ssh_pubkey(pubkey):
    if not pubkey.strip():
        log_error("公钥不能为空")
        return False

    process = subprocess.run(
        ["ssh-keygen", "-l", "-f", "-"],
        input=(pubkey.strip() + "\n").encode(),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )

    if process.returncode != 0:
        log_error("无效 SSH 公钥")
        return False

    return True


def generate_random_port():
    while True:
        port = random.randint(20000, 50000)

        if not port_in_use(port):
            return port


def get_ssh_port_from_input():
    value = input("请输入 SSH 端口（直接回车随机生成）: ").strip()

    if not value:
        log_info("生成随机 SSH 端口...")
        return generate_random_port()

    if not value.isdigit():
        log_error("端口必须是数字")
        return None

    port = int(value)

    if port < 1 or port > 65535:
        log_error("端口范围必须是 1-65535")
        return None

    if port_in_use(port):
        log_error(f"端口 {port} 已被占用")
        return None

    return port


def ensure_sshd_include():
    sshd_config = "/etc/ssh/sshd_config"
    include_line = "Include /etc/ssh/sshd_config.d/*.conf"

    if not os.path.exists(sshd_config):
        log_error(f"未找到 {sshd_config}")
        return False

    with open(sshd_config, "r", encoding="utf-8", errors="ignore") as file:
        content = file.read()

    if include_line not in content:
        with open(sshd_config, "a", encoding="utf-8") as file:
            file.write(f"\n{include_line}\n")

        log_info("已自动添加 Include 配置")

    return True


def write_authorized_key(username, user_home, pubkey):
    ssh_dir = Path(user_home) / ".ssh"
    authorized_keys = ssh_dir / "authorized_keys"

    ssh_dir.mkdir(parents=True, exist_ok=True)
    authorized_keys.touch(exist_ok=True)

    existing = authorized_keys.read_text(encoding="utf-8", errors="ignore")

    if pubkey not in existing:
        with authorized_keys.open("a", encoding="utf-8") as file:
            file.write(pubkey.strip() + "\n")

        log_info("已添加 SSH 公钥")
    else:
        log_info("SSH 公钥已存在，跳过添加")

    os.chmod(ssh_dir, 0o700)
    os.chmod(authorized_keys, 0o600)
    run_command(f"chown -R {username}:{username} {ssh_dir}")


def write_sshd_custom_config(port, permit_root_login):
    sshd_dir = "/etc/ssh/sshd_config.d"
    custom_conf = f"{sshd_dir}/99-custom.conf"

    os.makedirs(sshd_dir, exist_ok=True)

    backup_path = backup_file(custom_conf)

    if backup_path:
        log_info(f"已备份原 SSH 自定义配置：{backup_path}")

    content = f"""# Managed by setup tool

Port {port}
PasswordAuthentication no
PermitRootLogin {permit_root_login}
PubkeyAuthentication yes
"""

    with open(custom_conf, "w", encoding="utf-8") as file:
        file.write(content)

    os.chmod(custom_conf, 0o644)

    return custom_conf


def validate_sshd_config():
    log_info("验证 sshd 配置...")

    process = subprocess.run(
        ["/usr/sbin/sshd", "-t"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )

    if process.returncode != 0:
        log_error("sshd 配置校验失败")
        print(process.stderr.decode(errors="ignore"))
        return False

    return True


def reload_ssh_service():
    log_info("重新加载 SSH 服务...")

    try:
        run_command("systemctl reload ssh || systemctl restart ssh")
        return True
    except Exception:
        pass

    try:
        run_command("systemctl reload sshd || systemctl restart sshd")
        return True
    except Exception:
        log_error("未找到 SSH 服务，或 SSH 服务重载失败")
        return False


def config_ssh():
    real_user = get_real_user()
    user_home = get_user_home(real_user)

    if not user_home or not os.path.isdir(user_home):
        log_error(f"无法获取用户 {real_user} 的 home 目录")
        return False

    log_info(f"当前配置用户：{real_user}")
    print()

    if real_user != "root" and not user_in_sudo_group(real_user):
        log_warn(f"用户 {real_user} 不在 sudo/wheel 用户组中")
        confirm = input("继续配置可能导致无法提权，是否强行继续？(y/N): ").strip()

        if confirm.lower() != "y":
            return False

    pubkey = input("请粘贴 SSH 公钥: ").strip()

    if not validate_ssh_pubkey(pubkey):
        return False

    write_authorized_key(real_user, user_home, pubkey)

    print()
    port = get_ssh_port_from_input()

    if port is None:
        return False

    if not ensure_sshd_include():
        return False

    if real_user == "root":
        permit_root_login = "prohibit-password"
        log_warn("当前配置用户是 root：保留 root 公钥登录，不禁用 root 登录")
    else:
        permit_root_login = "no"

    print()
    log_info("写入自定义 SSH 配置...")
    write_sshd_custom_config(port, permit_root_login)

    print()

    if not validate_sshd_config():
        return False

    print()

    if not reload_ssh_service():
        return False

    print()
    log_info("SSH 配置完成")
    print()
    print(f"用户：{real_user}")
    print(f"端口：{port}")
    print(f"PermitRootLogin：{permit_root_login}")
    print()
    print("连接方式：")
    print(f"ssh -p {port} {real_user}@服务器IP")
    print()
    log_warn("请先新开一个终端测试 SSH 登录成功后，再关闭当前会话。")

    return True
