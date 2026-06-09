#!/bin/bash
set -e

log_info() {
    echo -e "\033[32m[INFO]\033[0m $*"
}

log_warn() {
    echo -e "\033[33m[WARN]\033[0m $*"
}

log_error() {
    echo -e "\033[31m[ERROR]\033[0m $*" >&2
}

port_in_use() {
    local port="$1"

    if command -v ss >/dev/null 2>&1; then
        ss -Htanl | awk '{print $4}' | grep -Eq "(^|:)$port$"
    elif command -v netstat >/dev/null 2>&1; then
        netstat -tanl | awk '{print $4}' | grep -Eq "(^|:)$port$"
    else
        log_warn "未找到 ss/netstat，无法检测端口占用"
        return 1
    fi
}

validate_ssh_pubkey() {
    local pubkey="$1"

    if [[ -z "$pubkey" ]]; then
        log_error "公钥不能为空"
        return 1
    fi

    if ! ssh-keygen -l -f <(printf '%s\n' "$pubkey") >/dev/null 2>&1; then
        log_error "无效 SSH 公钥"
        return 1
    fi
}

if [[ $EUID -ne 0 ]]; then
    log_error "请使用 sudo 或 root 运行此脚本"
    exit 1
fi

show_menu() {
    echo "请选择操作："
    echo "1) 配置当前用户 SSH 公钥登录 + 禁用密码登录 + 随机 SSH 端口"
    echo "2) 配置当前用户 Vim ~/.vimrc"
    echo "3) 配置当前用户 Bash 环境"
    echo "4) 配置当前用户 HTTP/HTTPS 代理环境变量"
    echo "q) 退出"
}

pause() {
    echo
    read -n 1 -s -r -p "按任意键返回菜单..." || true
    echo
}


config_ssh() {
    REAL_USER="${SUDO_USER:-$USER}"
    USER_HOME=$(getent passwd "$REAL_USER" | cut -d: -f6)

    if [[ -z "$USER_HOME" || ! -d "$USER_HOME" ]]; then
        log_error "无法获取用户 $REAL_USER 的 home 目录"
        return 1
    fi

    log_info "当前配置用户：$REAL_USER"
    echo

    if [[ "$REAL_USER" != "root" ]] && ! groups "$REAL_USER" | grep -qE '\b(sudo|wheel)\b'; then
        log_warn "用户 $REAL_USER 不在 sudo/wheel 用户组中"
        read -rp "继续配置可能导致无法提权，是否强行继续？(y/N): " CONFIRM
        [[ "$CONFIRM" =~ ^[Yy]$ ]] || return 1
    fi

    read -rp "请粘贴 SSH 公钥: " PUBKEY

    if ! validate_ssh_pubkey "$PUBKEY"; then
        return 1
    fi

    SSH_DIR="$USER_HOME/.ssh"
    AUTH_KEYS="$SSH_DIR/authorized_keys"

    mkdir -p "$SSH_DIR"
    chmod 700 "$SSH_DIR"
    touch "$AUTH_KEYS"

    if ! grep -qxF "$PUBKEY" "$AUTH_KEYS"; then
        echo "$PUBKEY" >> "$AUTH_KEYS"
        log_info "已添加 SSH 公钥"
    else
        log_info "SSH 公钥已存在，跳过添加"
    fi

    chmod 600 "$AUTH_KEYS"
    chown -R "$REAL_USER:$REAL_USER" "$SSH_DIR"

    echo
    read -rp "请输入 SSH 端口（直接回车随机生成）: " PORT

    if [[ -z "$PORT" ]]; then
        log_info "生成随机 SSH 端口..."

        while :; do
            if command -v shuf >/dev/null 2>&1; then
                PORT=$(shuf -i 20000-50000 -n 1)
            else
                PORT=$(awk 'BEGIN{srand();print int(rand()*(50000-20000+1))+20000}')
            fi

            if ! port_in_use "$PORT"; then
                break
            fi
        done
    else
        if ! [[ "$PORT" =~ ^[0-9]+$ ]]; then
            log_error "端口必须是数字"
            return 1
        fi

        if (( PORT < 1 || PORT > 65535 )); then
            log_error "端口范围必须是 1-65535"
            return 1
        fi

        if port_in_use "$PORT"; then
            log_error "端口 $PORT 已被占用"
            return 1
        fi
    fi

    SSHD_CONFIG="/etc/ssh/sshd_config"
    SSHD_DIR="/etc/ssh/sshd_config.d"
    CUSTOM_CONF="$SSHD_DIR/99-custom.conf"

    mkdir -p "$SSHD_DIR"

    echo
    log_info "检查 Include 配置..."

    if ! grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf' "$SSHD_CONFIG"; then
        echo 'Include /etc/ssh/sshd_config.d/*.conf' >> "$SSHD_CONFIG"
        log_info "已自动添加 Include 配置"
    fi

    if [[ -f "$CUSTOM_CONF" ]]; then
        BACKUP_FILE="${CUSTOM_CONF}.bak.$(date +%Y%m%d_%H%M%S)"
        cp "$CUSTOM_CONF" "$BACKUP_FILE"
        log_info "已备份原 SSH 自定义配置：$BACKUP_FILE"
    fi

    if [[ "$REAL_USER" == "root" ]]; then
        PERMIT_ROOT_LOGIN="prohibit-password"
        log_warn "当前配置用户是 root：保留 root 公钥登录，不禁用 root 登录"
    else
        PERMIT_ROOT_LOGIN="no"
    fi

    echo
    log_info "写入自定义 SSH 配置..."

    cat > "$CUSTOM_CONF" <<EOF
# Managed by setup tool

Port $PORT
PasswordAuthentication no
PermitRootLogin $PERMIT_ROOT_LOGIN
PubkeyAuthentication yes
EOF

    chmod 644 "$CUSTOM_CONF"

    echo
    log_info "验证 sshd 配置..."

    if ! /usr/sbin/sshd -t; then
        log_error "sshd 配置校验失败"
        return 1
    fi

    echo
    log_info "重新加载 SSH 服务..."

    if systemctl list-unit-files | grep -q '^ssh\.service'; then
        systemctl reload ssh || systemctl restart ssh || return 1
    elif systemctl list-unit-files | grep -q '^sshd\.service'; then
        systemctl reload sshd || systemctl restart sshd || return 1
    else
        log_error "未找到 SSH 服务"
        return 1
    fi

    echo
    log_info "SSH 配置完成"
    echo
    echo "用户：$REAL_USER"
    echo "端口：$PORT"
    echo "PermitRootLogin：$PERMIT_ROOT_LOGIN"
    echo
    echo "连接方式："
    echo "ssh -p $PORT $REAL_USER@服务器IP"
    echo
    log_warn "请先新开一个终端测试 SSH 登录成功后，再关闭当前会话。"
}




