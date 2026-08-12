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
用法：sudo sh scripts/install.sh [版本]

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

[ "$(id -u)" -eq 0 ] || fail "安装需要 root 权限，请使用 sudo sh scripts/install.sh，或通过 curl ... | sudo sh 运行"
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

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/servertool-install.XXXXXX")"
if [ "$RELEASE" = "latest" ]; then
	RELEASE_METADATA="${TEMP_DIR}/release.json"
	info "正在查询最新版本..."
	download "https://api.github.com/repos/${REPOSITORY}/releases/latest" "$RELEASE_METADATA"
	RELEASE_VERSION="$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$RELEASE_METADATA" | sed -n '1p')"
	[ -n "$RELEASE_VERSION" ] || fail "无法从 GitHub Release 信息中解析最新版本"
else
	RELEASE_VERSION="$RELEASE"
fi
case "$RELEASE_VERSION" in
	*[!A-Za-z0-9._-]*) fail "发布版本包含不支持的字符：$RELEASE_VERSION" ;;
esac

ASSET="snailtool_linux_${ARCH}_${RELEASE_VERSION}"
CHECKSUM_NAME="checksums_${RELEASE_VERSION}.txt"
DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/download/${RELEASE_VERSION}"
ASSET_FILE="${TEMP_DIR}/${ASSET}"
CHECKSUM_FILE="${TEMP_DIR}/${CHECKSUM_NAME}"
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

info "正在下载 ${ASSET}..."
if ! download_optional "${DOWNLOAD_BASE}/${CHECKSUM_NAME}" "$CHECKSUM_FILE"; then
	# 兼容改用版本化文件名之前发布的版本。
	ASSET="snailtool_linux_${ARCH}"
	CHECKSUM_NAME="checksums.txt"
	ASSET_FILE="${TEMP_DIR}/${ASSET}"
	CHECKSUM_FILE="${TEMP_DIR}/${CHECKSUM_NAME}"
	info "该版本使用旧版文件名，正在兼容下载..."
	download "${DOWNLOAD_BASE}/${CHECKSUM_NAME}" "$CHECKSUM_FILE"
fi
download_asset "${DOWNLOAD_BASE}/${ASSET}" "$ASSET_FILE"

EXPECTED_SHA256="$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset { print $1; exit }' "$CHECKSUM_FILE")"
[ -n "$EXPECTED_SHA256" ] || fail "${CHECKSUM_NAME} 中没有 ${ASSET} 的校验值"
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
