# KotaUI

KotaUI 是一个面向 Alpine、Debian 和 Ubuntu VPS 的自托管节点管理面板，使用中文单一浅色界面。当前仓库处于 **0.1.0 内部验证阶段**，尚未推送或发布。

## 已确定的首版范围

首版只围绕三类入站配置：**Reality（内部按 VLESS + Reality 结构生成）、Hysteria 2 和 Shadowsocks 2022**。节点端口使用 1024–65535 的随机高位端口；客户端用户名全局唯一，长度 3–32 位，仅允许英文字母、数字、下划线和连字符。

安装后统一使用 `kota` 唤醒本机菜单。菜单负责面板信息、证书状态、服务管理、升级和卸载。Web 设置页固定为中文浅色界面，并提供更新入口；更新流程以面板、sing-box 和 Hysteria 2 为整体，先备份，再更新，失败时回滚。

证书流程允许输入域名或 IP。输入 IP 时界面和安装流程必须提示：IP 证书的有效期可能明显短于域名证书，需要关注到期时间并及时续订。

## 当前已实现的验证功能

当前版本包含 Node.js HTTP 服务、健康检查、状态查询、入站创建和删除、客户端创建、用户名校验、订阅输出、Reality/Hysteria 2/Shadowsocks 2022 入站字段校验、IP 证书提示、更新事务备份、更新成功状态和模拟失败回滚，以及 `kota` 安装命令生成。

官方 sing-box 文档显示，Hysteria 2 入站需要 TLS，并支持带宽、obfs、用户、masquerade 等字段；Shadowsocks 2022 需要按加密方法生成对应长度的 Base64 密钥；Reality 是 TLS 的自定义支持，需要握手目标、私钥和 short ID 等字段。因此 KotaUI 已将这些字段拆分到协议表单中，并在服务端执行基础校验。[1] [2] [3]

## 本地测试

```sh
npm test
node --check server/index.mjs
sh -n install.sh
```

当前自动化测试覆盖健康检查、IP 证书提示、三种协议入站、客户端用户名规则、订阅接口、UI 全量更新状态和失败回滚。安装器还经过临时目录测试，确认生成的是 `kota` 而不是旧的 `kotaui` 命令。

## 当前限制

当前“更新”实现已经验证了**备份—更新—失败回滚**的事务逻辑，但还没有接入固定版本发布源并下载真实的 KotaUI、sing-box 和 Hysteria 2 二进制。正式生产版还需要补充签名校验、版本清单、下载超时、校验和验证、systemd/OpenRC 服务切换和真实进程健康检查。

同样，当前版本还不是生产级面板。管理员认证、TLS 实际申请与续期、真实 sing-box 配置写入、流量统计、在线 IP 限制、数据库迁移和跨发行版完整 VPS 回归仍需继续完成。不要将当前版本直接暴露到公网。

## 参考资料

[1]: https://sing-box.sagernet.org/configuration/inbound/hysteria2/ "sing-box Hysteria2 入站配置"
[2]: https://sing-box.sagernet.org/configuration/inbound/shadowsocks/ "sing-box Shadowsocks 入站配置"
[3]: https://sing-box.sagernet.org/configuration/shared/tls/ "sing-box TLS 与 Reality 配置"
