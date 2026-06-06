# LwyLink 手动更新与多节点部署流程

这份文档用于不走一键脚本时，手动完成版本更新、客户端发布、服务端部署和多节点扩容。命令默认在项目根目录执行。

## 1. 更新前检查

确认本地代码状态：

```powershell
git status --short --branch
git pull --ff-only
```

确认 Go、Node、Wails 可用：

```powershell
go version
npm.cmd --version
wails version
```

如果本次要改客户端版本号，例如 `0.1.8`，至少同步这些位置：

- `cmd/lwylink-windows-ui/app.go`：`AppVersion`
- `cmd/lwylink-windows-ui/wails.json`：`info.productVersion`
- `cmd/lwylink-windows-ui/frontend/package.json`：`version`
- `cmd/lwylink-windows-ui/frontend/package-lock.json`：顶层版本
- `cmd/lwylink-windows-ui/frontend/src/types.ts`：默认 `currentVersion`

检查旧版本是否残留：

```powershell
rg -n "0\.1\.7" cmd\lwylink-windows-ui -S
rg -n "0\.1\.8" cmd\lwylink-windows-ui -S
```

## 2. 本地验证

先跑后端测试和静态检查：

```powershell
go test -p 1 ./...
go vet -p 1 ./...
staticcheck ./...
```

前端生产构建：

```powershell
npm.cmd run build --prefix .\cmd\lwylink-windows-ui\frontend
```

Windows 客户端打包：

```powershell
Push-Location .\cmd\lwylink-windows-ui
wails build
Pop-Location
```

产物位置：

```text
cmd/lwylink-windows-ui/build/bin/LwyLink.exe
```

计算客户端 SHA256，发布后用来比对：

```powershell
Get-FileHash -Algorithm SHA256 .\cmd\lwylink-windows-ui\build\bin\LwyLink.exe
```

## 3. 编译 Linux 服务端

当前线上 Linux 服务端包含三个二进制：

- `lwylink-control`：控制平面、HTTP API、管理后台
- `lwylink-node`：房间 Overlay 节点
- `lwylink-smoke`：远端功能烟测工具

本地交叉编译：

```powershell
New-Item -ItemType Directory -Force -Path .\deploy\dist | Out-Null

$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

try {
  go build -o .\deploy\dist\lwylink-control .\cmd\lwylink-control
  go build -o .\deploy\dist\lwylink-node .\cmd\lwylink-node
  go build -o .\deploy\dist\lwylink-smoke .\cmd\lwylink-smoke
} finally {
  Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}
```

## 4. 上传并更新主服务器

下面用 `ssh/scp` 表示手动流程。服务器示例：

```text
SSH: root@64.83.34.53:22
远端目录: /opt/lwylink
控制 API: http://64.83.34.53:8080
Overlay: 64.83.34.53:35768
```

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

只有在配置确实变更时才上传配置：

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

注意：当前测试服 `443` 是旧服务，不属于 LwyLink。不要停止、覆盖或抢占 `443`。

## 5. 发布 Windows 客户端更新包

推荐用管理后台发布：

1. 打开 `http://64.83.34.53:8080/admin?token=<ADMIN_TOKEN>`。
2. 进入“更新发布”。
3. 版本号填写当前版本，例如 `0.1.8`。
4. 平台选择或填写 `windows-amd64`。
5. 上传 `cmd/lwylink-windows-ui/build/bin/LwyLink.exe`。
6. 裸 `LwyLink.exe` 的安装参数留空。
7. 更新说明按实际填写。

发布后检查：

```powershell
$latest = Invoke-RestMethod -Uri "http://64.83.34.53:8080/api/client/releases/latest?platform=windows-amd64"
$localHash = (Get-FileHash -Algorithm SHA256 .\cmd\lwylink-windows-ui\build\bin\LwyLink.exe).Hash.ToLowerInvariant()

$latest | Select-Object version,platform,fileName,sizeBytes,sha256,downloadURL,createdAt
"Hash matches: $($latest.sha256 -eq $localHash)"
```

## 6. Smoke 验证

使用本地配置里的 `adminToken` 跑远端 smoke：

```powershell
$adminToken = ((Get-Content .\configs\control.prod.json -Raw) | ConvertFrom-Json).adminToken
go run .\cmd\lwylink-smoke -api http://64.83.34.53:8080 -overlay 64.83.34.53:35768 -admin-token $adminToken
```

期望最后出现：

```text
SMOKE OK
```

远端状态检查：

```powershell
ssh root@64.83.34.53 @'
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(443|8080|35768) ' || true
journalctl -u lwylink-control -n 50 --no-pager
journalctl -u lwylink-node -n 50 --no-pager
'@
```

## 7. 多节点配置

### 7.1 节点调度机制

控制平面会在可用节点中自动选择房间节点：

- 节点必须 `enabled=true`。
- 节点状态必须是 `online`。
- 当前房间数不能达到 `maxRooms`。
- 多个节点可用时，按 `当前房间数 / 最大房间数` 选择负载最低的节点。
- 负载相同则选择 ID 更小的节点。

节点服务器不需要配置 `nodeID`。`lwylink-node` 启动时会用本机 `overlayPrivateKey` 推导 `overlayPublicKey`，再到控制平面的 MySQL 里查找对应节点记录。

### 7.2 新增一台独立节点服务器

新节点必须能访问控制中心使用的 MySQL 和 Redis。生产环境建议用内网或专用网络访问数据库，并用防火墙只允许节点服务器访问 MySQL/Redis。

在新服务器准备目录：

