#!/usr/bin/env bash
set -euo pipefail

NFT_MAIN_CONFIG="/etc/nftables.conf"
NFT_SNAIL_CONFIG="/etc/nftables.d/snail.nft"

install_snail_nft_config() {
    mkdir -p /etc/nftables.d

    cat > "$NFT_SNAIL_CONFIG" <<'EOF'
table inet filter {
    chain input {
        type filter hook input priority filter; policy accept;

        jump Snail
    }

    chain Snail {
        # 用户自定义规则写这里

        # 示例：
        # ip saddr 192.168.1.100 drop
        # tcp dport 22 accept
    }
}
EOF

    touch "$NFT_MAIN_CONFIG"

    if ! grep -qF "include \"/etc/nftables.d/snail.nft\"" "$NFT_MAIN_CONFIG"; then
        printf '\ninclude "/etc/nftables.d/snail.nft"\n' >> "$NFT_MAIN_CONFIG"
    fi
}

    if ! grep -qF "include \"$NFT_SNAIL_CONFIG\"" "$NFT_MAIN_CONFIG"; then
        printf '\ninclude "%s"\n' "$NFT_SNAIL_CONFIG" >> "$NFT_MAIN_CONFIG"
    fi

reload_nftables() {
    nft -c -f "$NFT_MAIN_CONFIG"
    systemctl enable nftables >/dev/null 2>&1 || true
    systemctl restart nftables
}

main() {
    install_snail_nft_config
    reload_nftables

    echo "Snail nftables config installed persistently."
}

main "$@"