# LwyLink 部署与发布

这个目录保存生产部署、发布和运维相关文件。日常发布优先使用 `release.cmd`；只有排查问题、临时跳过某个阶段或服务器初始化时，才需要拆开执行下面的命令。

## 文件速览

| 文件 | 说明 |
| --- | --- |
| `release.cmd` / `release.ps1` | 一键发布入口：同步版本、构建、部署、发布 Windows 客户端并校验 latest 接口 |
| `check.ps1` | 日常开发自检入口：Go 测试、前端构建、发布脚本自测和空白检查 |
| `build-all.ps1` | 本机一键全编译 |
| `backup-remote.ps1` | 远端 MySQL、Redis 和配置备份，可安装每日 systemd timer |
| `post-release-check.ps1` | 发布后巡检：healthz、latest release、smoke、coturn 和 backup timer |
| `upload-deploy.ps1` | 上传已有构建产物、安装 systemd、重启并可选 smoke 验证 |
| `setup-turn.ps1` | 安装/配置 coturn，为 WebRTC 语音提供 TURN 兜底 |
| `prometheus-alerts.yml` | Prometheus 告警规则：TURN 扣费失败、coturn 日志延迟、节点掉线 |
| `turn.example.json` | TURN 本机配置模板，复制到 `configs/turn.local.json` 后填写密钥 |
| `test-release.ps1` | 发布脚本自测，检查版本同步幂等和后台占位符不被误改 |
| `docker-compose.prod.yml` | 生产版 MySQL 和 Redis，只绑定 `127.0.0.1` |
| `lwylink-control.service` | 控制中心 systemd 服务 |
| `lwylink-node.service` | 房间节点 systemd 服务 |
| `TEST_SERVER.md` | 当前测试服务器、端口、远端路径和自动部署速查 |

生产配置位于：

- `configs/control.prod.json`：控制平面配置，包含 HTTP API、管理令牌、数据库、Redis、房间调度和清理参数。
- `configs/node.prod.json`：房间节点配置，包含 Overlay 监听、节点私钥、数据库、Redis、vDHCP 参数。
- `configs/deploy.local.json`：本机发布目标和 SSH 凭据，可由 `configs/deploy.example.json` 复制创建。

这些生产配置包含密钥和密码，不要提交、截图或完整贴到聊天里。仓库只保留 `.example.json` 示例配置，真实 `*.prod.json` 和 `*.local.json` 只放在本机或服务器。

## 程序分工

| 程序 | 部署位置 | 职责 |
| --- | --- | --- |
| `lwylink-control` | 控制服务器 | 玩家 API、管理后台、节点调度、节点健康检查、空房清理、客户端更新发布 |
| `lwylink-node` | 每台房间节点服务器 | `tcpx` 加密 Overlay、UDP 打洞协商、vDHCP、房间内 IPv4 包转发 |
| `lwylink-smoke` | 本地或服务器 | 端到端验证注册、登录、房间、Overlay、广播、踢人、删除和清理 |

控制 API 和 Overlay 端口分开配置。当前测试服约定：

| 端口 | 归属 | 说明 |
| --- | --- | --- |
| `127.0.0.1:8080/tcp` | `lwylink-control` | 本机 HTTP API 和管理后台，只允许 HTTPS 反向代理访问 |
| `35768/tcp` | `lwylink-node` | Overlay TCP 控制/中继 |
| `35768/udp` | `lwylink-node` | UDP 打洞协商 |
| `443/tcp` | HTTPS 反向代理 | `www.lwylink.xyz` / `lwylink.xyz`，反代到控制端 `8080` |
| `3478/udp` | coturn | WebRTC 语音 TURN 中继兜底，直连失败时使用 |
| `49160-49200/udp` | coturn | TURN relay 媒体端口池，可按规模扩大 |

## 日常一键发布

先准备本机部署配置：

```powershell
Copy-Item .\configs\deploy.example.json .\configs\deploy.local.json
```

在 `configs/deploy.local.json` 中填写 `hostName`、`user`、`port`、`password`、`apiURL` 和 `overlayAddr`。这个文件已被 Git 忽略，适合保存本机测试服发布参数。

```powershell
.\deploy\release.cmd -Version <version> -Notes "本次更新说明"
```

发布脚本会依次执行：

1. 同步 Windows 客户端版本号到 Go、Wails、前端包文件、前端默认状态和管理端占位文本。
2. 调用 `build-all.ps1 -Vet -AppVersion <version>` 全量构建。
3. 调用 `backup-remote.ps1` 备份远端配置、MySQL 和 Redis。
4. 调用 `upload-deploy.ps1` 上传 Linux 服务端二进制、安装 systemd、重启并跑 smoke。
5. 上传 `build/windows-amd64/LwyLink.exe` 到管理端“更新发布”。
6. 请求 `/api/client/releases/latest` 校验线上 latest 版本号和 SHA256。
7. 自动调用 `post-release-check.ps1` 做完整发布后巡检。

