package loadedrules

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Snapshot struct {
	Version          string
	Tables           int
	Chains           int
	Rules            int
	SnailLoaded      bool
	SnailDormant     bool
	SnailChains      int
	SnailRules       int
	InputActive      bool
	OtherInputChains int
	InputPolicy      string
	Err              error
	JSON             string
}

type nftObject struct {
	Meta *struct {
		Version string `json:"version"`
	} `json:"metainfo"`
	Table *struct {
		Family string   `json:"family"`
		Name   string   `json:"name"`
		Flags  []string `json:"flags"`
	} `json:"table"`
	Chain *struct {
		Family string `json:"family"`
		Table  string `json:"table"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Hook   string `json:"hook"`
		Policy string `json:"policy"`
	} `json:"chain"`
	Rule *struct {
		Family string `json:"family"`
		Table  string `json:"table"`
	} `json:"rule"`
}

func Inspect(commandOutput func(string, ...string) (string, error)) Snapshot {
	out, err := commandOutput("nft", "-j", "list", "ruleset")
	if err != nil {
		return Snapshot{Err: fmt.Errorf("%w: %s", err, strings.Join(strings.Fields(out), " "))}
	}
	return Parse(out)
}

func Parse(output string) Snapshot {
	var document struct {
		Objects []nftObject `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		return Snapshot{Err: fmt.Errorf("无法解析 nftables 状态: %w", err)}
	}
	if document.Objects == nil {
		return Snapshot{Err: fmt.Errorf("nftables 状态缺少规则集数据")}
	}
	result := Snapshot{JSON: output}
	var policies []string
	for _, object := range document.Objects {
		if object.Meta != nil {
			result.Version = object.Meta.Version
		}
		if object.Table != nil {
			result.Tables++
			if object.Table.Family == "inet" && object.Table.Name == "snail" {
				result.SnailLoaded = true
				for _, flag := range object.Table.Flags {
					if flag == "dormant" {
						result.SnailDormant = true
					}
				}
			}
		}
		if object.Chain != nil {
			result.Chains++
			chain := object.Chain
			owned := chain.Family == "inet" && chain.Table == "snail"
			if owned {
				result.SnailChains++
			}
			if chain.Type != "filter" || chain.Hook != "input" {
				continue
			}
			if owned {
				result.InputActive = true
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
			} else {
				result.OtherInputChains++
			}
		}
		if object.Rule != nil {
			result.Rules++
			if object.Rule.Family == "inet" && object.Rule.Table == "snail" {
				result.SnailRules++
			}
		}
	}
	result.InputActive = result.InputActive && result.SnailLoaded && !result.SnailDormant
	sort.Strings(policies)
	result.InputPolicy = strings.Join(policies, "；")
	return result
}
