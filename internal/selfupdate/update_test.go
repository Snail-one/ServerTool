package selfupdate

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunDownloadsAndExecutesScript(t *testing.T) {
	client := testClient(http.StatusOK, "#!/bin/sh\nprintf 'update-called\\n'\n")

	var output bytes.Buffer
	if err := run(client, "https://example.invalid/install.sh", strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "update-called\n" {
		t.Fatalf("意外的脚本输出：%q", output.String())
	}
}

func TestRunRejectsUnexpectedContent(t *testing.T) {
	client := testClient(http.StatusOK, "not a shell script")

	err := run(client, "https://example.invalid/install.sh", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "不是有效") {
		t.Fatalf("应拒绝意外内容，得到：%v", err)
	}
}

func TestRunReportsHTTPFailure(t *testing.T) {
	client := testClient(http.StatusNotFound, "missing")

	err := run(client, "https://example.invalid/install.sh", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("应返回 HTTP 错误，得到：%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testClient(status int, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
}
