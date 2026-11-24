package listeners

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"snail_tool/internal/ui"
)

type Listener struct {
	Protocol string
	State    string
	RecvQ    string
	SendQ    string
	Local    string
	Peer     string
	Port     int
	Process  string
	Titles   []string
}

var (
	processPattern = regexp.MustCompile(`\("([^"]+)",pid=(\d+),fd=(\d+)\)`)
	cgroupPattern  = regexp.MustCompile(`\bcgroup:(\S+)`)
	pidPattern     = regexp.MustCompile(`pid=\d+`)
)

func Show(commandOutput func(string, ...string) (string, error)) error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "开放端口")
	out, err := commandOutput("ss", "-H", "-lntup")
	if err != nil {
		return fmt.Errorf("读取监听端口失败（需要 ss 和 root 权限）: %w: %s", err, strings.TrimSpace(out))
	}
	found := Parse(out, processTitle)
	if len(found) == 0 {
		ui.PrintInfoCard("当前没有 TCP/UDP 监听端口", ui.CardField{Label: "数据来源", Value: "ss -H -lntup"})
		return nil
	}
	tcp, udp := 0, 0
	for _, item := range found {
		if item.Protocol == "udp" {
			udp++
			continue
		}
		tcp++
	}
	ui.PrintInfoCard("监听套接字",
		ui.CardField{Label: "数据来源", Value: "当前系统监听套接字"},
		ui.CardField{Label: "TCP", Value: strconv.Itoa(tcp)},
		ui.CardField{Label: "UDP", Value: strconv.Itoa(udp)},
	)
	fmt.Println()
	fmt.Println(ui.InfoText(formatRow("协议", "状态", "接收", "发送", "本地地址:端口", "对端地址:端口", "进程")))
	for _, line := range formatListeners(found) {
		fmt.Println(line)
	}
	fmt.Println()
	fmt.Println("以上是程序正在监听的地址和端口，不代表防火墙已经放行。")
	return nil
}

func Parse(output string, title func(pid string) string) []Listener {
	var found []Listener
	for _, line := range strings.Split(output, "\n") {
		item, ok := parseLine(line, title)
		if ok {
			found = append(found, item)
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Port != found[j].Port {
			return found[i].Port < found[j].Port
		}
		if found[i].Protocol != found[j].Protocol {
			return found[i].Protocol < found[j].Protocol
		}
		return found[i].Local < found[j].Local
	})
	return found
}

func parseLine(line string, title func(pid string) string) (Listener, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return Listener{}, false
	}
	protocol := strings.ToLower(fields[0])
	if protocol != "tcp" && protocol != "udp" {
		return Listener{}, false
	}
	_, port, ok := splitEndpoint(fields[4])
	if !ok {
		return Listener{}, false
	}
	found := processes(line)
	process := "未知"
	if len(found) > 0 {
		process = formatProcesses(found)
	} else if service := cgroupService(line); service != "" {
		process = service
	}
	return Listener{
		Protocol: protocol,
		State:    fields[1],
		RecvQ:    fields[2],
		SendQ:    fields[3],
		Local:    fields[4],
		Peer:     fields[5],
		Port:     port,
		Process:  process,
		Titles:   processTitles(found, title),
	}, true
}

type processInfo struct {
	name string
	pid  string
	fd   string
}

func processes(line string) []processInfo {
	matches := processPattern.FindAllStringSubmatch(line, -1)
	found := make([]processInfo, 0, len(matches))
	for _, match := range matches {
		found = append(found, processInfo{name: match[1], pid: match[2], fd: match[3]})
	}
	return found
}

func formatProcesses(found []processInfo) string {
	parts := make([]string, len(found))
	for index, item := range found {
		parts[index] = `("` + item.name + `",pid=` + item.pid + `,fd=` + item.fd + `)`
	}
	return "users:(" + strings.Join(parts, ",") + ")"
}

func processTitles(found []processInfo, title func(pid string) string) []string {
	if title == nil {
		return nil
	}
	var titles []string
	seen := map[string]bool{}
	for _, item := range found {
		if seen[item.pid] {
			continue
		}
		seen[item.pid] = true
		text := strings.TrimSpace(title(item.pid))
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		titles = append(titles, text)
	}
	return titles
}

