# IP 证书兼容性结论

- Let’s Encrypt 的 IP 地址证书必须使用 `shortlived` profile，当前有效期为 160 小时（约六天）。
- Certbot 的 `--ip-address` 在 5.3 版本才加入；对 IP 使用 webroot 的支持要求 5.4 或更高。
- Certbot 官方说明指出 standalone 插件可用于 IP 证书，但需要临时占用 TCP 80；对现有 KotaUI 安装器应使用 `certonly --standalone --preferred-profile shortlived --ip-address <IP>`。
- Debian/Ubuntu 系统仓库提供的 Certbot 可能显著旧于 5.3，因此安装器必须在 IP 分支显式安装兼容版本并验证 `certbot --version` 与 `certbot --help` 包含所需选项。

来源：
- https://letsencrypt.org/2026/03/11/shorter-certs-certbot
- https://letsencrypt.org/docs/profiles/
