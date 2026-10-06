package startup

import (
	"errors"
	"reflect"
	"testing"

	"snail_tool/internal/status"
	"snail_tool/internal/system"
)

func TestCompletedReportKeepsOriginalStartupSnapshot(t *testing.T) {
	report := New("v1.2.3")
	report.Record("Docker 服务（docker info）", func() {})
	report.Complete(&system.Account{Name: "first-user"}, nil, status.Status{Runtime: "Docker"})
	finishedAt := report.FinishedAt
	steps := append([]Step(nil), report.Steps...)

	called := false
	report.Record("later probe", func() { called = true })
	report.Complete(&system.Account{Name: "other-user"}, errors.New("later failure"), status.Status{Runtime: "未安装"})
	if !called {
		t.Fatal("recording changed whether the supplied probe runs")
	}
	if report.TargetUser != "first-user" || report.UserError != "" || report.Status.Runtime != "Docker" {
		t.Fatalf("startup snapshot was overwritten: %+v", report)
	}
	if report.FinishedAt != finishedAt || !reflect.DeepEqual(report.Steps, steps) {
		t.Fatalf("startup timing was overwritten: %+v", report)
	}
}

func TestReportRetainsUserLookupFailure(t *testing.T) {
	report := New("dev")
	report.Complete(nil, errors.New("getent failed"), status.Status{Runtime: "未安装"})
	if report.TargetUser != "" || report.UserError != "getent failed" {
		t.Fatalf("user lookup failure was lost: %+v", report)
	}
	if report.FinishedAt.Before(report.StartedAt) {
		t.Fatal("startup finished before it started")
	}
}
