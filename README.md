# KotaUI

KotaUI 是一个面向 Alpine、Debian 和 Ubuntu VPS 的自托管 sing-box 管理面板。当前仓库处于 **0.1.0 验证原型阶段**，目标是先验证安装器、面板 API、入站管理、客户端规则和订阅输出，再逐步扩展到证书、服务管理和完整 sing-box 配置生成。

## 当前已实现

当前版本包含一个无外部运行时依赖的 Node.js HTTP 服务、基础 Web 管理界面、JSON 状态存储、入站创建和删除、客户端创建、用户名校验、重复用户名保护、客户端订阅输出，以及 `kotaui` 本机命令的 `info`、`start`、`check` 和二次确认卸载入口。

安装脚本会检查 root 权限、CPU 架构和发行版，并在缺少 Node.js 或 Git 时尝试通过 Alpine `apk` 或 Debian/Ubuntu `apt` 安装。生产部署前仍需补充 systemd/OpenRC 服务注册、TLS 证书申请与自动续期、管理员认证、数据库迁移、真实 sing-box 配置生成、流量统计和审计日志。

## 本地运行

```sh
KOTAUI_PORT=1108 npm start
```

浏览器打开 `http://127.0.0.1:1108/`。状态文件默认写入 `data/state.json`，生产环境应将数据目录设置到 `/var/lib/kotaui` 并限制为 root 可读。

## 安装

在源码目录或发布包目录中以 root 执行：

```sh
KOTAUI_SOURCE_DIR="$PWD" sh install.sh
kotaui start
```

当前安装器是验证版本，默认安装到 `/opt/kotaui`，状态目录为 `/var/lib/kotaui`。它不会自动申请证书，也不会把服务注册为系统守护进程。

## 测试

```sh
npm test
node --check server/index.mjs
sh -n install.sh
```

跨发行版测试脚本位于 `test-env/run-install-tests.sh`。在受限沙盒中，Docker bridge 网络受宿主内核 iptables 能力限制，因此当前首先使用无网络 Node.js 容器完成服务烟雾测试，并将 Alpine、Debian、Ubuntu 的依赖安装测试作为下一轮环境回归项。

## 安全边界

不要将当前版本直接暴露到公网。它尚未包含管理员认证、CSRF 防护、速率限制、TLS、密钥轮换和完整的输入审计。任何生产发布都必须在真实 VPS 或专用测试 VM 中完成证书、权限、服务隔离和恢复演练后再决定。
