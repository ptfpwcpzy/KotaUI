# KotaUI

轻量级 **Go + sing-box** 管理面板，面向单核、512 MB–1 GB 内存的 VPS。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ptfpwcpzy/KotaUI/main/install.sh)
```

首次 SSH 安装按以下顺序完成：选择域名或 IP 证书、设置面板端口（默认 `1989`）、设置路径（默认 `ptf`）、设置管理员账号和密码。安装结束会显示完整管理地址与初始凭据，请立即保存。

面板支持 **VLESS + REALITY、Hysteria 2、Shadowsocks 2022**，并提供订阅、用户流量/到期控制与证书自动续签。

安装后输入以下命令打开本机管理菜单：

```bash
kota
```

> 作者那么羡慕你，仅供学习自用，请勿随意传播。
