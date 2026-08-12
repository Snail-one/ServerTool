package proxy

import (
	"os"
	"path/filepath"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
	"snail_tool/internal/ui"
)

var proxyBegin, proxyEnd = commonproxy.ProxyMarkers()

func Run(account *system.Account) error {
	bashrc := filepath.Join(account.Home, ".bashrc")
	changed, err := shared.CleanupManagedBlocks(
		bashrc,
		shared.BlockMarker{Begin: proxyBegin, End: proxyEnd},
	)
	if err != nil {
		return err
	}
	for _, name := range commonproxy.ProxyCleanupEnvNames() {
		_ = os.Unsetenv(name)
	}
	if changed {
		log.Info("已清理代理托管配置：", bashrc)
		ui.PrintWarningCard("代理清理提示",
			ui.CardField{Label: "配置文件", Value: bashrc},
			ui.CardField{Label: "当前终端", Value: "已有代理变量可能仍然存在"},
			ui.CardField{Label: "生效方式", Value: "重新登录或手动 unset"},
		)
	} else {
		log.Info("未发现代理托管配置，跳过")
	}
	return nil
}
