package configuration

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"

	"snail_tool/internal/input"
	"snail_tool/internal/system"
)

type Token struct {
	Text       string
	Start, End int
}

func Tokens(content string) ([]Token, error) {
	var result []Token
	for i := 0; i < len(content); {
		if unicode.IsSpace(rune(content[i])) {
			i++
			continue
		}
		if content[i] == '#' {
			for i < len(content) && content[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		switch content[i] {
		case '{', '}', ';':
			i++
		case '"', '\'':
			quote := content[i]
			i++
			for i < len(content) && content[i] != quote {
				if content[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(content) {
				return nil, fmt.Errorf("配置文件字符串未闭合")
			}
			i++
		default:
			for i < len(content) && !unicode.IsSpace(rune(content[i])) && !strings.ContainsRune("{};#", rune(content[i])) {
				i++
			}
		}
		result = append(result, Token{Text: content[start:i], Start: start, End: i})
	}
	return result, nil
}

func ClosingBrace(tokens []Token, open int) (int, error) {
	depth := 0
	for i := open; i < len(tokens); i++ {
		switch tokens[i].Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("配置文件花括号未闭合")
}

func ParsePortRange(raw string) (string, int, int, error) {
	if err := input.Text(raw); err != nil {
		return "", 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) < 1 || len(parts) > 2 {
		return "", 0, 0, fmt.Errorf("请输入单端口或端口范围，例如 443 或 8000-8100")
	}
	start, err := system.ValidatePort(strings.TrimSpace(parts[0]))
	if err != nil || start < 1 || start > 65535 {
		return "", 0, 0, fmt.Errorf("端口必须在 1-65535 之间")
	}
	end := start
	if len(parts) == 2 {
		end, err = system.ValidatePort(strings.TrimSpace(parts[1]))
		if err != nil || end < start || end > 65535 {
			return "", 0, 0, fmt.Errorf("端口范围必须递增且位于 1-65535 之间")
		}
	}
	normalized := strconv.Itoa(start)
	if end != start {
		normalized += "-" + strconv.Itoa(end)
	}
	return normalized, start, end, nil
}

func NormalizeInterface(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if err := input.Text(raw); err != nil {
		return "", err
	}
	if len(raw) > 15 {
		return "", fmt.Errorf("网卡名过长")
	}
	for _, r := range raw {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' && r != '*' {
			return "", fmt.Errorf("网卡名只能包含字母、数字、_ - . *")
		}
	}
	return raw, nil
}

func ProtocolLabel(protocol string) string {
	if protocol == "both" {
		return "TCP+UDP"
	}
	return strings.ToUpper(protocol)
}

func InputChainBounds(content string) (int, int, int, error) {
	tokens, err := Tokens(content)
	if err != nil {
		return 0, 0, 0, err
	}
	open, close, insertion := -1, -1, -1
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].Text != "table" || tokens[i+1].Text != "inet" || tokens[i+2].Text != "snail" || tokens[i+3].Text != "{" {
			continue
		}
		end, err := ClosingBrace(tokens, i+3)
		if err != nil {
			return 0, 0, 0, err
		}
		for j := i + 4; j+2 < end; j++ {
			if tokens[j].Text != "chain" || tokens[j+2].Text != "{" {
				continue
			}
			chainEnd, err := ClosingBrace(tokens, j+2)
			if err != nil {
				return 0, 0, 0, err
			}
			if tokens[j+1].Text == "input" {
				if open >= 0 {
					return 0, 0, 0, fmt.Errorf("发现多个 inet snail input 链，未修改规则")
				}
				open, close = tokens[j+2].End, tokens[chainEnd].Start
				if j+6 >= chainEnd || tokens[j+3].Text != "type" || tokens[j+4].Text != "filter" || tokens[j+5].Text != "hook" || tokens[j+6].Text != "input" {
					return 0, 0, 0, fmt.Errorf("配置中的 inet snail input 链未挂接入站过滤 hook，未修改规则")
				}
				// Only declarations may precede the insertion point. This avoids
				// persisting an allow rule behind an earlier terminal verdict.
				declarationEnd := j + 3
				for declarationEnd < chainEnd && tokens[declarationEnd].Text != ";" {
					declarationEnd++
				}
				for k := j + 3; k+2 < chainEnd; k++ {
					if tokens[k].Text == "policy" && (tokens[k+1].Text == "drop" || tokens[k+1].Text == "accept") && tokens[k+2].Text == ";" {
						if k != declarationEnd+1 {
							return 0, 0, 0, fmt.Errorf("input 链声明顺序无法安全识别，未修改规则")
						}
						insertion = tokens[k+2].End
						break
					}
				}
			}
			j = chainEnd
		}
		i = end
	}
	if insertion < 0 {
		return 0, 0, 0, fmt.Errorf("配置中未找到 inet snail input 基础链及默认策略，请先生成防火墙配置")
	}
	return open, close, insertion, nil
}

func NormalizeSource(raw string) (string, error) {
	if err := input.Text(raw); err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Zone() != "" {
			return "", fmt.Errorf("来源地址不能包含接口区域标识")
		}
		return addr.Unmap().String(), nil
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return "", fmt.Errorf("请输入有效的 IPv4、IPv6 或 CIDR 来源地址")
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return "", fmt.Errorf("IPv4 映射地址的网段前缀不能小于 96")
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	prefix = prefix.Masked()
	if prefix.Bits() == prefix.Addr().BitLen() {
		return prefix.Addr().String(), nil
	}
	return prefix.String(), nil
}

func SourcePrefix(source string) netip.Prefix {
	if source == "" {
		return netip.Prefix{}
	}
	if addr, err := netip.ParseAddr(source); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen())
	}
	prefix, _ := netip.ParsePrefix(source)
	return prefix.Masked()
}

func SameTokens(a, b string) bool {
	x, err := Tokens(a)
	if err != nil {
		return false
	}
	y, err := Tokens(b)
	if err != nil || len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i].Text != y[i].Text {
			return false
		}
	}
	return true
}

// A Ping block must precede all rules, including established-connection accepts.
// Otherwise an ongoing Ping stream could bypass the disabled setting.
