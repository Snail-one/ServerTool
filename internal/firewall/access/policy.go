package access

import (
	"errors"

	"snail_tool/internal/firewall/configuration"
	"snail_tool/internal/firewall/internal/rules"
)

var Lists = inputPolicyLists()

func inputPolicyLists() []rules.PolicyList {
	var lists []rules.PolicyList
	for _, public := range []bool{true, false} {
		for _, set := range configuration.InputPortSets(public) {
			lists = append(lists, rules.PolicyList{Name: set.Name, Label: set.Label, NamedSet: true})
		}
	}
	return append(lists,
		rules.PolicyList{Name: "snail_private_ipv4", Label: "IPv4 内网范围", Family: 4, Defaults: "172.16.0.0/12, 192.168.0.0/16"},
		rules.PolicyList{Name: "snail_private_ipv6", Label: "IPv6 内网范围", Family: 6, Defaults: "fc00::/7, fe80::/10"},
	)
}

func Service(options rules.Options) rules.Session {
	return rules.NewAccess(options, Title, Label, Lists, SummaryFields, MigrateGlobalPortRule)
}

func MigrateGlobalPortRule(content, id string) (string, error) {
	managed, err := rules.ManagedRules(content)
	if err != nil {
		return "", err
	}
	var target rules.Rule
	var remaining []rules.Rule
	for _, rule := range managed {
		if rule.ID == id {
			target = rule
		} else {
			remaining = append(remaining, rule)
		}
	}
	if target.Kind != "port" {
		return "", errors.New("请选择已有的全来源端口例外")
	}
	updated := content
	for i, list := range Lists[:8] {
		family := 1
		if i%4 >= 2 {
			family = 2
		}
		protocol := "tcp"
		if i%2 == 1 {
			protocol = "udp"
		}
		if target.FamilyMask()&family == 0 || target.Protocol != "both" && target.Protocol != protocol {
			continue
		}
		current, err := rules.ReadPolicyValue(updated, list)
		if err != nil {
			return "", err
		}
		value, err := rules.ChangePolicyValue(list, current, "add", target.Ports, -1)
		if err != nil {
			return "", err
		}
		updated, err = rules.WithPolicyValue(updated, list, value)
		if err != nil {
			return "", err
		}
	}
	return rules.ReplaceManagedRules(updated, remaining)
}
