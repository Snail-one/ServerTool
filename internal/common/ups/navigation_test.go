package ups

import (
	"os"
	"snail_tool/internal/ui"
	"testing"
)

func TestUPSDeviceBackReturnsToPreviousField(t *testing.T) {
	view := navigationUI(t, "0463\nffff\nq\nq\n1234\nabcd\n测试 UPS\nparent-next\n")
	device, err := askUPSDeviceConfig(view)
	if err != nil {
		t.Fatal(err)
	}
	if device.VendorID != "1234" || device.ProductID != "abcd" || device.Desc != "测试 UPS" {
		t.Fatalf("back did not revisit device fields: %+v", device)
	}
	if next, err := view.Ask("parent"); err != nil || next != "parent-next" {
		t.Fatalf("next=%q err=%v", next, err)
	}
}

func navigationUI(t *testing.T, input string) *ui.UI {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = f
	view := ui.New()
	os.Stdin = previous
	return view
}
