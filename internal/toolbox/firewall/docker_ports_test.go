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
	if len(parts) != 2 || !strings.Contains(parts[0], "443, 2222, 8000-8002, 8443") || strings.Contains(parts[0], "0.0.0.0:22") {
		t.Fatalf("Docker mappings must stay separate from native policy: %s", output)
	}
	for _, want := range []string{"web", "0.0.0.0:22 → 22 / TCP · IPv4 全部地址", "[::]:22 → 22 / TCP · IPv6 全部地址", "127.0.0.1:8080 → 80 / TCP · IPv4 回环地址", "192.168.1.10:5353 → 53 / UDP · IPv4", "host-app", "需查看服务实际监听端口", "不代表外网一定可达"} {
		if !strings.Contains(parts[1], want) {
			t.Fatalf("missing %q in %s", want, parts[1])
		}
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
	fields, err := dockerPortFields([]dockerPortContainer{{Name: "/routed", Ports: map[string][]dockerPortBinding{
		"80/tcp": {{HostIP: "::", HostPort: ""}},
	}}})
	if err != nil || len(fields) != 1 || fields[0].Value != "直接路由（无宿主机映射端口） → 80 / TCP · IPv6 全部地址" {
		t.Fatalf("routed publication misrepresented: %+v %v", fields, err)
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
