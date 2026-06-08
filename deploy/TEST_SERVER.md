# 测试服务器速查

这份文档用于后续 IDE/Codex 会话快速接手测试环境。开始部署前先看这里，重点是不要把敏感配置写进文档或聊天。

## 服务器信息

| 项目 | 值 |
| --- | --- |
| 主机 | `64.83.34.53` |
| 系统 | Ubuntu |
| SSH | `root@64.83.34.53:22` |
| 官网 / 控制 API | `https://www.lwylink.xyz` |
| 控制 API 内网监听 | `http://64.83.34.53:8080` |
| Overlay | `64.83.34.53:35768` |
| 远端目录 | `/opt/lwylink` |

`443/tcp` 现在归 LwyLink 官网 HTTPS 反向代理使用，反代到本机 `8080`。旧的 LwyV-Net 443 服务已不再作为测试服务器入口。

SSH 密码不要写进 README、代码或提交记录。部署时临时设置：

```powershell
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填测试服务器 SSH 密码"
```

可选环境变量：

```powershell
$env:LWYLINK_DEPLOY_HOST = "64.83.34.53"
$env:LWYLINK_DEPLOY_SSH_USER = "root"
$env:LWYLINK_DEPLOY_SSH_PORT = "22"
$env:LWYLINK_ADMIN_TOKEN = "这里填控制端 adminToken"
```

## 远端服务

systemd 服务：

```bash
systemctl status lwylink-control
systemctl status lwylink-node
```

远端文件布局：

```text
/opt/lwylink/lwylink-control
/opt/lwylink/lwylink-node
/opt/lwylink/lwylink-smoke
/opt/lwylink/configs/control.prod.json
/opt/lwylink/configs/node.prod.json
/opt/lwylink/deploy/dist/
```

端口检查：

```bash
ss -ltnup | grep -E ':(443|8080|35768) '
```

期望结果：

| 端口 | 期望归属 |
| --- | --- |
| `8080/tcp` | `lwylink-control` |
| `35768/tcp` | `lwylink-node` 控制/中继 |
| `35768/udp` | `lwylink-node` UDP 打洞协商 |
| `443/tcp` | HTTPS 反向代理，域名 `www.lwylink.xyz` / `lwylink.xyz` |

## 一键发布

推荐路径：

```powershell
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填测试服务器 SSH 密码"
.\deploy\release.cmd -Version <version> -Notes "本次更新说明"
```

脚本会执行：

1. 同步版本号到 Go、Wails、前端包文件、客户端默认状态和管理端占位文本。
2. 运行 `go test -p 1 ./...` 和 `go vet -p 1 ./...`。
3. 构建 Linux 服务端、Linux smoke 工具、Windows 虚拟组网引擎和 Wails UI。
4. 上传服务端二进制到 `/opt/lwylink/deploy/dist`。
5. 安装到 `/opt/lwylink`。
6. 重启 `lwylink-control` 和 `lwylink-node`。
7. 检查 `443/https`、`8080`、`35768/tcp`、`35768/udp`。
8. 跑远端 smoke 测试。
9. 上传 Windows 客户端更新包并校验 latest 版本和 SHA256。

只上传已有构建产物，不覆盖远端配置：

```powershell
.\deploy\upload-deploy.ps1
```

确实改了生产配置时才同步配置：

```powershell
.\deploy\upload-deploy.ps1 -UploadConfigs
```

快速重启部署，不跑 smoke：

```powershell
.\deploy\upload-deploy.ps1 -SkipSmoke
```

## 手动验证

本地跑远端 smoke：

```powershell
$env:LWYLINK_ADMIN_TOKEN = "这里填控制端 adminToken"
go run .\cmd\lwylink-smoke -api https://www.lwylink.xyz -overlay 64.83.34.53:35768
```

期望最后出现：

```text
SMOKE OK
```

查看远端状态和日志：

```bash
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(443|8080|35768) ' || true
journalctl -u lwylink-control -n 50 --no-pager
journalctl -u lwylink-node -n 50 --no-pager
```

## 修改后的建议流程

普通后端改动：

```powershell
go test -p 1 ./...
go vet -p 1 ./...
.\deploy\release.cmd -Version <version> -Notes "本次更新说明"
```

Windows 客户端、Wails 或 Wintun 路由改动：

```powershell
npm.cmd run build --prefix .\cmd\lwylink-windows-ui\frontend
Push-Location .\cmd\lwylink-windows-ui
wails build
Pop-Location
go test -p 1 ./...
```

部署后必须确认：

```bash
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(443|8080|35768) '
journalctl -u lwylink-control -n 50 --no-pager
journalctl -u lwylink-node -n 50 --no-pager
```

## Smoke 覆盖点

`cmd/lwylink-smoke` 覆盖这些核心行为：

- 注册、重复邮箱拒绝。
- 单账号单点登录，旧 token 失效。
- 创建房间、加入房间。
- 一人只能处在一个房间。
- 房间满员拒绝。
- Overlay vDHCP 分配 IP，网关为 `.254`，玩家 IP 从 `.1` 开始。
- 房间成员在线/离线状态。
- 有限广播、房间子网广播、IPv4 组播转发。
- 房主转让、踢人、删除房间。
- 不启动 Overlay 时，房主踢人后房间仍只保留房主，不会被误清理。
- 被踢玩家收到 SSE 房间事件，房间节点主动断开该玩家 Overlay 连接。
- 删除房间后公钥失效。
- 空房自动清理。

## 敏感配置

这些文件包含真实密钥或密码，不要公开贴出完整内容：

- `configs/control.prod.json`
- `configs/node.prod.json`
- `deploy/.env`
- 服务器 SSH 密码

如果需要对比配置，只总结字段含义和必要差异，不要输出 `tokenSecret`、`adminToken`、数据库密码或节点私钥的完整值。
