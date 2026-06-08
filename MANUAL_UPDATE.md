# LwyLink 手动更新 Runbook

这份文档用于不走一键脚本时，手动完成验证、构建、上传、发布客户端、扩容和回滚。日常优先使用：

```powershell
.\deploy\release.cmd -Version 0.1.12 -Notes "本次更新说明"
```

只需要构建和部署时：

```powershell
.\deploy\build-all.ps1 -Vet
.\deploy\upload-deploy.ps1
```

完整生产部署说明见 [deploy/README.md](deploy/README.md)，测试服务器信息见 [deploy/TEST_SERVER.md](deploy/TEST_SERVER.md)。

## 1. 更新前检查

确认工作区和工具链：

```powershell
git status --short --branch
git pull --ff-only

go version
npm.cmd --version
wails version
```

如果要发布新的 Windows 客户端版本，建议同步这些位置：

| 文件 | 内容 |
| --- | --- |
| `cmd/lwylink-windows-ui/app.go` | `AppVersion` |
| `cmd/lwylink-windows-ui/wails.json` | `info.productVersion` |
| `cmd/lwylink-windows-ui/frontend/package.json` | `version` |
| `cmd/lwylink-windows-ui/frontend/package-lock.json` | 顶层版本 |
| `cmd/lwylink-windows-ui/frontend/src/types.ts` | 默认 `currentVersion` |
| `internal/web/admin_assets/admin.html` | 管理端版本占位示例 |

检查版本残留示例：

```powershell
rg -n "0\.1\.11|0\.1\.12" cmd\lwylink-windows-ui internal\web -S
```

## 2. 本地验证

后端：

```powershell
go test -p 1 ./...
go vet -p 1 ./...
```

如果本机装了 `staticcheck`：

```powershell
staticcheck ./...
```

前端和 Wails 客户端：

```powershell
npm.cmd run build --prefix .\cmd\lwylink-windows-ui\frontend

Push-Location .\cmd\lwylink-windows-ui
wails build
Pop-Location
```

Wails 默认产物：

```text
cmd/lwylink-windows-ui/build/bin/LwyLink.exe
```

## 3. 编译服务端

```powershell
New-Item -ItemType Directory -Force -Path .\deploy\dist | Out-Null

$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

try {
  go build -trimpath -o .\deploy\dist\lwylink-control .\cmd\lwylink-control
  go build -trimpath -o .\deploy\dist\lwylink-node .\cmd\lwylink-node
  go build -trimpath -o .\deploy\dist\lwylink-smoke .\cmd\lwylink-smoke
} finally {
  Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}
```

可选：单独编译 Windows 虚拟组网引擎：

```powershell
New-Item -ItemType Directory -Force -Path .\build | Out-Null
go build -trimpath -o .\build\lwylink-net.exe .\cmd\lwylink-net
```

## 4. 上传并重启主服务器

测试服务器：

```text
SSH: root@64.83.34.53:22
远端目录: /opt/lwylink
控制 API: http://64.83.34.53:8080
Overlay: 64.83.34.53:35768
```

注意：测试服 `443` 是旧服务，不属于 LwyLink。不要停止、覆盖或抢占它。

创建远端目录：

```powershell
ssh root@64.83.34.53 "mkdir -p /opt/lwylink/deploy/dist /opt/lwylink/configs"
```

上传二进制和 systemd 文件：

```powershell
scp .\deploy\dist\lwylink-control root@64.83.34.53:/opt/lwylink/deploy/dist/
scp .\deploy\dist\lwylink-node root@64.83.34.53:/opt/lwylink/deploy/dist/
scp .\deploy\dist\lwylink-smoke root@64.83.34.53:/opt/lwylink/deploy/dist/
scp .\deploy\lwylink-control.service root@64.83.34.53:/opt/lwylink/deploy/
scp .\deploy\lwylink-node.service root@64.83.34.53:/opt/lwylink/deploy/
```

只有配置确实变更时才上传：

```powershell
scp .\configs\control.prod.json root@64.83.34.53:/opt/lwylink/configs/
scp .\configs\node.prod.json root@64.83.34.53:/opt/lwylink/configs/
```

远端安装并重启：

```powershell
ssh root@64.83.34.53 @'
set -e
install -m 0755 /opt/lwylink/deploy/dist/lwylink-control /opt/lwylink/lwylink-control
install -m 0755 /opt/lwylink/deploy/dist/lwylink-node /opt/lwylink/lwylink-node
install -m 0755 /opt/lwylink/deploy/dist/lwylink-smoke /opt/lwylink/lwylink-smoke
install -m 0644 /opt/lwylink/deploy/lwylink-control.service /etc/systemd/system/lwylink-control.service
install -m 0644 /opt/lwylink/deploy/lwylink-node.service /etc/systemd/system/lwylink-node.service
chmod 600 /opt/lwylink/configs/*.prod.json 2>/dev/null || true
systemctl daemon-reload
systemctl enable --now lwylink-control
systemctl enable --now lwylink-node
systemctl restart lwylink-control
systemctl restart lwylink-node
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(443|8080|35768) ' || true
'@
```

## 5. Smoke 验证

