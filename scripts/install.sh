#!/bin/sh

set -eu

REPOSITORY="Snail-one/ServerTool"
INSTALL_DIR="${SERVERTOOL_INSTALL_DIR:-/usr/local/sbin}"
BINARY_NAME="${SERVERTOOL_BINARY_NAME:-snail}"
RELEASE="${SERVERTOOL_VERSION:-latest}"
MODE="install"
TEMP_DIR=""
STAGED_FILE=""

RESET=""
BOLD=""
DIM=""
ORANGE=""
BLUE=""
GREEN=""
YELLOW=""
RED=""

init_colors() {
	# NO_COLOR 始终优先；FORCE_COLOR=1 可为保留 ANSI 的日志采集器强制启用颜色。
	if [ "${NO_COLOR+set}" = "set" ]; then
		return
	fi
	if [ "${FORCE_COLOR:-0}" != "1" ]; then
		[ "${TERM:-}" != "dumb" ] || return
		[ -t 1 ] || return
	fi
	ESC="$(printf '\033')"
	RESET="${ESC}[0m"
	BOLD="${ESC}[1m"
	DIM="${ESC}[2m"
	ORANGE="${ESC}[38;5;208m"
	BLUE="${ESC}[34m"
	GREEN="${ESC}[32m"
	YELLOW="${ESC}[33m"
	RED="${ESC}[31m"
}

print_banner() {
	printf '%s%s%s\n' "$BOLD$ORANGE" "╭─ ServerTool" "$RESET"
	printf '%s│ %s%s\n' "$ORANGE" "$1" "$RESET"
	printf '%s%s%s\n' "$ORANGE" "╰──────────────────────────────────────────────" "$RESET"
	printf '\n'
}

print_completion_card() {
	printf '\n'
	printf '%s%s%s\n' "$BOLD$ORANGE" "╭─ ServerTool" "$RESET"
	printf '%s│ %s%s%s\n' "$ORANGE" "$BOLD$GREEN" "$1完成" "$RESET"
	printf '%s│ %s版本：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$2" "$RESET"
	printf '%s│ %s位置：%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$3"
	printf '%s%s%s\n' "$ORANGE" "╰──────────────────────────────────────────────" "$RESET"
}

print_result_card() {
	printf '%s%s%s\n' "$BOLD$ORANGE" "╭─ ServerTool" "$RESET"
	printf '%s│ %s%s%s\n' "$ORANGE" "$BOLD$GREEN" "$1" "$RESET"
	printf '%s%s%s\n' "$ORANGE" "╰──────────────────────────────────────────────" "$RESET"
}

step() {
	printf '%s[步骤]%s %s\n' "$ORANGE" "$RESET" "$*"
}

info() {
	printf '%s[信息]%s %s\n' "$BLUE" "$RESET" "$*"
}

result() {
	printf '%s[结果]%s %s\n' "$GREEN" "$RESET" "$*"
}

warn() {
	printf '%s[警告]%s %s\n' "$YELLOW" "$RESET" "$*"
}

fail() {
	printf '%s[错误]%s %s\n' "$RED" "$RESET" "$*" >&2
	exit 1
}

print_release_info() {
	printf '\n'
	printf '%s%s%s\n' "$BOLD$ORANGE" "╭─ ServerTool" "$RESET"
	printf '%s│ %s%s%s\n' "$ORANGE" "$BOLD$ORANGE" "发布信息" "$RESET"
	printf '%s│ %s平台：%sLinux/%s\n' "$ORANGE" "$BLUE" "$RESET" "$ARCH"
	printf '%s│ %s当前版本：%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$1"
	printf '%s│ %s目标版本：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$2" "$RESET"
	printf '%s│ %s执行操作：%s%s%s%s\n' "$ORANGE" "$BLUE" "$RESET" "$BOLD" "$3" "$RESET"
	printf '%s%s%s\n' "$ORANGE" "╰──────────────────────────────────────────────" "$RESET"
}

