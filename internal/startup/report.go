package startup

import (
	"time"

	"snail_tool/internal/status"
	"snail_tool/internal/system"
)

type Step struct {
	Label   string
	Elapsed time.Duration
}

// Report holds the startup detection only and lives for this process.
type Report struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Version    string
	TargetUser string
	UserError  string
	Steps      []Step
	Status     status.Status
}

func New(buildVersion string) *Report {
	return &Report{StartedAt: time.Now(), Version: buildVersion}
}

func (report *Report) Record(label string, detect func()) {
	started := time.Now()
	detect()
	if report.FinishedAt.IsZero() {
		report.Steps = append(report.Steps, Step{Label: label, Elapsed: time.Since(started)})
	}
}

func (report *Report) Complete(account *system.Account, userErr error, detected status.Status) {
	if !report.FinishedAt.IsZero() {
		return
	}
	if account != nil {
		report.TargetUser = account.Name
	}
	if userErr != nil {
		report.UserError = userErr.Error()
	}
	report.Status = detected
	report.FinishedAt = time.Now()
}
