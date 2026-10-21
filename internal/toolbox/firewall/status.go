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
	fields := []ui.CardField{
		{Label: "nftables", Value: software},
		{Label: "配置文件", Value: configPath},
	}
	data, fileErr := os.ReadFile(configPath)
	switch {
	case os.IsNotExist(fileErr):
		fields = append(fields, ui.CardField{Label: "文件状态", Value: "尚未生成配置"})
	case fileErr != nil:
		fields = append(fields, ui.CardField{Label: "文件状态", Value: "读取失败", Detail: fileErr.Error()})
	default:
		fileStatus := "已保存 · 来源未标记"
		if strings.TrimSpace(string(data)) == "" {
			fileStatus = "空文件 · 请先生成或编辑配置"
		} else if strings.Contains(string(data), "# ===== BEGIN SNAIL NFTABLES CONFIG =====") && strings.Contains(string(data), "# ===== END SNAIL NFTABLES CONFIG =====") {
			fileStatus = "已保存 · 含本工具标记"
		}
		fields = append(fields, ui.CardField{Label: "文件状态", Value: fileStatus})
		if info, err := os.Stat(configPath); err == nil {
			fields = append(fields, ui.CardField{Label: "最后修改", Value: info.ModTime().Format("2006-01-02 15:04:05 MST")})
		}
	}
	switch {
	case !installed:
		fields = append(fields, ui.CardField{Label: "内核规则", Value: "无法查询 · nft 未安装"})
	case live.err != nil:
		fields = append(fields, ui.CardField{Label: "内核规则", Value: "检测失败", Detail: live.err.Error()})
	default:
		fields = append(fields, ui.CardField{Label: "内核规则", Value: fmt.Sprintf("%d 个表 · %d 条链 · %d 条规则", live.tables, live.chains, live.rules)})
		tableState := "未加载 inet snail 表"
		if live.snailLoaded {
			tableState = "已加载 inet snail 表"
		}
		fields = append(fields, ui.CardField{Label: "本工具表", Value: tableState})
		if live.snailLoaded {
			policy := live.inputPolicy
			if policy == "" {
				policy = "未检测到 input 基础链"
			}
			fields = append(fields, ui.CardField{Label: "本表入站策略", Value: policy})
		}
	}
	fields = append(fields, configSyncField(data, fileErr, installed, live))
	return append(fields,
		ui.CardField{Label: "生成配置", Value: "校验、备份并写入后自动加载"},
		ui.CardField{Label: "手动编辑", Value: "退出后自动校验；选择 5 加载最新配置"},
	)
}