usage() {
	cat <<'EOF'
用法：
  sudo sh scripts/install.sh [版本]
  sudo sh scripts/install.sh uninstall

不指定版本时安装或更新到最新版本；也可以指定发布标签，例如 v1.2.0。
使用 uninstall 或 --uninstall 删除已安装的程序文件，不清理由本工具配置的系统服务和用户配置。

可选环境变量：
  SERVERTOOL_VERSION       要安装的发布标签，默认为 latest
  SERVERTOOL_INSTALL_DIR   安装目录，默认为 /usr/local/sbin
  SERVERTOOL_BINARY_NAME   安装后的命令名，默认为 snail
EOF
}

cleanup() {
	if [ -n "$STAGED_FILE" ]; then
		rm -f "$STAGED_FILE"
	fi
	if [ -n "$TEMP_DIR" ]; then
		rm -rf "$TEMP_DIR"
	fi
}

proxy_configured() {
	[ -n "${HTTPS_PROXY:-}" ] || [ -n "${https_proxy:-}" ] ||
		[ -n "${HTTP_PROXY:-}" ] || [ -n "${http_proxy:-}" ] ||
		[ -n "${ALL_PROXY:-}" ] || [ -n "${all_proxy:-}" ]
}

trap cleanup 0
trap 'exit 1' HUP INT TERM

case "${1:-}" in
	-h|--help)
		usage
		exit 0
		;;
	uninstall|--uninstall)
		MODE="uninstall"
		;;
	"") ;;
	*) RELEASE="$1" ;;
esac

init_colors
if [ "$MODE" = "uninstall" ]; then
	print_banner "自身卸载"
else
	print_banner "自身安装与更新"
fi

case "$BINARY_NAME" in
	""|*/*) fail "命令名不能为空或包含路径分隔符" ;;
esac
case "$INSTALL_DIR" in
	/*) ;;
	*) fail "安装目录必须是绝对路径：$INSTALL_DIR" ;;
esac
TARGET="${INSTALL_DIR}/${BINARY_NAME}"

[ "$(id -u)" -eq 0 ] || fail "该操作需要 root 权限，请使用 sudo sh scripts/install.sh 运行"

if [ "$MODE" = "uninstall" ]; then
	for REQUIRED_COMMAND in awk rm sed; do
		command -v "$REQUIRED_COMMAND" >/dev/null 2>&1 || fail "缺少必要命令：$REQUIRED_COMMAND"
	done

	step "检查安装状态"
	if [ ! -e "$TARGET" ] && [ ! -L "$TARGET" ]; then
		info "程序文件不存在：${TARGET}"
		printf '\n'
		result "ServerTool 当前未安装，无需卸载。"
		exit 0
	fi

	CURRENT_RELEASE=""
	if [ -x "$TARGET" ]; then
		CURRENT_VERSION="$("$TARGET" --version 2>/dev/null | sed -n '1p' || true)"
		CURRENT_RELEASE="$(printf '%s\n' "$CURRENT_VERSION" | awk '$1 == "snailtool" { print $2; exit }')"
	fi
	info "当前版本：${CURRENT_RELEASE:-未知}"
	info "程序路径：${TARGET}"

	printf '\n'
	step "卸载程序"
	rm -f "$TARGET"
	[ ! -e "$TARGET" ] && [ ! -L "$TARGET" ] || fail "无法删除程序文件：${TARGET}"
	result "ServerTool 卸载完成。"
	info "本工具配置的系统服务和用户配置均已保留。"
	exit 0
fi

case "$RELEASE" in
	*[!A-Za-z0-9._-]*) fail "版本号包含不支持的字符：$RELEASE" ;;
esac

[ "$(uname -s)" = "Linux" ] || fail "目前仅支持 Linux"

for REQUIRED_COMMAND in awk chmod install mktemp mv sed uname; do
	command -v "$REQUIRED_COMMAND" >/dev/null 2>&1 || fail "缺少必要命令：$REQUIRED_COMMAND"
done

case "$(uname -m)" in
	x86_64|amd64) ARCH="amd64" ;;
	aarch64|arm64) ARCH="arm64" ;;
	*) fail "不支持的处理器架构：$(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
	download() {
		curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 --output "$2" "$1"
	}
	download_asset() {
		curl --fail --location --show-error --retry 3 --connect-timeout 15 --progress-bar --output "$2" "$1"
	}
	download_optional() {
		curl --fail --location --silent --retry 1 --connect-timeout 15 --output "$2" "$1"
	}
	download_api_direct() {
		curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 --noproxy '*' --output "$2" "$1"
	}
elif command -v wget >/dev/null 2>&1; then
	download() {
		wget --quiet --tries=3 --timeout=15 --output-document="$2" "$1"
	}
	download_asset() {
		wget --tries=3 --timeout=15 --output-document="$2" "$1"
	}
	download_optional() {
		wget --quiet --tries=1 --timeout=15 --output-document="$2" "$1"
	}
	download_api_direct() {
		wget --no-proxy --quiet --tries=3 --timeout=15 --output-document="$2" "$1"
	}
else
	fail "需要 curl 或 wget 才能下载安装包"
fi

download_with_direct_fallback() {
	if download "$1" "$2"; then
		return 0
	fi
	proxy_configured || return 1
	warn "通过代理访问 GitHub 失败，正在尝试直连"
	download_api_direct "$1" "$2"
}

release_version_from_checksums() {
	awk '
		{
			name = $2
			sub(/^\*/, "", name)
			if (name ~ /^snailtool_linux_(amd64|arm64)_/) {
				sub(/^snailtool_linux_(amd64|arm64)_/, "", name)
				if (version == "") {
					version = name
				} else if (version != name) {
					mismatch = 1
				}
			}
		}
		END {
			if (version == "" || mismatch) {
				exit 1
			}
			print version
		}
	' "$1"
}

