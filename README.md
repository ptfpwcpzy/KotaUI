# KotaUI

KotaUI 是一个面向 Alpine、Debian 和 Ubuntu VPS 的自托管 **sing-box 节点管理面板**。它不是独立代理核心；Reality、Hysteria 2 和 Shadowsocks 2022 都由同一个 sing-box 进程提供，KotaUI 负责 Web 管理、客户端、订阅、证书、流量、在线 IP、服务和更新。

## VPS 一键安装

在全新的 VPS 上以 root 身份执行以下命令即可安装试验版：

```sh
curl -fsSL https://raw.githubusercontent.com/ptfpwcpzy/KotaUI/main/install.sh | sh
```

安装器会识别 Alpine、Debian 或 Ubuntu，安装 Node.js/npm、Git、OpenSSL、Certbot 和编译所需工具，构建带 `with_v2ray_api`、`with_utls`、`with_quic` 的 sing-box 版本，复制 KotaUI，生成管理员账号和随机密码，注册 systemd/OpenRC 服务，启动面板，并输出登录地址和凭据。安装完成后使用 `kota` 命令进行本机运维。

如果希望固定管理员账号和密码，可以在安装前设置环境变量：

```sh
curl -fsSL https://raw.githubusercontent.com/ptfpwcpzy/KotaUI/main/install.sh | \
  env KOTAUI_ADMIN_USER=admin KOTAUI_ADMIN_PASSWORD='change-this-password-123' sh
```

如果希望安装时申请证书，可以使用域名或 IP。证书申请需要目标域名/IP 指向当前 VPS；standalone 校验需要临时使用 TCP 80。IP 证书会使用 `shortlived` profile，寿命可能只有数天，安装器和 UI 会明确提示并配置高频续期：

```sh
curl -fsSL https://raw.githubusercontent.com/ptfpwcpzy/KotaUI/main/install.sh | \
  env KOTAUI_ISSUE_CERT=1 KOTAUI_CERT_DOMAIN=example.com KOTAUI_CERT_EMAIL=admin@example.com sh
```

默认服务端口为 TCP `1108`，订阅端口为 TCP `1109`，统计 API 只监听 `127.0.0.1:9090`。如果明确希望安装器管理 UFW，可以设置 `KOTAUI_CONFIGURE_FIREWALL=1`；这样只会开放面板、订阅和实际创建的节点端口，统计 API不会开放公网。未显式启用时，安装器不会覆盖现有防火墙规则。

## 管理功能

Web 面板提供管理员登录、入站管理、客户端管理、订阅输出、证书申请和续期、流量同步、自然月流量、总流量、有效期、跨入站活跃 IP 限制，以及 KotaUI 与 sing-box 的更新入口。更新前备份，更新时执行 SHA-256 校验和原子替换，更新后执行配置校验和健康检查，失败时恢复文件与状态。

首版入站协议为 Reality、Hysteria 2 和 Shadowsocks 2022。Hysteria 2 和 Shadowsocks 2022 不是独立核心，不会安装或运行单独的 Hysteria 2 服务。

## 本机运维

安装后执行：

```sh
kota info
kota check
kota restart
kota logs
```

`kota` 需要 root 身份运行。管理员凭据保存在 `/var/lib/kotaui/admin.env`，权限为 `0600`；systemd/OpenRC 服务只引用该文件，不把密码写入服务模板。

## 支持系统与测试

目标系统为 Alpine、Debian 和 Ubuntu，支持 `x86_64` 和 `aarch64`。本地验证包括 10 项 Node.js 自动化测试、真实 sing-box 三协议配置校验、带 V2Ray API 的真实统计接口、Alpine musl 构建、Alpine/Ubuntu 一键安装功能回归、systemd/OpenRC 模板语法和更新失败回滚。

真实 CA 签发、公网 UDP/QUIC 客户端连接和正式续期需要用户提供测试域名或临时 VPS，不能在离线或无公网入口的沙盒中伪造为已完成。

## 参考资料

[1]: https://sing-box.sagernet.org/configuration/inbound/hysteria2/ "sing-box Hysteria 2 入站配置"
[2]: https://sing-box.sagernet.org/configuration/inbound/shadowsocks/ "sing-box Shadowsocks 入站配置"
[3]: https://sing-box.sagernet.org/configuration/experimental/v2ray-api/ "sing-box V2Ray API"
[4]: https://eff-certbot.readthedocs.io/en/stable/using.html "Certbot User Guide"
