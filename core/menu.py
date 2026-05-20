def show_menu():
    print("请选择操作：")
    print("1) 配置当前用户 SSH 公钥登录 + 禁用密码登录 + 随机 SSH 端口")
    print("2) 配置当前用户 Vim ~/.vimrc")
    print("3) 配置当前用户 Bash 环境")
    print("4) 配置当前用户 HTTP/HTTPS 代理环境变量")
    print("q) 退出")


def pause():
    input("\n按回车返回菜单...")
