package vim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonvim "snail_tool/internal/common/vim"
	"snail_tool/internal/shared"
	"snail_tool/internal/system"
)

func TestRunRemovesManagedBlockOnly(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	vimrc := filepath.Join(home, ".vimrc")
	begin, end := commonvim.VimMarkers()
	manual := "set number\ncolorscheme desert\n"
	content := manual + "\n" + shared.FormatManagedBlock(begin, commonvim.ManagedVimConfigContent(), end)

	if err := os.WriteFile(vimrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, vimrc)
	if got != manual {
		t.Fatalf("manual Vim configuration was not preserved:\ngot:\n%s\nwant:\n%s", got, manual)
	}
	if strings.Contains(got, begin) || strings.Contains(got, end) {
		t.Fatalf("managed Vim markers remained:\n%s", got)
	}
}

func TestRunClearsLegacyManagedTemplateAndPreservesManualFile(t *testing.T) {
	home := t.TempDir()
	account := &system.Account{Name: "test", Home: home}
	vimrc := filepath.Join(home, ".vimrc")

	legacy := `" --- 第一步：加载官方默认设置 ---
if !exists('g:skip_defaults_vim')
  source $VIMRUNTIME/defaults.vim
endif

" --- 第二步：写你自己的『覆盖』命令 ---
syntax on
filetype plugin indent on
set mouse=
set pastetoggle=<F2>
nnoremap <F3> :set number!<CR>
`
	if err := os.WriteFile(vimrc, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, vimrc); got != "" {
		t.Fatalf("expected legacy managed vimrc to be kept empty, got:\n%s", got)
	}

	if err := os.WriteFile(vimrc, []byte("set number\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run(account); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, vimrc); got != "set number\n" {
		t.Fatalf("modified vimrc should be preserved, got:\n%s", got)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
