# 测试服务器与自动部署说明

这份文档给后续 Codex/IDE 会话快速接手项目用。开始修改代码前先读这里，避免误停旧服务或把测试服务器配置弄乱。

## 测试服务器

- 主机：`64.83.34.53`
- 系统：Ubuntu
- SSH：`root@64.83.34.53:22`
- GameNet 控制 API：`http://64.83.34.53:8080`
- GameNet Overlay：`64.83.34.53:35768`
- GameNet 远端目录：`/opt/lwyv-gamenet`

重要：服务器上的 `443` 端口被旧的 LwyV-Net 服务占用，进程通常显示为 `lwyvnet-linux-a`。不要停止、重启、覆盖或抢占这个 443 服务。GameNet 测试只使用 `8080` 和 `35768`。

SSH 密码不要写进 README 或代码。新会话需要部署时，先让用户提供密码，或者在本机 PowerShell 临时设置：

```powershell
$env:LWYV_TEST_SSH_PASSWORD = "这里填测试服务器 SSH 密码"
```

可选环境变量：

```powershell
$env:LWYV_TEST_HOST = "64.83.34.53"
$env:LWYV_TEST_SSH_USER = "root"
$env:LWYV_TEST_SSH_PORT = "22"
```

## 远端服务

systemd 服务名：

```bash
systemctl status lwyv-gamenet-control
systemctl status lwyv-gamenet-node
```

远端文件布局：

```text
/opt/lwyv-gamenet/gamenet-control
/opt/lwyv-gamenet/gamenet-node
/opt/lwyv-gamenet/gamenet-smoke
/opt/lwyv-gamenet/configs/control.prod.json
/opt/lwyv-gamenet/configs/node.prod.json
/opt/lwyv-gamenet/deploy/dist/
```

监听端口检查：

```bash
ss -ltnp | grep -E ':(443|8080|35768) '
```

期望结果：

- `8080`：`gamenet-control`
- `35768`：`gamenet-node`
- `443`：旧服务，不属于 GameNet

## 一键部署

本机 PowerShell 中执行：

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
$env:LWYV_TEST_SSH_PASSWORD = "这里填测试服务器 SSH 密码"
.\deploy\deploy-test.ps1
```

脚本会自动执行：

1. `go test -p 1 ./...`
2. 构建 Linux 版 `gamenet-control`、`gamenet-node`、`gamenet-smoke`
3. 上传到 `/opt/lwyv-gamenet/deploy/dist`
4. 安装到 `/opt/lwyv-gamenet`
5. 重启 `lwyv-gamenet-control` 和 `lwyv-gamenet-node`
6. 检查 `8080`、`35768`、`443`
7. 跑远端 smoke 测试

只上传二进制，不覆盖远端配置：

```powershell
.\deploy\deploy-test.ps1
```

如果确实改了 `configs/control.prod.json` 或 `configs/node.prod.json`，需要同步配置：

```powershell
.\deploy\deploy-test.ps1 -UploadConfigs
```

如果同时要重新打 Windows 客户端：

```powershell
.\deploy\deploy-test.ps1 -BuildWindowsUI
```

如果只是快速重启部署，不想跑 smoke：

```powershell
.\deploy\deploy-test.ps1 -SkipSmoke
```

## 手动部署步骤

没有使用脚本时，按下面顺序操作。

本地构建：

```powershell
go test -p 1 ./...

$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -o .\deploy\dist\gamenet-control .\cmd\gamenet-control
go build -o .\deploy\dist\gamenet-node .\cmd\gamenet-node
go build -o .\deploy\dist\gamenet-smoke .\cmd\gamenet-smoke
Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED -ErrorAction SilentlyContinue
```

Windows UI 构建：

```powershell
Push-Location .\cmd\gamenet-windows-ui
wails build
Pop-Location
```

远端安装：

```bash
install -m 0755 /opt/lwyv-gamenet/deploy/dist/gamenet-control /opt/lwyv-gamenet/gamenet-control
install -m 0755 /opt/lwyv-gamenet/deploy/dist/gamenet-node /opt/lwyv-gamenet/gamenet-node
install -m 0755 /opt/lwyv-gamenet/deploy/dist/gamenet-smoke /opt/lwyv-gamenet/gamenet-smoke
chmod 600 /opt/lwyv-gamenet/configs/*.prod.json
systemctl restart lwyv-gamenet-control
systemctl restart lwyv-gamenet-node
```

本地跑远端 smoke：

```powershell
go run .\cmd\gamenet-smoke -api http://64.83.34.53:8080 -overlay 64.83.34.53:35768
```

## 修改代码后的建议流程

普通后端改动：

```powershell
go test -p 1 ./...
go vet -p 1 ./...
.\deploy\deploy-test.ps1
```

Windows 客户端或 Wintun 路由改动：

```powershell
npm.cmd run build --prefix .\cmd\gamenet-windows-ui\frontend
Push-Location .\cmd\gamenet-windows-ui
wails build
Pop-Location
go test -p 1 ./...
```

部署后必须确认：

```bash
systemctl is-active lwyv-gamenet-control
systemctl is-active lwyv-gamenet-node
ss -ltnp | grep -E ':(443|8080|35768) '
journalctl -u lwyv-gamenet-control -n 50 --no-pager
journalctl -u lwyv-gamenet-node -n 50 --no-pager
```

## 当前关键测试点

`cmd/gamenet-smoke` 会覆盖这些核心行为：

- 注册、重复用户名拒绝
- 单账号单点登录，旧 token 失效
- 创建房间、加入房间
- 一人只能处在一个房间
- 房间满员拒绝
- Overlay vDHCP 分配 IP，网关为 `.254`，玩家 IP 从 `.1` 开始
- 房间成员在线/离线状态
- 有限广播、房间子网广播、IPv4 组播转发
- 房主转让、踢人、删除房间
- 不启动 Overlay 时，房主踢人后房间仍只保留房主，不会被误清理
- 被踢玩家收到 SSE 房间事件，房间节点主动断开该玩家 Overlay 连接
- 删除房间后公钥失效
- 空房自动清理

## 敏感配置注意

这些文件包含真实密钥或密码，不要公开贴出完整内容：

- `configs/control.prod.json`
- `configs/node.prod.json`
- `deploy/.env`
- 服务器 SSH 密码

如果下一次会话需要查看配置，只总结字段含义和必要差异，不要把 `tokenSecret`、`adminToken`、数据库密码、节点私钥完整输出到聊天里。
