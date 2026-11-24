#!/bin/sh
set -eu

PATH=$PATH:/usr/sbin:/sbin
export PATH

configs=$1
external=
container=
cleanup() {
    for pid in $external $container; do
        kill "$pid" 2>/dev/null || :
        wait "$pid" 2>/dev/null || :
    done
}
trap cleanup EXIT
trap 'exit 1' INT TERM

# The caller enters a private router namespace before any network changes.
# Use child namespaces for the two traffic endpoints.
unshare --net sh -c 'touch "$1"; exec sleep 40' sh "$configs/external-ready" &
external=$!
unshare --net sh -c 'touch "$1"; exec sleep 40' sh "$configs/container-ready" &
container=$!
while [ ! -f "$configs/external-ready" ] || [ ! -f "$configs/container-ready" ]; do
    kill -0 "$external" "$container"
    sleep 0.1
done

setup_link() {
    router_if=$1
    endpoint_if=$2
    pid=$3
    gateway=$4
    address=$5
    ip link add "$router_if" type veth peer name "$endpoint_if"
    ip link set "$endpoint_if" netns "$pid"
    ip addr add "$gateway/24" dev "$router_if"
    ip link set "$router_if" up
    nsenter --net="/proc/$pid/ns/net" ip addr add "$address/24" dev "$endpoint_if"
    nsenter --net="/proc/$pid/ns/net" ip link set "$endpoint_if" up
    nsenter --net="/proc/$pid/ns/net" ip route add default via "$gateway"
}

setup_link wan0 external0 "$external" 192.0.2.1 192.0.2.2
setup_link br-test container0 "$container" 198.51.100.1 198.51.100.2
sysctl -q -w net.ipv4.ip_forward=1

load() {
    printf 'Loading %s\n' "$1"
    nft -c -f "$configs/$1.nft"
    nft -f "$configs/$1.nft"
}

check_ping() {
    expected=$1
    source=$2
    destination=$3
    identifier=$4
    status=0
    nsenter --net="/proc/$source/ns/net" ping -4 -n -c 1 -W 1 -e "$identifier" "$destination" || status=$?
    if [ "$status" -ne "$expected" ]; then
        echo "ping to $destination returned $status, expected $expected" >&2
        nft list ruleset >&2
        exit 1
    fi
}

load default
check_ping 1 "$external" 198.51.100.2 12001
check_ping 1 "$container" 192.0.2.2 12002
load inbound
check_ping 0 "$external" 198.51.100.2 12003
check_ping 1 "$container" 192.0.2.2 12004
load outbound
check_ping 1 "$external" 198.51.100.2 12005
check_ping 0 "$container" 192.0.2.2 12006

# Model the later Docker filter hook with real conntrack matching. This is
# independent of the installed Docker version and daemon configuration.
nft -f - <<'NFT'
table inet docker_probe {
    counter accepted {}
    counter unmarked {}
    chain forward {
        type filter hook forward priority 0; policy accept;
        iifname "wan0" oifname "br-test" udp dport 18080 ct mark & 0x00800000 == 0x00800000 counter name accepted accept
        iifname "wan0" oifname "br-test" udp dport 18080 counter name unmarked drop
    }
}
NFT

send_packet() {
    nsenter --net="/proc/$external/ns/net" bash -c 'printf test > /dev/udp/198.51.100.2/18080'
}

check_counter() {
    value=$(nft list counter inet docker_probe "$1")
    if ! printf '%s\n' "$value" | grep -Eq "packets $2 bytes"; then
        printf 'Unexpected %s counter: %s\n' "$1" "$value" >&2
        exit 1
    fi
}

wait_for_packet() {
    for attempt in 1 2 3 4 5 6 7 8 9 10; do
        if "$@" | grep -q 'packets 1 bytes'; then
            return
        fi
        sleep 0.1
    done
    echo "No packet recorded by $*" >&2
    nft list ruleset >&2
    exit 1
}

load denied
send_packet
wait_for_packet nft list chain inet snail forward_source_allow
check_counter accepted 0
check_counter unmarked 0
load inbound
send_packet
wait_for_packet nft list counter inet docker_probe unmarked
check_counter accepted 0
check_counter unmarked 1
load marked
send_packet
wait_for_packet nft list counter inet docker_probe accepted
check_counter accepted 1
check_counter unmarked 1
