package firewall

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLoadedPortsSeparatesDockerMappingsFromInputPolicy(t *testing.T) {
	setupTest(t)
	commandExists = func(name string) bool { return name == "nft" || name == "docker" }
	ids := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	var queries []string
	commandOutput = func(name string, args ...string) (string, error) {
		queries = append(queries, name+" "+strings.Join(args, " "))
		switch {
		case name == "nft" && reflect.DeepEqual(args, []string{"-j", "list", "ruleset"}):
			return portFixture(t), nil
		case name == "docker" && reflect.DeepEqual(args, []string{"ps", "--quiet", "--no-trunc"}):
			return strings.Join(ids, "\n"), nil
		case name == "docker" && reflect.DeepEqual(args, append([]string{"inspect", "--type", "container", "--format", dockerPortsFormat}, ids...)):
			return `{"name":"/web","running":true,"networkMode":"bridge","ports":{"22/tcp":[{"HostIp":"0.0.0.0","HostPort":"22"},{"HostIp":"::","HostPort":"22"}],"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}],"53/udp":[{"HostIp":"192.168.1.10","HostPort":"5353"}],"9999/tcp":null}}
{"name":"/host-app","running":true,"networkMode":"host","ports":{}}
{"name":"/stopped","running":false,"networkMode":"bridge","ports":{"9000/tcp":[{"HostIp":"0.0.0.0","HostPort":"9000"}]}}`, nil
		default:
			t.Fatalf("unexpected query: %s %v", name, args)
			return "", nil
		}
	}
	output := capturePortOutput(t, func() {
		if err := showLoadedPorts(); err != nil {
			t.Fatal(err)
		}
	})
	if len(queries) != 3 {
		t.Fatalf("expected only nft read, docker ps and inspect: %v", queries)
	}
	parts := strings.SplitN(output, "Docker 已发布端口", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], "443, 2222, 8000-8002, 8443") || strings.Contains(parts[0], "22 → 22 / TCP") {
		t.Fatalf("Docker mappings must stay separate from native policy: %s", output)
	}
	for _, want := range []string{"web", "└─ 22 → 22 / TCP · 全部地址（IPv4 / IPv6）", "└─ 8080 → 80 / TCP · 回环地址（127.0.0.1）", "5353 → 53 / UDP · 指定地址（192.168.1.10）", "host-app", "需查看服务实际监听端口", "不代表公网一定可达"} {
		if !strings.Contains(parts[1], want) {
			t.Fatalf("missing %q in %s", want, parts[1])
		}
	}
	if strings.Count(parts[1], "web") != 3 || strings.Count(parts[1], "22 → 22 / TCP") != 1 {
		t.Fatalf("container must appear once per scope, with dual-stack mapping merged: %s", parts[1])
	}
	var starts []int
	for _, heading := range []string{"Docker 已发布端口 · 全部地址", "Docker 已发布端口 · 回环地址", "Docker 已发布端口 · 指定地址", "Docker host 网络容器"} {
		index := strings.Index(output, heading)
		if index < 0 || (len(starts) > 0 && index <= starts[len(starts)-1]) {
			t.Fatalf("Docker scopes must be separate and ordered: %s", output)
		}
		starts = append(starts, index)
	}
	all, loopback := output[starts[0]:starts[1]], output[starts[1]:starts[2]]
	if !strings.Contains(all, "22 → 22 / TCP") || strings.Contains(all, "8080 →") || strings.Contains(all, "5353 →") || strings.Contains(all, "host-app") {
		t.Fatalf("all-address card contains another binding scope: %s", all)
	}
	if !strings.Contains(loopback, "8080 → 80 / TCP") || strings.Contains(loopback, "22 →") || strings.Contains(loopback, "5353 →") {
		t.Fatalf("loopback card contains another binding scope: %s", loopback)
	}
	for _, absent := range []string{"9999", "9000", "stopped"} {
		if strings.Contains(parts[1], absent) {
			t.Fatalf("EXPOSE-only or stopped container port included: %s", parts[1])
		}
	}
}

func TestDockerPortsStillDisplayWithoutSummarizableSnailRules(t *testing.T) {
	unsupported := editPortFixture(t, func(objects []map[string]any) []map[string]any {
		for _, object := range objects {
			if chain, ok := object["chain"].(map[string]any); ok && chain["name"] == "input" && chain["table"] == "snail" {
				chain["policy"] = "unknown"
			}
		}
		return objects
	})
	for name, native := range map[string]string{"missing table": `{"nftables":[]}`, "unsupported policy": unsupported} {
		t.Run(name, func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" {
					return native, nil
				}
				if name == "docker" && reflect.DeepEqual(args, []string{"ps", "--quiet", "--no-trunc"}) {
					return "", nil
				}
				t.Fatalf("unexpected query: %s %v", name, args)
				return "", nil
			}
			output := capturePortOutput(t, func() {
				if err := showLoadedPorts(); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(output, "Docker 已发布端口") || !strings.Contains(output, "未发现运行中容器的已发布端口") {
				t.Fatalf("Docker section missing without native summary: %s", output)
			}
		})
	}
}