如需单独重跑巡检：

```powershell
.\deploy\post-release-check.ps1
```

这条命令会检查 `/healthz`、Windows latest release、端到端 smoke、远端 `coturn` active 和 `lwylink-backup.timer` active。
新环境尚未安装 coturn 或备份定时器时，可显式加 `-SkipCoturn` 或 `-SkipBackupTimer` 只跑已启用组件。

当前阶段客户端协议和账号/网络逻辑经常是不兼容更新，发布脚本会默认把 Windows 客户端标记为强制更新。
客户端发布和 smoke 默认使用公网 HTTPS API：`https://www.lwylink.xyz`。控制端 `8080` 只监听本机，不再作为公网排障入口；如需排查反向代理，优先在服务器本机 `curl http://127.0.0.1:8080`。

常用参数：

```powershell
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipSmoke
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipDeploy
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipClientRelease
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipBuild
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipBackup
.\deploy\release.cmd -Version <version> -Notes "说明" -SkipPostReleaseCheck
.\deploy\release.cmd -Version <version> -Notes "说明" -InstallDailyBackup
.\deploy\release.cmd -Version <version> -Notes "说明" -UploadConfigs
.\deploy\release.cmd -Version <version> -Notes "说明" -Commit
```

`release.cmd` 会用 `PowerShell -ExecutionPolicy Bypass` 调用 `release.ps1`，适合本机执行策略较严格时使用。

`-SkipBuild` 会跳过全量构建并复用 `build/artifacts.json` 和 `build/windows-amd64/LwyLink.exe` 等已有产物，适合只重试部署或只重试客户端发布。

