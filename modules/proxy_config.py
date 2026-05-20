import re
from pathlib import Path

from core.logger import log_info
from core.system import get_real_user, get_user_home, run_command


PROXY_BLOCK_TEMPLATE = """

# ===== BEGIN PROXY CONFIG =====

export http_proxy="{proxy_url}"
export https_proxy="{proxy_url}"

export HTTP_PROXY="{proxy_url}"
export HTTPS_PROXY="{proxy_url}"

export no_proxy="localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
export NO_PROXY="localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

# ===== END PROXY CONFIG =====
"""


def validate_proxy_input(proxy_input):
    auth_pattern = r"^([^:]+):([^@]+)@([^:]+):([0-9]+)$"
    host_pattern = r"^([^:]+):([0-9]+)$"

    auth_match = re.match(auth_pattern, proxy_input)
    host_match = re.match(host_pattern, proxy_input)

    if auth_match:
        port = int(auth_match.group(4))
    elif host_match:
        port = int(host_match.group(2))
    else:
        return None

    if port < 1 or port > 65535:
        return None

    return f"http://{proxy_input}"


def remove_existing_proxy_block(content):
    pattern = r"# ===== BEGIN PROXY CONFIG =====.*?# ===== END PROXY CONFIG ====="

    return re.sub(pattern, "", content, flags=re.S).rstrip()


def config_proxy():
    real_user = get_real_user()
    user_home = get_user_home(real_user)
    bashrc = Path(user_home) / ".bashrc"

    log_info("配置代理环境变量")
    print()

    proxy_input = input("请输入代理地址: ").strip()

    if not proxy_input:
        print("错误：代理地址不能为空")
        return False

    proxy_url = validate_proxy_input(proxy_input)

    if proxy_url is None:
        print("错误：代理格式不正确")
        print()
        print("支持格式：")
        print("127.0.0.1:8888")
        print("192.168.31.205:8888")
        print("admin:123456@192.168.31.205:8888")
        return False

    bashrc.touch(exist_ok=True)

    content = bashrc.read_text(encoding="utf-8", errors="ignore")
    content = remove_existing_proxy_block(content)
    content += PROXY_BLOCK_TEMPLATE.format(proxy_url=proxy_url)

    bashrc.write_text(content, encoding="utf-8")

    run_command(f"chown {real_user}:{real_user} {bashrc}")

    print()
    print("代理配置完成")
    print()
    print(f"代理地址：{proxy_url}")
    print()
    print("立即生效请执行：")
    print("source ~/.bashrc")

    return True
