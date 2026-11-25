package docker

import "testing"

func TestStatusFieldsIncludeLogAndProxy(t *testing.T) {
	fields := statusFields(
		"Docker",
		"json-file，100m × 3",
		"http://admin:******@127.0.0.1:7890/",
		"HTTP http://192.168.1.100:7890",
	)
	if len(fields) != 4 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].Label != "当前运行时" || fields[0].Value != "Docker" {
		t.Fatalf("runtime field = %#v", fields[0])
	}
	if fields[1].Label != "容器日志" || fields[1].Value != "json-file，100m × 3" {
		t.Fatalf("log field = %#v", fields[1])
	}
	if fields[2].Label != "服务代理" || fields[2].Value != "http://admin:******@127.0.0.1:7890/" {
		t.Fatalf("proxy field = %#v", fields[2])
	}
	if fields[3].Label != "构建与容器代理" || fields[3].Value != "HTTP http://192.168.1.100:7890" {
		t.Fatalf("client proxy field = %#v", fields[3])
	}
}

func TestUninstallLabelKeepsDockerServiceDetail(t *testing.T) {
	if got := uninstallLabel("Docker"); got != "Docker 卸载" {
		t.Fatalf("docker label = %q", got)
	}
	if got := uninstallLabel("Docker（服务异常：daemon 不可达）"); got != "Docker 卸载（服务异常：daemon 不可达）" {
		t.Fatalf("abnormal docker label = %q", got)
	}
}