config_vim() {
    REAL_USER="${SUDO_USER:-$USER}"
    USER_HOME=$(getent passwd "$REAL_USER" | cut -d: -f6)
    VIMRC="$USER_HOME/.vimrc"

    echo "检查 vim 是否安装..."

    if ! command -v vim >/dev/null 2>&1; then
        log_info "vim 未安装，正在安装..."

        if command -v apt >/dev/null 2>&1; then
            apt update
            apt install -y vim
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y vim
        elif command -v yum >/dev/null 2>&1; then
            yum install -y vim
        elif command -v pacman >/dev/null 2>&1; then
            pacman -Sy --noconfirm vim
        else
            log_error "无法识别包管理器，请手动安装 vim"
            return 1
        fi
    else
        log_info "vim 已安装"
    fi

    echo

    if [[ -f "$VIMRC" && -s "$VIMRC" ]]; then
        echo "检测到已有 Vim 配置：$VIMRC"

        read -rp "是否覆盖现有配置？(y/N): " CONFIRM

        if [[ ! "$CONFIRM" =~ ^[Yy]$ ]]; then
            echo "已取消覆盖"
            return 0
        fi

        BACKUP_FILE="${VIMRC}.bak.$(date +%Y%m%d_%H%M%S)"

        cp "$VIMRC" "$BACKUP_FILE"

        echo "已备份原配置：$BACKUP_FILE"
        echo
    fi

    echo "写入 ~/.vimrc ..."

    cat > "$VIMRC" <<'EOF'
" --- 第一步：加载官方默认设置 ---
if !exists('g:skip_defaults_vim')
  source $VIMRUNTIME/defaults.vim
endif

" --- 第二步：写你自己的『覆盖』命令 ---
set mouse=
set pastetoggle=<F2>
nnoremap <F3> :set number!<CR>
EOF

    chown "$REAL_USER:$REAL_USER" "$VIMRC"
    chmod 644 "$VIMRC"

    echo
    echo "Vim 配置完成"
    echo "配置文件：$VIMRC"
}