```powershell
$adminToken = ((Get-Content .\configs\control.prod.json -Raw) | ConvertFrom-Json).adminToken
go run .\cmd\lwylink-smoke -api http://64.83.34.53:8080 -overlay 64.83.34.53:35768 -admin-token $adminToken
```

期望最后出现：

```text
SMOKE OK
```

查看远端状态和日志：

```powershell
ssh root@64.83.34.53 @'
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(443|8080|35768) ' || true
journalctl -u lwylink-control -n 50 --no-pager
journalctl -u lwylink-node -n 50 --no-pager
'@
```

## 6. 发布 Windows 客户端

推荐在管理后台发布：

1. 打开 `http://64.83.34.53:8080/admin?token=<ADMIN_TOKEN>`。
2. 进入“更新发布”。
3. 版本号填写当前版本，例如 `0.1.12`。
4. 平台填写 `windows-amd64`。
5. 上传 `cmd/lwylink-windows-ui/build/bin/LwyLink.exe` 或 `build/windows-amd64/LwyLink.exe`。
6. 强制更新选择“是”。当前阶段经常是不兼容更新，不要发布成可选更新。
7. 裸 `LwyLink.exe` 的安装参数留空。
8. 更新说明按实际填写。

发布后校验：

```powershell
$latest = Invoke-RestMethod -Uri "http://64.83.34.53:8080/api/client/releases/latest?platform=windows-amd64"
$localHash = (Get-FileHash -Algorithm SHA256 .\build\windows-amd64\LwyLink.exe).Hash.ToLowerInvariant()

$latest | Select-Object version,platform,fileName,sizeBytes,sha256,downloadURL,createdAt
"Hash matches: $($latest.sha256 -eq $localHash)"
```

## 7. 新增房间节点

新节点必须能访问控制中心使用的 MySQL 和 Redis。生产环境建议通过内网或专用网络访问数据库，并用防火墙限制来源。

准备目录：

```bash
mkdir -p /opt/lwylink/configs /opt/lwylink/deploy/dist
```

上传文件：

```powershell
scp .\deploy\dist\lwylink-node root@<NODE_IP>:/opt/lwylink/lwylink-node
scp .\deploy\lwylink-node.service root@<NODE_IP>:/etc/systemd/system/lwylink-node.service
scp .\configs\node.prod.example.json root@<NODE_IP>:/opt/lwylink/configs/node.prod.json
```

关键配置：

| 字段 | 说明 |
| --- | --- |
| `overlayAddr` | 节点监听地址，例如 `0.0.0.0:35768` |
| `mysqlDsn` | 指向控制中心 MySQL |
| `redisAddr` / `redisPassword` | 指向控制中心 Redis |
| `tokenSecret` | 必须复制控制中心 `configs/control.prod.json` 中的值 |
| `overlayPrivateKey` | 新节点可以留空，首次生成公钥时会自动写回 |

生成节点公钥：

```bash
chmod +x /opt/lwylink/lwylink-node
/opt/lwylink/lwylink-node -config /opt/lwylink/configs/node.prod.json -print-public-key
```

在管理后台添加节点：

| 字段 | 示例 |
| --- | --- |
| 名称 | `node-2` |
| 公网 Overlay 地址 | `<NODE_PUBLIC_IP>:35768` |
| 健康检查地址 | 控制中心能访问的地址，可以是内网地址 |
| Overlay 公钥 | 上一步输出 |
| 最大房间数 | 例如 `100` |
| 状态 | 启用 |

启动：

```bash
chmod 600 /opt/lwylink/configs/node.prod.json
systemctl daemon-reload
systemctl enable --now lwylink-node
systemctl restart lwylink-node
systemctl status lwylink-node --no-pager
```

后台点“检查”，状态为 `online` 后，新房间会自动调度到该节点。

## 8. 同机多节点

同一台机器跑多个节点时，每个节点必须使用独立目录、独立配置、独立端口和独立 `overlayPrivateKey`：

```text
/opt/lwylink-node-1/configs/node.prod.json -> 0.0.0.0:35768
/opt/lwylink-node-2/configs/node.prod.json -> 0.0.0.0:35769
```

每个节点都要分别打印 `overlayPublicKey` 并在后台登记。防火墙也要开放对应的 TCP/UDP 端口。

## 9. 回滚

服务端回滚：

1. 上传上一版 `lwylink-control`、`lwylink-node` 和必要的 `lwylink-smoke`。
2. 远端重新 `install` 覆盖二进制。
3. `systemctl restart lwylink-control lwylink-node`。
4. 查看日志并重新跑 smoke。

客户端回滚：

1. 管理后台重新上传上一版或修复版客户端。
2. 版本号必须高于客户端当前版本，才会触发自动更新。

## 10. 端口速查

控制服务器：

| 端口 | 说明 |
| --- | --- |
| `8080/tcp` | HTTP API 和管理后台 |
| MySQL / Redis | 只允许控制服务和受信节点访问 |

房间节点：

| 端口 | 说明 |
| --- | --- |
| `35768/tcp` | Overlay TCP 控制/中继 |
| `35768/udp` | UDP 打洞协商 |

同机多节点按实际端口追加，例如 `35769/tcp` 和 `35769/udp`。