if command -v sha256sum >/dev/null 2>&1; then
	file_sha256() {
		sha256sum "$1" | awk '{print $1}'
	}
elif command -v shasum >/dev/null 2>&1; then
	file_sha256() {
		shasum -a 256 "$1" | awk '{print $1}'
	}
else
	fail "需要 sha256sum 或 shasum 才能校验安装包"
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/servertool-install.XXXXXX")"
step "检查发布版本"
if [ "$RELEASE" = "latest" ]; then
	RELEASE_METADATA="${TEMP_DIR}/release.json"
	RELEASE_VERSION=""
	if download_with_direct_fallback "https://api.github.com/repos/${REPOSITORY}/releases/latest" "$RELEASE_METADATA"; then
		RELEASE_VERSION="$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$RELEASE_METADATA" | sed -n '1p')"
	fi
	if [ -n "$RELEASE_VERSION" ]; then
		info "最新正式版本：${RELEASE_VERSION}（GitHub Releases API）"
	else
		warn "GitHub Releases API 不可用，正在通过 checksums.txt 解析最新版本"
		CHECKSUM_FILE="${TEMP_DIR}/checksums.txt"
		if ! download_with_direct_fallback "https://github.com/${REPOSITORY}/releases/latest/download/checksums.txt" "$CHECKSUM_FILE"; then
			fail "无法通过 GitHub Releases API 或 checksums.txt 获取最新版本"
		fi
		if ! RELEASE_VERSION="$(release_version_from_checksums "$CHECKSUM_FILE")"; then
			fail "checksums.txt 中没有唯一有效的 ServerTool 版本号"
		fi
		info "最新正式版本：${RELEASE_VERSION}（checksums.txt 兜底）"
	fi
	LATEST_RELEASE=true
else
	RELEASE_VERSION="$RELEASE"
	info "指定发布版本：${RELEASE_VERSION}（用户指定）"
	LATEST_RELEASE=false
fi
case "$RELEASE_VERSION" in
	*[!A-Za-z0-9._-]*) fail "发布版本包含不支持的字符：$RELEASE_VERSION" ;;
esac

ASSET="snailtool_linux_${ARCH}_${RELEASE_VERSION}"
CHECKSUM_NAME="checksums.txt"
DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/download/${RELEASE_VERSION}"
ASSET_FILE="${TEMP_DIR}/${ASSET}"
CHECKSUM_FILE="${TEMP_DIR}/${CHECKSUM_NAME}"

# 先取得体积很小的校验文件，用它同时验证现有程序和待下载程序。
# API 失败时可能已经通过 latest 下载过该文件，此处直接复用。
if [ ! -s "$CHECKSUM_FILE" ] && ! download_optional "${DOWNLOAD_BASE}/${CHECKSUM_NAME}" "$CHECKSUM_FILE"; then
	# 兼容曾经使用版本化校验文件名的 Release。
	CHECKSUM_NAME="checksums_${RELEASE_VERSION}.txt"
	CHECKSUM_FILE="${TEMP_DIR}/${CHECKSUM_NAME}"
	info "该版本使用旧版校验文件名，正在兼容处理"
	download "${DOWNLOAD_BASE}/${CHECKSUM_NAME}" "$CHECKSUM_FILE"
