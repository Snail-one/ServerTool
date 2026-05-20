import sys

GREEN = "\033[32m"
YELLOW = "\033[33m"
RED = "\033[31m"
RESET = "\033[0m"


def log_info(message):
    print(f"{GREEN}[INFO]{RESET} {message}")


def log_warn(message):
    print(f"{YELLOW}[WARN]{RESET} {message}")


def log_error(message):
    print(f"{RED}[ERROR]{RESET} {message}", file=sys.stderr)
