package firewall

import (
	"snail_tool/internal/firewall/loadedrules"
	"snail_tool/internal/ui"
)

func configSyncField(content []byte, fileErr error, installed bool, live loadedrules.Snapshot) ui.CardField {
	return configStore().SyncField(content, fileErr, installed, live.JSON, live.Err, commandExists)
}

func compareConfigRules(content []byte, live loadedrules.Snapshot) (bool, error) {
	return configStore().Compare(content, live.JSON, live.Err, commandExists)
}