func TestDockerQueryFailureDoesNotHideInputPolicyOrClaimNoPorts(t *testing.T) {
	for _, stage := range []string{"ps", "inspect", "invalid ID", "invalid JSON", "incomplete inspect", "invalid binding"} {
		t.Run(stage, func(t *testing.T) {
			setupTest(t)
			commandExists = func(string) bool { return true }
			commandOutput = func(name string, args ...string) (string, error) {
				if name == "nft" {
					return portFixture(t), nil
				}
				if name != "docker" {
					t.Fatalf("unexpected query: %s %v", name, args)
				}
				if args[0] == "ps" {
					if stage == "ps" {
						return "daemon unavailable", errors.New("failed")
					}
					if stage == "invalid ID" {
						return "--unexpected-option", nil
					}
					return strings.Repeat("a", 64), nil
				}
				switch stage {
				case "inspect":
					return "container disappeared", errors.New("failed")
				case "invalid JSON":
					return "invalid", nil
				case "incomplete inspect":
					return "", nil
				default:
					return `{"name":"/web","running":true,"ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"70000"}]}}`, nil
				}
			}
			output := capturePortOutput(t, func() {
				if err := showLoadedPorts(); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(output, "公网 IPv4 TCP") || !strings.Contains(output, "无法读取") || strings.Contains(output, "未发现运行中容器的已发布端口") {
				t.Fatalf("Docker failure must preserve native policy and remain explicit: %s", output)
			}
		})
	}
}

func TestDockerRoutedPortDoesNotInventHostMapping(t *testing.T) {
	sections, err := dockerPortSections([]dockerPortContainer{{Name: "/routed", Ports: map[string][]dockerPortBinding{
		"80/tcp": {{HostIP: "::", HostPort: ""}},
	}}})
	if err != nil || len(sections) != 1 || sections[0].scope != "直接路由" || len(sections[0].fields) != 2 || sections[0].fields[1].Value != "└─ 80 / TCP · 直接路由（IPv6，无宿主机映射端口）" {
		t.Fatalf("routed publication misrepresented: %+v %v", sections, err)
	}
}

func TestDockerPortGroupingPreservesBindingsProtocolsAndTargets(t *testing.T) {
	rows, err := dockerContainerPortRows(dockerPortContainer{Name: "/app", Ports: map[string][]dockerPortBinding{
		"80/tcp": {
			{HostIP: "0.0.0.0", HostPort: "8080"},
			{HostIP: "::", HostPort: "8080"},
			{HostIP: "0.0.0.0", HostPort: "8080"}, // Duplicate binding.
			{HostIP: "127.0.0.1", HostPort: "8080"},
			{HostIP: "::1", HostPort: "8080"},
			{HostIP: "192.0.2.1", HostPort: "8080"},
			{HostIP: "2001:db8::1", HostPort: "8080"},
			{HostIP: "", HostPort: "8080"},
			{HostIP: "::", HostPort: "8081"},
			{HostIP: "::", HostPort: ""}, // Direct routing, not port 8080.
		},
		"80/udp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
		"81/tcp": {{HostIP: "::", HostPort: "8080"}},
		"443/tcp": {
			{HostIP: "0.0.0.0", HostPort: "9000"},
			{HostIP: "::", HostPort: "10000"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"8080 → 80 / TCP · 全部地址（IPv4 / IPv6）",
		"8080 → 80 / TCP · 回环地址（127.0.0.1、::1）",
		"8080 → 80 / TCP · 指定地址（192.0.2.1、2001:db8::1）",
		"8080 → 80 / TCP · 地址未指定",
		"8080 → 80 / UDP · 全部地址（IPv4）",
		"8080 → 81 / TCP · 全部地址（IPv6）",
		"8081 → 80 / TCP · 全部地址（IPv6）",
		"9000 → 443 / TCP · 全部地址（IPv4）",
		"10000 → 443 / TCP · 全部地址（IPv6）",
		"80 / TCP · 直接路由（IPv6，无宿主机映射端口）",
	}
	got := make(map[string]int)
	for _, values := range rows {
		for _, row := range values {
			got[row]++
		}
	}
	if len(got) != len(want) {
		t.Fatalf("bindings incorrectly combined or duplicated: %v", rows)
	}
	for _, row := range want {
		if got[row] != 1 {
			t.Fatalf("expected mapping exactly once: %s; got %v", row, rows)
		}
	}
	if !reflect.DeepEqual(rows["全部地址"], []string{want[0], want[4], want[5], want[6], want[7], want[8]}) || !reflect.DeepEqual(rows["直接路由"], []string{want[9]}) {
		t.Fatalf("ports must sort numerically within scope, with routed ports separate: %v", rows)
	}
}

func TestDockerInspectBatchesManyContainers(t *testing.T) {
	setupTest(t)
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = strings.Repeat("a", 64)
	}
	var batchSizes []int
	commandOutput = func(name string, args ...string) (string, error) {
		if name != "docker" {
			t.Fatalf("unexpected command: %s", name)
		}
		if reflect.DeepEqual(args, []string{"ps", "--quiet", "--no-trunc"}) {
			return strings.Join(ids, "\n"), nil
		}
		if len(args) < 6 || !reflect.DeepEqual(args[:5], []string{"inspect", "--type", "container", "--format", dockerPortsFormat}) {
			t.Fatalf("unexpected query: %v", args)
		}
		batchSizes = append(batchSizes, len(args)-5)
		return strings.Repeat("{\"name\":\"/app\",\"running\":true,\"ports\":null}\n", len(args)-5), nil
	}
	containers, err := inspectDockerPorts()
	if err != nil || len(containers) != 101 || !reflect.DeepEqual(batchSizes, []int{100, 1}) {
		t.Fatalf("containers=%d batches=%v err=%v", len(containers), batchSizes, err)
	}
}
