package proxy

import (
	"fmt"
	"os"
	"path/filepath"

	commonproxy "snail_tool/internal/common/proxy"
	"snail_tool/internal/log"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
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
		fmt.Println("当前终端已存在的代理环境变量可能需要重新登录或手动 unset 后才会消失。")
	} else {
		log.Info("未发现代理托管配置，跳过")
	}
	return nil
}
