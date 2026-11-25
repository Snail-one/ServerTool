package configuration

import (
	"fmt"
	"os"

	"snail_tool/internal/log"
	"snail_tool/internal/ui"
)

func (store Store) Show() error {
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "配置文件")
	data, err := os.ReadFile(store.Path)
	if os.IsNotExist(err) {
		ui.PrintInfoCard("尚未生成防火墙配置", ui.CardField{Label: "下一步", Value: "选择 d 生成默认配置"})
		return nil
	}
	if err != nil {
		return err
	}
	ui.PrintField("配置文件", store.Path)
	fmt.Println(string(data))
	return nil
}

func (store Store) Reload(view *ui.UI, isRoot func() bool, commandExists func(string) bool) error {
	if !isRoot() {
		return fmt.Errorf("加载防火墙配置需要 root 权限，请使用 sudo 运行本工具")
	}
	ui.ClearScreen()
	ui.MenuTitle("防火墙管理", "校验加载配置")
	info, err := os.Lstat(store.Path)
	if os.IsNotExist(err) {
		return fmt.Errorf("未找到 %s，请先生成或编辑配置", store.Path)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("拒绝加载非普通文件或软链接: %s", store.Path)
	}
	content, err := os.ReadFile(store.Path)
	if err != nil {
		return err
	}
	version := Version{Content: content, Exists: true}
	if err := store.Validate(version); err != nil {
		return err
	}
	equal, compareErr := store.compareLoaded(content, commandExists)
	if err := store.Check(version); err != nil {
		return err
	}
	if compareErr == nil && equal {
		ui.PrintSuccessCard("配置已同步，无需加载",
			ui.CardField{Label: "校验结果", Value: "通过"},
			ui.CardField{Label: "配置对比", Value: "当前文件与内核规则一致"},
		)
		return nil
	}
	fields := []ui.CardField{
		{Label: "配置文件", Value: store.Path},
		{Label: "校验结果", Value: "通过"},
	}
	if compareErr != nil {
		fields = append(fields, ui.CardField{Label: "配置对比", Value: "无法比较", Detail: compareErr.Error()})
	} else {
		fields = append(fields, ui.CardField{Label: "配置对比", Value: "存在差异，需要加载才能同步"})
	}
	fields = append(fields,
		ui.CardField{Label: "生效范围", Value: "立即执行文件中的规则，影响新建网络连接"},
	)
	ui.PrintInfoCard("准备加载防火墙配置", fields...)
	confirmed, err := view.Confirm("是否立即加载此防火墙配置？(y/N，0/q 返回)：")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("配置文件已保留，未加载；可稍后输入 r 校验加载配置")
		return ui.ErrReturnToMenu
	}
	return store.loadAndShow(content)
}

func (store Store) loadAndShow(content []byte) error {
	if err := store.Apply(content); err != nil {
		return err
	}
	ui.PrintSuccessCard("防火墙配置已加载",
		ui.CardField{Label: "配置文件", Value: store.Path},
		ui.CardField{Label: "当前状态", Value: "已立即生效"},
		ui.CardField{Label: "查看规则", Value: "选择 4 查看端口策略，选择 5 查看全部规则"},
	)
	return nil
}