func processTitle(pid string) string {
	if text := readProcText("/proc/" + pid + "/cmdline"); text != "" {
		return text
	}
	return readProcText("/proc/" + pid + "/comm")
}

func readProcText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.ReplaceAll(string(data), "\x00", " ")
	return strings.TrimSpace(text)
}

func splitEndpoint(endpoint string) (string, int, bool) {
	prefix, portText, ok := splitPort(endpoint)
	if !ok {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	address := strings.TrimSuffix(prefix, ":")
	if !strings.HasPrefix(address, "[") {
		return address, port, true
	}
	end := strings.IndexByte(address, ']')
	if end < 0 {
		return "", 0, false
	}
	if end == len(address)-1 {
		return address[1:end], port, true
	}
	return address[1:end] + address[end+1:], port, true
}

// splitPort separates an ss endpoint into the address prefix, including the
// colon, and the port text. IPv6 zones stay in the prefix: [addr]%zone:port.
func splitPort(endpoint string) (string, string, bool) {
	if strings.HasPrefix(endpoint, "[") {
		end := strings.IndexByte(endpoint, ']')
		if end < 0 {
			return "", "", false
		}
		rest := endpoint[end+1:]
		if strings.HasPrefix(rest, "%") {
			zone, portText, found := strings.Cut(rest[1:], ":")
			if !found || zone == "" || portText == "" {
				return "", "", false
			}
			return endpoint[:len(endpoint)-len(portText)], portText, true
		}
		if !strings.HasPrefix(rest, ":") || len(rest) < 2 {
			return "", "", false
		}
		return endpoint[:end+2], rest[1:], true
	}
	colon := strings.LastIndexByte(endpoint, ':')
	if colon <= 0 || colon == len(endpoint)-1 {
		return "", "", false
	}
	return endpoint[:colon+1], endpoint[colon+1:], true
}

func cgroupService(line string) string {
	match := cgroupPattern.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	name := match[1]
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	return strings.TrimSuffix(name, ".service")
}

func formatListeners(found []Listener) []string {
	var lines []string
	for index, item := range found {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, formatRow(item.Protocol, item.State, item.RecvQ, item.SendQ, item.Local, item.Peer, item.Process))
		for _, title := range item.Titles {
			lines = append(lines, "    └─ "+ui.PurpleText(title))
		}
	}
	return lines
}

func formatRow(protocol, state, recv, send, local, peer, process string) string {
	return padColored(protocol, colorProtocol(protocol), 6) +
		padColored(state, colorState(state), 8) +
		pad(recv, 6) +
		pad(send, 6) +
		padColored(local, colorEndpoint(local), 46) +
		padColored(peer, colorEndpoint(peer), 18) +
		colorProcess(process)
}

func colorProtocol(protocol string) string {
	switch protocol {
	case "tcp", "udp":
		return ui.CyanText(protocol)
	default:
		return protocol
	}
}

func colorState(state string) string {
	switch state {
	case "LISTEN", "UNCONN":
		return ui.SuccessText(state)
	default:
		return state
	}
}

func colorEndpoint(endpoint string) string {
	prefix, port, ok := splitPort(endpoint)
	if !ok || !allDigits(port) {
		return endpoint
	}
	return prefix + ui.LightBlueText(port)
}

func colorProcess(process string) string {
	return pidPattern.ReplaceAllStringFunc(process, func(match string) string {
		return "pid=" + ui.WarningText(strings.TrimPrefix(match, "pid="))
	})
}

func allDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, current := range text {
		if current < '0' || current > '9' {
			return false
		}
	}
	return true
}

func pad(text string, width int) string {
	return padColored(text, text, width)
}

func padColored(plain, colored string, width int) string {
	padding := width - displayWidth(plain)
	if padding < 1 {
		padding = 1
	}
	return colored + strings.Repeat(" ", padding)
}

func displayWidth(text string) int {
	width := 0
	for _, current := range text {
		if current >= 0x2e80 {
			width += 2
			continue
		}
		width++
	}
	return width
}