发布脚本改动后可以先跑：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\deploy\test-release.ps1
```

## 日常自检

代码整理、UI 调整、语音/Overlay 或外部示例改动后，优先跑统一自检：

```powershell
PowerShell -ExecutionPolicy Bypass -File .\deploy\check.ps1
```

它会依次执行：

1. `go test ./...`
2. Windows UI `npm.cmd run build`
3. Python 示例 `py_compile` 和 `--self-test`
4. Java 示例 `javac` 和 `--self-test`
5. `deploy/test-release.ps1`
6. `git diff --check`

示例组网自测需要已有 `build/lwylink-net.exe`；如果缺少该产物，先运行 `.\deploy\build-all.ps1`。

需要更重的并发检查时加 `-Race`；需要静态检查时加 `-Vet`。只改服务端时可以加 `-SkipFrontend`，不涉及外部示例时可以加 `-SkipExamples`，只改业务代码且不涉及发布脚本时可以加 `-SkipReleaseScript`。

## 构建产物

```powershell
.\deploy\build-all.ps1 -Vet
```

默认流程：

1. `go test -p 1 ./...`
2. 可选 `go vet -p 1 ./...`
3. 编译 Linux `amd64` 版 `lwylink-control`、`lwylink-node`、`lwylink-smoke` 到 `deploy/dist/`
4. 编译 Windows `amd64` 版独立虚拟组网引擎到 `build/lwylink-net.exe`
5. 构建 Wails 图形客户端，并复制 `LwyLink.exe` 到 `build/windows-amd64/`
6. 输出 `build/artifacts.json`，包含产物路径、大小和 SHA256

常用参数：

```powershell
.\deploy\build-all.ps1 -SkipTests
.\deploy\build-all.ps1 -SkipWindowsUI
.\deploy\build-all.ps1 -AppVersion <version>
.\deploy\build-all.ps1 -NoInstallWails
```

`-AppVersion` 只通过 Wails `ldflags` 注入 Go 端 `main.AppVersion`，不会修改 `wails.json`、`package.json` 或 `package-lock.json`。正式发布客户端版本时，建议使用 `release.cmd` 或手动同步版本文件。

如果本机找不到 `wails`，脚本会尝试自动安装 `github.com/wailsapp/wails/v2/cmd/wails@v2.12.0`。不希望自动安装时加 `-NoInstallWails`。

## 上传部署

先编译：

```powershell
.\deploy\build-all.ps1
```

再上传到默认测试服务器：

```powershell
.\deploy\upload-deploy.ps1
```

也可以显式指定目标：

```powershell
.\deploy\upload-deploy.ps1 -HostName 64.83.34.53 -User root -Port 22
```

脚本默认上传 `deploy/dist/` 下的三个 Linux 产物和两个 systemd 文件，然后安装到 `/opt/lwylink`、重启 `lwylink-control` 和 `lwylink-node`，最后跑 smoke。

常用参数：

```powershell
.\deploy\upload-deploy.ps1 -UploadConfigs
.\deploy\upload-deploy.ps1 -SkipSmoke
.\deploy\upload-deploy.ps1 -RemoteRoot /opt/lwylink
.\deploy\upload-deploy.ps1 -AdminToken "这里填 adminToken"
```

没有传密码时脚本会使用系统 `ssh/scp`，适合已经配置 SSH key 的机器。脚本会先用 `BatchMode` 做非交互登录预检；如果没有可用 SSH key，会直接报错，不会卡在密码交互提示里。
传了 `-Password`、设置 `LWYLINK_DEPLOY_SSH_PASSWORD`，或在 `configs/deploy.local.json` 中填写 `password` 时，会使用 `Posh-SSH`。

可用环境变量：

```powershell
$env:LWYLINK_DEPLOY_HOST = "64.83.34.53"
$env:LWYLINK_DEPLOY_SSH_USER = "root"
$env:LWYLINK_DEPLOY_SSH_PORT = "22"
$env:LWYLINK_DEPLOY_SSH_PASSWORD = "这里填 SSH 密码"
$env:LWYLINK_ADMIN_TOKEN = "这里填控制端 adminToken"
$env:LWYLINK_API_URL = "https://www.lwylink.xyz"
```

命令行参数优先级最高，其次环境变量，最后读取 `configs/deploy.local.json`。临时切换服务器时直接传 `-HostName` / `-Password`，不需要改本地配置文件。

## 备份与恢复准备

发布脚本默认会在部署前执行一次远端备份。备份保存在服务器 `$RemoteRoot/backups/<UTC 时间戳>/`，默认保留 14 天，目录内包含：

- `configs.tgz`：`configs/`、`deploy/.env`、生产 docker compose 和 systemd 文件。
- `mysql-lwylink.sql.gz`：`lwylink-mysql` 容器内 `lwylink` 数据库 dump。
- `redis-dump.rdb.gz`：`lwylink-redis` 当前 RDB 快照。
- `manifest.sha256`：备份文件校验清单。

单独备份一次：

```powershell
.\deploy\backup-remote.ps1
```

安装每日自动备份 timer：

```powershell
.\deploy\backup-remote.ps1 -InstallDailyTimer
```

常用参数：

```powershell
.\deploy\backup-remote.ps1 -BackupRoot /opt/lwylink/backups -RetentionDays 30
.\deploy\backup-remote.ps1 -SkipRedis
.\deploy\backup-remote.ps1 -SkipDatabase
.\deploy\backup-remote.ps1 -SkipConfigs
```

每日 timer 会安装 `/usr/local/sbin/lwylink-backup`、`lwylink-backup.service` 和 `lwylink-backup.timer`，默认每天 03:20 运行。真实恢复前先解压备份并校验 `manifest.sha256`。

## Prometheus 指标与告警

控制端暴露 Prometheus 文本指标：

```text
GET /metrics
```

当前覆盖 TURN 扣费失败、coturn usage 日志延迟、房间在线数、Overlay 连接数、relay/TURN 流量、节点掉线和下载趋势。告警规则模板在 `deploy/prometheus-alerts.yml`。

## TURN 语音兜底

WebRTC 语音在普通 NAT 下会优先走 P2P；遇到严格 NAT、企业网络或 UDP 映射异常时，需要 TURN 中继兜底。控制端支持 TURN REST 短期凭据，长效 `staticAuthSecret` 只保存在 coturn 和控制端配置中，客户端只拿短期用户名和密码。

首次准备本机 TURN 配置：

```powershell
Copy-Item .\deploy\turn.example.json .\configs\turn.local.json
```

填写 `configs/turn.local.json`：

- `realm`：通常用 `lwylink.xyz`。
- `publicHost`：客户端可访问的 TURN 域名，例如 `www.lwylink.xyz` 或单独的 `turn.lwylink.xyz`。
- `staticAuthSecret`：随机长密钥，必须和 coturn/control 配置一致。
- `listeningPort`：默认 `3478`。
- `minPort` / `maxPort`：TURN relay UDP 端口池，测试服可先用 `49160-49200`，用户变多后扩大。

预检：

```powershell
.\deploy\setup-turn.ps1 -ValidateOnly
```

安装并配置 coturn，同时更新本机 `configs/control.prod.json` 的 `voiceTURNCredential` 和服务端 TURN usage log 计费路径：

```powershell
.\deploy\setup-turn.ps1 -UpdateLocalControlConfig
```

只更新本机控制端配置、不连接服务器：

```powershell
.\deploy\setup-turn.ps1 -UpdateLocalControlConfig -LocalConfigOnly
```

然后把控制端配置同步到服务器并重启控制端：

```powershell
.\deploy\upload-deploy.ps1 -UploadConfigs -SkipSmoke
```

安全注意：

- 不要提交 `configs/turn.local.json`、`configs/control.prod.json` 或真实 `staticAuthSecret`。
- 云安全组/防火墙需要允许 `3478/udp` 和 relay 端口池 UDP 入站。
- 没有 TLS TURN 时优先使用 `turn:域名:3478?transport=udp`；`turns:` 需要证书路径和 `-EnableTLS`。
- TURN 会消耗服务器带宽。`setup-turn.ps1 -UpdateLocalControlConfig` 会把 `usageLogPath` 设置为 `/var/log/lwylink/coturn.log`，控制端按 coturn 服务端 usage 日志扣账号中继流量；客户端 WebRTC stats 只作为未配置 usage log 时的测试兜底。

## 新增房间节点

新节点必须能访问控制中心使用的 MySQL 和 Redis。生产环境建议通过内网或专用网络访问数据库，并用防火墙限制来源。

1. 在新服务器创建目录：

```bash
mkdir -p /opt/lwylink/configs /opt/lwylink/deploy/dist
```

2. 上传节点程序、systemd 文件和节点配置：

```powershell
scp .\deploy\dist\lwylink-node root@<NODE_IP>:/opt/lwylink/lwylink-node
scp .\deploy\lwylink-node.service root@<NODE_IP>:/etc/systemd/system/lwylink-node.service
scp .\configs\node.prod.example.json root@<NODE_IP>:/opt/lwylink/configs/node.prod.json
```

3. 调整 `/opt/lwylink/configs/node.prod.json`：

| 字段 | 说明 |
| --- | --- |
| `overlayAddr` | 节点监听地址，例如 `0.0.0.0:35768` |
| `mysqlDsn` | 指向控制平面使用的 MySQL |
| `redisAddr` / `redisPassword` | 指向控制平面使用的 Redis |
| `tokenSecret` | 必须复制控制平面 `configs/control.prod.json` 中的值 |
| `overlayPrivateKey` | 新节点可以留空，打印公钥时会自动生成并写回 |

4. 生成节点公钥：

```bash
chmod +x /opt/lwylink/lwylink-node
/opt/lwylink/lwylink-node -config /opt/lwylink/configs/node.prod.json -print-public-key
```

5. 在控制后台“节点”模块新增节点：

| 字段 | 示例 |
| --- | --- |
| 名称 | `node-2` |
| 公网 Overlay 地址 | `<NODE_PUBLIC_IP>:35768` |
| 健康检查地址 | 控制平面能访问的地址，可以是内网地址 |
| Overlay 公钥 | 第 4 步输出的值 |
| 最大房间数 | 例如 `100` |
| 状态 | 启用 |

6. 启动节点：

```bash
chmod 600 /opt/lwylink/configs/node.prod.json
systemctl daemon-reload
systemctl enable --now lwylink-node
systemctl restart lwylink-node
systemctl status lwylink-node --no-pager
```

控制后台点“检查”，节点状态为 `online` 后，新房间会自动调度到该节点。

## 同机多节点

同一台机器跑多个节点时，每个节点必须使用独立目录、独立配置、独立端口和独立 `overlayPrivateKey`：

```text
/opt/lwylink-node-1/configs/node.prod.json -> 0.0.0.0:35768
/opt/lwylink-node-2/configs/node.prod.json -> 0.0.0.0:35769
```

每个节点都要分别打印 `overlayPublicKey` 并在后台登记。防火墙也要开放对应的 TCP/UDP 端口。

## 安全注意

- `configs/control.prod.json`、`configs/node.prod.json`、`deploy/.env` 必须保密，并且不应提交到 Git。
- 控制平面配置里的 `tokenSecret`、`adminToken` 可以留空；控制端第一次启动时会自动生成并写回 JSON 文件。
- 房间节点配置里的 `overlayPrivateKey` 可以留空；节点第一次打印公钥时会自动生成并写回。
- 房间节点配置里的 `tokenSecret` 必须和控制平面一致。
- 公网入口使用 HTTPS 反向代理，控制端必须只监听 `127.0.0.1:8080`，不要把 `8080/tcp` 暴露到公网。

## 故障排查命令

```bash
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E '(127\.0\.0\.1:8080|:443|:35768) ' || true
journalctl -u lwylink-control -n 50 --no-pager
journalctl -u lwylink-node -n 50 --no-pager
```

本地跑远端 smoke：

```powershell
$env:LWYLINK_ADMIN_TOKEN = "这里填控制端 adminToken"
go run .\cmd\lwylink-smoke -api https://www.lwylink.xyz -overlay 64.83.34.53:35768
```
