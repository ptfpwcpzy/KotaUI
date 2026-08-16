# KotaUI

面向 Alpine、Debian、Ubuntu VPS 的轻量级 **Go + sing-box** 管理面板，适合单核、512 MB–1 GB 内存环境。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ptfpwcpzy/KotaUI/main/install.sh)
```

首次 SSH 安装依次选择域名/IP 证书、面板端口（默认 `1989`）、面板路径（默认 `ptf`）以及管理员账号密码。安装结束会显示完整管理地址，请立即保存。

| 能力 | 说明 |
|---|---|
| 实时仪表盘 | CPU、内存、Swap、存储、负载、运行时间、面板/sing-box/证书状态。 |
| 入站 | VLESS + REALITY、Hysteria 2、Shadowsocks 2022；支持创建、编辑、启停和删除。 |
| REALITY | 从候选或自定义伪装域名中选择；自动测试 VPS TLS 延迟，自动生成密钥与 Short ID。 |
| 客户端 | 多入站绑定、总流量、自然月流量、有效期、在线 IP 限制、订阅链接。 |
| 维护 | 自动证书续签、备份、健康检查、更新与一次 `y/N` 确认的干净卸载。 |

安装后输入以下命令打开本机管理菜单：

```bash
kota
```

> 作者那么羡慕你，仅供学习自用，请勿随意传播。
