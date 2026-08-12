#!/bin/sh

set -eu

REPOSITORY="Snail-one/ServerTool"
INSTALL_DIR="${SERVERTOOL_INSTALL_DIR:-/usr/local/sbin}"
BINARY_NAME="${SERVERTOOL_BINARY_NAME:-snail}"
RELEASE="${SERVERTOOL_VERSION:-latest}"
TEMP_DIR=""
STAGED_FILE=""

info() {
	printf '%s\n' "[INFO] $*"
}

fail() {
	printf '%s\n' "[ERROR] $*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
用法：sudo sh install.sh [版本]

不指定版本时安装或更新到最新版本；也可以指定发布标签，例如 v1.2.0。

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

trap cleanup 0
trap 'exit 1' HUP INT TERM

case "${1:-}" in
	-h|--help)
		usage
		exit 0
		;;
	"") ;;
	*) RELEASE="$1" ;;
esac

case "$RELEASE" in
	*[!A-Za-z0-9._-]*) fail "版本号包含不支持的字符：$RELEASE" ;;
esac
case "$BINARY_NAME" in
	""|*/*) fail "命令名不能为空或包含路径分隔符" ;;
esac
case "$INSTALL_DIR" in
	/*) ;;
	*) fail "安装目录必须是绝对路径：$INSTALL_DIR" ;;
esac

[ "$(id -u)" -eq 0 ] || fail "安装需要 root 权限，请使用 sudo sh install.sh，或通过 curl ... | sudo sh 运行"
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
elif command -v wget >/dev/null 2>&1; then
	download() {
		wget --quiet --tries=3 --timeout=15 --output-document="$2" "$1"
	}
else
	fail "需要 curl 或 wget 才能下载安装包"
fi

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

ASSET="snailtool_linux_${ARCH}"
if [ "$RELEASE" = "latest" ]; then
	DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/latest/download"
else
	DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/download/${RELEASE}"
fi

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/servertool-install.XXXXXX")"
ASSET_FILE="${TEMP_DIR}/${ASSET}"
CHECKSUM_FILE="${TEMP_DIR}/checksums.txt"
TARGET="${INSTALL_DIR}/${BINARY_NAME}"

if [ -e "$TARGET" ]; then
	CURRENT_VERSION=""
	if [ -x "$TARGET" ]; then
		CURRENT_VERSION="$("$TARGET" --version 2>/dev/null | sed -n '1p' || true)"
	fi
	info "当前版本：${CURRENT_VERSION:-未知}"
	ACTION="更新"
else
	ACTION="安装"
fi

info "正在下载 ${ASSET}（${RELEASE}）..."
download "${DOWNLOAD_BASE}/${ASSET}" "$ASSET_FILE"
download "${DOWNLOAD_BASE}/checksums.txt" "$CHECKSUM_FILE"

EXPECTED_SHA256="$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset { print $1; exit }' "$CHECKSUM_FILE")"
[ -n "$EXPECTED_SHA256" ] || fail "checksums.txt 中没有 ${ASSET} 的校验值"
ACTUAL_SHA256="$(file_sha256 "$ASSET_FILE")"
[ "$ACTUAL_SHA256" = "$EXPECTED_SHA256" ] || fail "安装包 SHA-256 校验失败"
info "SHA-256 校验通过"

chmod 0755 "$ASSET_FILE"
DOWNLOADED_VERSION="$("$ASSET_FILE" --version 2>/dev/null | sed -n '1p')"
[ -n "$DOWNLOADED_VERSION" ] || fail "下载的文件无法正常运行"

install -d -m 0755 "$INSTALL_DIR"
STAGED_FILE="${INSTALL_DIR}/.${BINARY_NAME}.new.$$"
install -m 0755 "$ASSET_FILE" "$STAGED_FILE"
mv -f "$STAGED_FILE" "$TARGET"
STAGED_FILE=""

info "${ACTION}完成：${TARGET}"
info "当前版本：${DOWNLOADED_VERSION}"
info "运行命令：sudo ${BINARY_NAME}"
