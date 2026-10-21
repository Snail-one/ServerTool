package firewall

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"snail_tool/internal/ui"
)

type liveRules struct {
	version     string
	tables      int
	chains      int
	rules       int
	snailLoaded bool
	inputPolicy string
	err         error
	json        string
}

type nftObject struct {
	Meta *struct {
		Version string `json:"version"`
	} `json:"metainfo"`
	Table *struct {
		Family string `json:"family"`
		Name   string `json:"name"`
	} `json:"table"`
	Chain *struct {
		Family string `json:"family"`
		Table  string `json:"table"`
		Name   string `json:"name"`
		Hook   string `json:"hook"`
		Policy string `json:"policy"`
	} `json:"chain"`
	Rule json.RawMessage `json:"rule"`
}

func inspectLiveRules() liveRules {
	out, err := commandOutput("nft", "-j", "list", "ruleset")
	if err != nil {
		return liveRules{err: fmt.Errorf("%w: %s", err, strings.Join(strings.Fields(out), " "))}
	}
	return parseLiveRules(out)
}

func parseLiveRules(output string) liveRules {
	var document struct {
		Objects []nftObject `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		return liveRules{err: fmt.Errorf("无法解析 nftables 状态: %w", err)}
	}
	if document.Objects == nil {
		return liveRules{err: fmt.Errorf("nftables 状态缺少规则集数据")}
	}
	result := liveRules{json: output}
	var policies []string
	for _, object := range document.Objects {
		if object.Meta != nil {
			result.version = object.Meta.Version
		}
		if object.Table != nil {
			result.tables++
			if object.Table.Family == "inet" && object.Table.Name == "snail" {
				result.snailLoaded = true
			}
		}
		if object.Chain != nil {
			result.chains++
			chain := object.Chain
			if chain.Family == "inet" && chain.Table == "snail" && chain.Hook == "input" {
				policy := chain.Policy
				switch policy {
				case "drop":
					policy = "默认丢弃（drop）"
				case "accept":
					policy = "默认放行（accept）"
				case "":
					policy = "未返回默认策略"
				}
				policies = append(policies, chain.Name+"："+policy)
			}
		}
		if len(object.Rule) != 0 && string(object.Rule) != "null" {
			result.rules++
		}
	}
	sort.Strings(policies)
	result.inputPolicy = strings.Join(policies, "；")
	return result
}

func currentConfigFields() []ui.CardField {
	installed := commandExists("nft")
	live := liveRules{}
	if installed {
		live = inspectLiveRules()
	}
	software := ui.InstallationBadge(installed)
	if live.version != "" {
		software += " v" + strings.TrimPrefix(live.version, "v")
	}
	fields := []ui.CardField{{Label: "nftables", Value: software}}
	switch {
	case !installed:
		return append(fields, ui.CardField{Label: "规则状态", Value: "未安装"})
	case live.err != nil:
		return append(fields, ui.CardField{Label: "规则状态", Value: "检测失败", Detail: live.err.Error()})
	default:
		fields = append(fields, ui.CardField{Label: "已加载规则", Value: fmt.Sprintf("%d 个表 · %d 条链 · %d 条规则", live.tables, live.chains, live.rules)})
		policy := "未加载本工具规则"
		if live.snailLoaded {
			policy = strings.TrimPrefix(live.inputPolicy, "input：")
			if policy == "" {
				policy = "未配置本工具入站链"
			}
		}
		fields = append(fields, ui.CardField{Label: "入站策略", Value: policy})
	}
	data, fileErr := os.ReadFile(configPath)
	sync := configSyncField(data, fileErr, installed, live)
	state, _, _ := strings.Cut(sync.Value, " · ")
	if state == "已同步" || state == "未同步" {
		sync.Value, sync.Detail = state, ""
	}
	return append(fields, sync)
}
