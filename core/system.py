import os
import pwd
import grp
import shutil
import socket
import subprocess
from datetime import datetime


def run_command(command, check=True):
    return subprocess.run(command, shell=True, check=check)


def command_exists(command):
    return shutil.which(command) is not None


def get_real_user():
    return os.environ.get("SUDO_USER") or os.environ.get("USER") or "root"


def get_user_home(username):
    return pwd.getpwnam(username).pw_dir


def get_user_groups(username):
    groups = []

    try:
        primary_gid = pwd.getpwnam(username).pw_gid
        groups.append(grp.getgrgid(primary_gid).gr_name)
    except Exception:
        pass

    for group in grp.getgrall():
        if username in group.gr_mem:
            groups.append(group.gr_name)

    return sorted(set(groups))


def user_in_sudo_group(username):
    groups = get_user_groups(username)
    return "sudo" in groups or "wheel" in groups


def backup_file(path):
    if not os.path.exists(path):
        return None

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup_path = f"{path}.bak.{timestamp}"
    shutil.copy(path, backup_path)

    return backup_path


def port_in_use(port):
    port = int(port)

    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        return sock.connect_ex(("127.0.0.1", port)) == 0