```bash
mkdir -p /opt/lwylink/configs /opt/lwylink/deploy/dist
```

上传：

```powershell
scp .\deploy\dist\lwylink-node root@<NODE_IP>:/opt/lwylink/lwylink-node
scp .\deploy\lwylink-node.service root@<NODE_IP>:/etc/systemd/system/lwylink-node.service
scp .\configs\node.prod.example.json root@<NODE_IP>:/opt/lwylink/configs/node.prod.json
```

编辑新节点的 `/opt/lwylink/configs/node.prod.json`：

```json
{
  "overlayAddr": "0.0.0.0:35768",
  "mysqlDsn": "lwylink:<DB_PASSWORD>@tcp(<MYSQL_HOST>:3306)/lwylink?parseTime=true&charset=utf8mb4&loc=Local",
  "redisAddr": "<REDIS_HOST>:6379",
  "redisPassword": "",
  "tokenSecret": "<COPY_FROM_CONTROL_CONFIG>",
  "overlayPrivateKey": "",
  "roomNetworkBase": "172.16.0.0/16",
  "roomSubnetPrefix": 24,
  "defaultMTU": 1300,
  "defaultDNS": ["223.5.5.5", "119.29.29.29"]
}
```

关键点：

- `tokenSecret` 必须和控制中心 `configs/control.prod.json` 完全一致。
- `overlayPrivateKey` 新节点可以留空，第一次生成公钥时会自动写回。
- `overlayAddr` 是节点本机监听地址，通常用 `0.0.0.0:35768`。
- 后台里的“公网 Overlay 地址”要填玩家能访问到的地址，例如 `<NODE_PUBLIC_IP>:35768`。
- 后台里的“健康检查地址”要填控制中心能访问到的地址，可以是内网地址，例如 `<NODE_PRIVATE_IP>:35768`。

生成并打印节点公钥：

```bash
/opt/lwylink/lwylink-node -config /opt/lwylink/configs/node.prod.json -print-public-key
```

复制输出的 `overlayPublicKey`，到管理后台添加节点：

1. 打开 `http://<CONTROL_HOST>:8080/admin?token=<ADMIN_TOKEN>`。
2. 进入“节点”。
3. 名称：例如 `node-2`。
4. 公网 Overlay 地址：例如 `<NODE_PUBLIC_IP>:35768`。
5. 健康检查地址：例如 `<NODE_PRIVATE_IP>:35768`，没有内网就填公网地址。
6. Overlay 公钥：填刚打印出的 `overlayPublicKey`。
7. 最大房间：按机器配置填写，例如 `100`。
8. 状态选择“启用”并保存。

启动节点：

```bash
chmod +x /opt/lwylink/lwylink-node
chmod 600 /opt/lwylink/configs/node.prod.json
systemctl daemon-reload
systemctl enable --now lwylink-node
systemctl restart lwylink-node
systemctl status lwylink-node --no-pager
```

回到管理后台点“检查”，状态变成 `online` 后，新房间会自动调度到该节点。

### 7.3 同一台机器启动多个节点

同一台服务器跑多个节点时，每个节点必须使用不同端口和不同配置文件，例如：

```text
/opt/lwylink-node-1/configs/node.prod.json -> 0.0.0.0:35768
/opt/lwylink-node-2/configs/node.prod.json -> 0.0.0.0:35769
```

每个节点都要有独立的 `overlayPrivateKey`，并分别打印对应 `overlayPublicKey` 后在后台登记。

示例 systemd：

```ini
[Unit]
Description=LwyLink room node 2
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/lwylink-node-2
ExecStart=/opt/lwylink-node-2/lwylink-node -config /opt/lwylink-node-2/configs/node.prod.json
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

启用：

```bash
systemctl daemon-reload
systemctl enable --now lwylink-node-2
```

后台登记时：

- 节点 1 公网 Overlay 地址：`<SERVER_IP>:35768`
- 节点 2 公网 Overlay 地址：`<SERVER_IP>:35769`
- 健康检查地址也对应各自端口

### 7.4 防火墙端口

控制服务器：

- `8080/tcp`：HTTP API 和管理后台
- MySQL/Redis：只允许控制服务和受信节点访问，不建议公网开放

每台节点服务器：

- `35768/tcp`：Overlay TCP 控制/中继
- `35768/udp`：UDP 打洞协商

如果同机多节点，按实际端口开放，例如 `35769/tcp` 和 `35769/udp`。

## 8. 回滚

如果服务端更新后异常：

1. 重新上传上一版 `lwylink-control` / `lwylink-node`。
2. 远端重新 `install` 覆盖二进制。
3. `systemctl restart lwylink-control lwylink-node`。
4. 查看日志和 smoke。

如果客户端更新包异常：

1. 管理后台重新上传上一版客户端，版本号必须比客户端当前版本更高才会触发自动更新。
2. 如果只是阻止继续更新，可以暂时上传一个修复包，或在代码里调整版本策略后重新发布。

## 9. 常用一键命令

项目已有自动部署脚本，日常测试服更新优先用它：

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
$env:LWYLINK_TEST_SSH_PASSWORD = "<SSH_PASSWORD>"
.\deploy\deploy-test.ps1
```

包含 Windows 客户端构建：

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
$env:LWYLINK_TEST_SSH_PASSWORD = "<SSH_PASSWORD>"
.\deploy\deploy-test.ps1 -BuildWindowsUI
```

配置有变更时同步配置：

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
$env:LWYLINK_TEST_SSH_PASSWORD = "<SSH_PASSWORD>"
.\deploy\deploy-test.ps1 -UploadConfigs
```

