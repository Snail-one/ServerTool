package status

import "snail_tool/internal/ssh/security"

func Show() error {
	return security.ShowStatus()
}