fi
EXPECTED_SHA256="$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset { print $1; exit }' "$CHECKSUM_FILE")"
# 兼容早期未在二进制文件名末尾添加版本号的 Release。
if [ -z "$EXPECTED_SHA256" ]; then
	LEGACY_ASSET="snailtool_linux_${ARCH}"
	EXPECTED_SHA256="$(awk -v asset="$LEGACY_ASSET" '$2 == asset || $2 == "*" asset { print $1; exit }' "$CHECKSUM_FILE")"
	if [ -n "$EXPECTED_SHA256" ]; then
		ASSET="$LEGACY_ASSET"
		ASSET_FILE="${TEMP_DIR}/${ASSET}"
		info "该版本使用旧版发布文件名，正在兼容处理"
	fi
fi
[ -n "$EXPECTED_SHA256" ] || fail "${CHECKSUM_NAME} 中没有 ${ASSET} 的校验值"

CURRENT_DISPLAY="未安装"
if [ -e "$TARGET" ]; then
	CURRENT_VERSION=""
	CURRENT_RELEASE=""
	if [ -x "$TARGET" ]; then
		CURRENT_VERSION="$("$TARGET" --version 2>/dev/null | sed -n '1p' || true)"
		CURRENT_RELEASE="$(printf '%s\n' "$CURRENT_VERSION" | awk '$1 == "snailtool" { print $2; exit }')"
	fi
	CURRENT_DISPLAY="${CURRENT_RELEASE:-未知}"
	if [ "$CURRENT_RELEASE" = "$RELEASE_VERSION" ]; then
		CURRENT_SHA256="$(file_sha256 "$TARGET")"
		if [ "$CURRENT_SHA256" = "$EXPECTED_SHA256" ]; then
			print_release_info "$CURRENT_DISPLAY" "$RELEASE_VERSION" "无需更新"
			printf '\n'
			if [ "$LATEST_RELEASE" = true ]; then
				print_result_card "当前已是最新正式版本，无需更新。"
			else
				print_result_card "当前已是指定版本，无需更新。"
			fi
			exit 0
		fi
		ACTION="修复安装"
		print_release_info "$CURRENT_DISPLAY" "$RELEASE_VERSION" "$ACTION"
		printf '\n'
		warn "当前版本号一致，但文件校验失败，将重新下载修复"
	else
		ACTION="更新"
		print_release_info "$CURRENT_DISPLAY" "$RELEASE_VERSION" "$ACTION"
	fi
else
	ACTION="安装"
	print_release_info "$CURRENT_DISPLAY" "$RELEASE_VERSION" "$ACTION"
fi

printf '\n'
step "下载发布文件"
info "正在下载：${ASSET}"
download_asset "${DOWNLOAD_BASE}/${ASSET}" "$ASSET_FILE"
result "发布文件下载完成"

printf '\n'
step "校验发布文件"
ACTUAL_SHA256="$(file_sha256 "$ASSET_FILE")"
[ "$ACTUAL_SHA256" = "$EXPECTED_SHA256" ] || fail "安装包 SHA-256 校验失败"
result "SHA-256 校验通过"

chmod 0755 "$ASSET_FILE"
DOWNLOADED_VERSION="$("$ASSET_FILE" --version 2>/dev/null | sed -n '1p')"
[ -n "$DOWNLOADED_VERSION" ] || fail "下载的文件无法正常运行"

printf '\n'
step "安装程序"
info "正在写入：${TARGET}"
install -d -m 0755 "$INSTALL_DIR"
STAGED_FILE="${INSTALL_DIR}/.${BINARY_NAME}.new.$$"
install -m 0755 "$ASSET_FILE" "$STAGED_FILE"
mv -f "$STAGED_FILE" "$TARGET"
STAGED_FILE=""

print_completion_card "$ACTION" "$RELEASE_VERSION" "$TARGET"
