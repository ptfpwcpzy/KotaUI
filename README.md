# KotaUI

KotaUI 是一个面向 VPS 的轻量级 **sing-box Web 管理面板**。项目只使用一个 sing-box 核心，统一管理 Reality、Hysteria 2 和 Shadowsocks 2022 入站。

## 项目特色

- 支持 Reality、Hysteria 2、Shadowsocks 2022。
- 提供管理员登录、客户端管理、订阅生成和配置校验。
- 提供流量统计、自然月流量、总流量、有效期和在线 IP 限制。
- 支持域名证书和 IP 证书申请，并自动维护证书。
- IP 证书有效期较短，面板和安装向导会明确提示，并使用高频续期检查。
- 面板和 sing-box 核心支持备份、校验、原子更新和失败回滚。
- 支持 Debian、Ubuntu、Alpine，以及 systemd 和 OpenRC。
- 管理员凭据保存于 root-only 文件，统计 API 默认只监听本机回环地址。

## 一键安装

在全新的 VPS 上以 root 身份执行：

```sh
curl -fsSL https://raw.githubusercontent.com/ptfpwcpzy/KotaUItest/main/install.sh | sh
```

安装器会显示欢迎页。按回车后，选择使用域名或 IP 申请证书；默认选择域名。输入域名或公网 IP 后，安装器会自动申请证书、配置证书自动维护、安装 KotaUI 和 sing-box，并注册系统服务。

证书申请使用 TCP 80 进行临时校验，因此申请期间需要确保 TCP 80 可访问。IP 证书的有效期较短，安装器会在申请前显示提示，并配置自动续期检查。

## 安装后

安装完成后可使用以下命令进行基本运维：

```sh
kota info
kota check
kota restart
kota logs
```

默认面板端口为 TCP `1108`，订阅端口为 TCP `1109`，统计 API 为本机 `127.0.0.1:9090`。

## 支持环境

支持 Alpine、Debian 和 Ubuntu，支持 `x86_64` 与 `aarch64`。项目核心为 sing-box，KotaUI 负责配置、证书、统计、订阅、服务和更新管理。

## 许可证

本项目当前为实验性项目，代码和安装脚本位于 [KotaUItest](https://github.com/ptfpwcpzy/KotaUItest)。
