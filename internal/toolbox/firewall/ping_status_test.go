package firewall

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func echoTestRule(chain, expressions string) string {
	return fmt.Sprintf(`{"rule":{"family":"inet","table":"snail","chain":%q,"expr":[%s]}}`, chain, expressions)
}

func echoTestRules(objects ...string) string {
	return `{"nftables":[{"table":{"family":"inet","name":"snail"}},` +
		`{"chain":{"family":"inet","table":"snail","name":"input","type":"filter","hook":"input","policy":"drop"}},` +
		strings.Join(objects, ",") + `]}`
}

func TestLoadedEchoReadsActualRulesAndTheirOrder(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		want := echoBlock
		if !ipv6 {
			// This fixture includes a manual all-protocol IPv4 source exception.
			want |= echoAllow
		}
		if state := loadedEchoState(portFixture(t), ipv6); state != want {
			t.Fatalf("source exceptions in legacy fixture were lost: ipv6=%v state=%d", ipv6, state)
		}
	}
	request4 := `{"match":{"op":"==","left":{"payload":{"protocol":"icmp","field":"type"}},"right":"echo-request"}}`
	request6 := strings.Replace(request4, `"icmp"`, `"icmpv6"`, 1)
	loopback := `{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"lo"}}`
	established := `{"match":{"op":"in","left":{"ct":{"key":"state"}},"right":{"set":["established","related"]}}}`
	source := `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"192.0.2.1"}}`
	for _, scenario := range []struct {
		name   string
		rules  []string
		v4, v6 int
	}{
		{"both allowed", []string{echoTestRule("input", request4+`,{"accept":null}`), echoTestRule("input", request6+`,{"accept":null}`)}, echoAllow, echoAllow},
		{"both blocked", []string{echoTestRule("input", `{"drop":null}`)}, echoBlock, echoBlock},
		{"families differ", []string{echoTestRule("input", request4+`,{"accept":null}`)}, echoAllow, echoBlock},
		{"earlier drop wins", []string{echoTestRule("input", `{"drop":null}`), echoTestRule("input", request4+`,{"accept":null}`)}, echoBlock, echoBlock},
		{"loopback is not external", []string{echoTestRule("input", loopback+`,{"accept":null}`)}, echoBlock, echoBlock},
		{"established does not allow new Ping", []string{echoTestRule("input", established+`,{"accept":null}`)}, echoBlock, echoBlock},
		{"control messages only", []string{echoTestRule("input", strings.Replace(request6, "echo-request", "nd-neighbor-solicit", 1)+`,{"accept":null}`)}, echoBlock, echoBlock},
		{"conditional source", []string{echoTestRule("input", source+`,`+request4+`,{"accept":null}`)}, echoAllow | echoBlock, echoBlock},
		{"unsupported rate limit", []string{echoTestRule("input", request4+`,{"limit":{"rate":5}},{"accept":null}`)}, echoUnknown, echoBlock},
		{"jump returns to fallback", []string{`{"chain":{"family":"inet","table":"snail","name":"extra"}}`, echoTestRule("input", `{"jump":{"target":"extra"}}`), echoTestRule("extra", `{"return":null}`)}, echoBlock, echoBlock},
		{"jump accepts", []string{`{"chain":{"family":"inet","table":"snail","name":"extra"}}`, echoTestRule("input", `{"jump":{"target":"extra"}}`), echoTestRule("extra", request4+`,{"accept":null}`)}, echoAllow, echoBlock},
		{"cycle is unknown", []string{echoTestRule("input", `{"jump":{"target":"input"}}`)}, echoUnknown, echoUnknown},
		{"other input hook is unknown", []string{`{"chain":{"family":"inet","table":"snail","name":"extra","hook":"input"}}`}, echoUnknown, echoUnknown},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			output := echoTestRules(scenario.rules...)
			if v4, v6 := loadedEchoState(output, false), loadedEchoState(output, true); v4 != scenario.v4 || v6 != scenario.v6 {
				t.Fatalf("states=(%d,%d) want=(%d,%d)", v4, v6, scenario.v4, scenario.v6)
			}
		})
	}
}

func TestEchoCardsSeparateSavedSwitchFromLoadedBlocking(t *testing.T) {
	setupTest(t)
	version := ruleTestConfig(t, nil)
	allowed, _ := withPingSetting(string(version.content), true)
	if err := os.WriteFile(configPath, []byte(allowed), 0600); err != nil {
		t.Fatal(err)
	}
	// File allows Ping, but the loaded input chain still drops it.
	loaded := echoTestRules(echoTestRule("input", `{"drop":null}`))
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}) {
			t.Fatalf("unexpected query: %s %v", name, args)
		}
		return loaded, nil
	}
	fields := currentConfigFields(autostartStatus{})
	if fieldValue(fields, "ICMP 回显") != "已拦截" || fieldValue(fields, "ICMPv6 回显") != "已拦截" {
		t.Fatal("main card inferred loaded state from saved switch")
	}
	output := capturePortOutput(t, func() {
		if err := configurePing(inputUI(t, "q\n")); err == nil {
			t.Fatal("expected return sentinel")
		}
	})
	for _, want := range []string{"ICMP 回显：已拦截", "保存的开关：已允许", "配置同步：无法比较"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in card: %s", want, output)
		}
	}
	for _, removed := range []string{"作用范围：", "保留流量：", "生效方式：", "连通条件："} {
		if strings.Contains(output, removed) {
			t.Fatalf("static explanation still in compact card: %s", removed)
		}
	}
	for _, live := range []liveRules{{}, {err: errors.New("denied")}} {
		for _, field := range echoStateFields(true, live) {
			if field.Value == "已允许" || field.Value == "已拦截" {
				t.Fatal("inactive or unreadable firewall was assigned an active verdict")
			}
		}
	}
}