config_bash() {
    REAL_USER="${SUDO_USER:-$USER}"
    USER_HOME="$(eval echo "~$REAL_USER")"
    BASHRC="$USER_HOME/.bashrc"

    log_info "配置 Bash 环境..."
    echo "当前用户：$REAL_USER"
    echo "配置文件：$BASHRC"

    touch "$BASHRC"

    if [[ "$REAL_USER" == "root" ]]; then
        cat > "$BASHRC" <<'EOF'
# ~/.bashrc: executed by bash(1) for non-login shells.

# Note: PS1 is set in /etc/profile, and the default umask is defined
# in /etc/login.defs. You should not need this unless you want different
# defaults for root.

 PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '
 PS1='\[\033[38;5;196m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '


# PS1='\[\033[01;35m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '

# umask 022
# You may uncomment the following lines if you want `ls' to be colorized:
 export LS_OPTIONS='--color=auto'
 eval "$(dircolors)"
 alias ls='ls $LS_OPTIONS'
 alias ll='ls $LS_OPTIONS -l'
 alias l='ls $LS_OPTIONS -lA'
#
# Some more alias to avoid making mistakes:
 alias rm='rm -i'
 alias cp='cp -i'
 alias mv='mv -i'
EOF
    else
        # sed -i \
        #     -e "s/^[[:space:]]*#\?[[:space:]]*alias ll=.*/alias ll='ls -l'/" \
        #     -e "s/^[[:space:]]*#\?[[:space:]]*alias la=.*/alias la='ls -A'/" \
        #     -e "s/^[[:space:]]*#\?[[:space:]]*alias l=.*/alias l='ls -lah'/" \
        #     "$BASHRC"
        sed -i -E \
            -e "s|^[[:space:]]*#?[[:space:]]*alias ll=.*|alias ll='ls -l'|" \
            -e "s|^[[:space:]]*#?[[:space:]]*alias la=.*|alias la='ls -A'|" \
            -e "s|^[[:space:]]*#?[[:space:]]*alias l=.*|alias l='ls -lah'|" \
            "$BASHRC"
    fi

    chown "$REAL_USER:$REAL_USER" "$BASHRC"
    chmod 644 "$BASHRC"

    echo
    echo "已经修改 ~/.bashrc，新的别名配置如下："
    echo "alias ll='ls -l'"
    echo "alias la='ls -A'"
    echo "alias l='ls -lah'"
    echo "Bash 配置完成"
}



config_proxy() {
    REAL_USER="${SUDO_USER:-$USER}"
    USER_HOME="$(eval echo "~$REAL_USER")"
    BASHRC="$USER_HOME/.bashrc"

    log_info "配置代理环境变量"
    echo


    log_info "ip:port 格式，或 username:password@ip:port 格式"
    echo
    read -rp "请输入代理地址: " PROXY_INPUT


    if [[ -z "$PROXY_INPUT" ]]; then
        echo "错误：代理地址不能为空"
        return 1
    fi

    # 用户名:密码@IP:端口
    if [[ "$PROXY_INPUT" =~ ^([^:]+):([^@]+)@([^:]+):([0-9]+)$ ]]; then
        PORT="${BASH_REMATCH[4]}"

        if (( PORT < 1 || PORT > 65535 )); then
            echo "错误：代理端口范围必须是 1-65535"
            return 1
        fi

        PROXY_URL="http://$PROXY_INPUT"

    # IP:端口
    elif [[ "$PROXY_INPUT" =~ ^([^:]+):([0-9]+)$ ]]; then
        PORT="${BASH_REMATCH[2]}"

        if (( PORT < 1 || PORT > 65535 )); then
            echo "错误：代理端口范围必须是 1-65535"
            return 1
        fi

        PROXY_URL="http://$PROXY_INPUT"

    else
        echo "错误：代理格式不正确"
        echo
        echo "支持格式："
        echo "127.0.0.1:8888"
        echo "192.168.1.1:8888"
        echo "admin:123456@192.168.1.1:8888"
        return 1
    fi

    touch "$BASHRC"

    sed -i '/# ===== BEGIN PROXY CONFIG =====/,/# ===== END PROXY CONFIG =====/d' "$BASHRC"

    cat >> "$BASHRC" <<EOF

# ===== BEGIN PROXY CONFIG =====

export http_proxy="$PROXY_URL"
export https_proxy="$PROXY_URL"

export HTTP_PROXY="$PROXY_URL"
export HTTPS_PROXY="$PROXY_URL"

export no_proxy="localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
export NO_PROXY="localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

# ===== END PROXY CONFIG =====
EOF

    chown "$REAL_USER:$REAL_USER" "$BASHRC"

    echo
    echo "代理配置完成"
    echo
    echo "代理地址：$PROXY_URL"
    echo
    echo "立即生效请执行："
    echo "source ~/.bashrc"
}


while true; do
    command -v clear >/dev/null 2>&1 && clear

    show_menu

    echo
    read -rp "输入选项: " CHOICE
    echo

    case "$CHOICE" in
        1)
            config_ssh || log_error "SSH 配置失败，已返回菜单"
            pause
            ;;
        2)
            config_vim || log_error "Vim 配置失败，已返回菜单"
            pause
            ;;
        3)
            config_bash || log_error "Bash 配置失败，已返回菜单"
            pause
            ;;
        4)
            config_proxy || log_error "代理配置失败，已返回菜单"
            pause
            ;;

        q|Q|exit|EXIT)
            echo "已退出"
            exit 0
            ;;
        *)
            echo "无效选项，请重新输入"
            pause
            ;;
    esac
done