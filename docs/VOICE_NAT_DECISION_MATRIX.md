# LwyLink Voice and NAT Decision Matrix

本文件是 `NETWORK_RESEARCH.md` 的后续决策表。目标是在改代码前先明确：我们要用什么证据判断“哪条路线最好”。

## 目标定义

### 语音目标

LwyLink 的房间语音不应该只是“能听见”，而应该接近常见语音软件体验：

- 正常说话不断句。
- 小声和尾音不被吃掉。
- 没有明显金属音、短促断续、爆音。
- P2P 下端到端延迟尽量低。
- Relay 下宁可略高延迟，也不能频繁断续。
- 多人开麦时不会因为混音或队列堆积变得混乱。

### 组网目标

LwyLink 的虚拟局域网核心目标仍然是：

- 默认可用：打洞失败也能进入房间。
- Direct 优先：LAN /公网候选成功时尽快走 P2P。
- Relay 稳定：Relay 是产品能力，不是临时错误处理。
- 状态可解释：用户和管理员能知道为什么当前是 P2P 或 Relay。
- 不频繁抖动：网络路径切换要有稳定窗口和冷却策略。

## 路线评分

评分范围：1 到 5，越高越好。

| 维度 | WebRTC 语音 | 原生 Overlay 语音 | 混合实验路线 |
| --- | ---: | ---: | ---: |
| 语音质量上限 | 5 | 3 | 5 |
| 低延迟能力 | 5 | 3 | 5 |
| 开发复杂度 | 3 | 2 | 1 |
| 与现有 Overlay 统一性 | 2 | 5 | 3 |
| NAT/Relay 成熟度 | 5 | 3 | 5 |
| 可观测性 | 4 | 3 | 4 |
| 长期维护成本 | 4 | 2 | 2 |
| 流量计费可控性 | 3 | 5 | 3 |

初步判断：

- 如果目标是尽快接近 QQ/微信电话效果，WebRTC 语音更有胜算。
- 如果目标是证明 LwyLink overlay 可以承载所有业务，原生 Overlay 语音更统一，但必须投入更多实时媒体工程。
- 混合路线适合测试阶段找答案，但不适合作为长期产品架构。

## WebRTC 语音实验设计

### 最小范围

- 只支持 Windows 客户端。
- 只做房间内语音，不做视频。
- 复用现有账号、房间、麦位限制。
- 新增信令接口，但不改变虚拟局域网传输。
- 使用公网 STUN。
- 允许配置 TURN；如果没有 TURN，先记录 P2P 成功率。

### 需要新增的服务端能力

- `POST /api/rooms/{id}/voice/webrtc/offer`
- `POST /api/rooms/{id}/voice/webrtc/answer`
- `POST /api/rooms/{id}/voice/webrtc/ice`
- 房间事件流推送 offer/answer/ice。
- 信令消息只允许同房间、已加入语音的用户互发。
- 信令过期清理，避免堆积。

### 客户端能力

- `RTCPeerConnection` 管理。
- `getUserMedia` 采集。
- `audio` track 发送和接收。
- 读取 `getStats()`：
  - candidate pair 类型。
  - roundTripTime。
  - packetsLost。
  - jitter。
  - bytesSent/bytesReceived。
- UI 展示：
  - `WebRTC P2P 语音`
  - `TURN 语音`
  - `语音连接中`
  - `语音质量不佳`

### 成功标准

- 两个客户端同房间，语音连续性明显好于当前原生 Overlay 语音。
- 小声和尾音不被明显吃掉。
- 端到端延迟主观明显低于固定 800ms 原生方案。
- `getStats()` 能解释当前路径。
- 失败时能回落到 TURN，而不是静默失败。

## 原生 Overlay 语音实验设计

### 最小范围

- 继续使用 `FrameVoice / UDPTypeVoice`。
- 不改变房间和账号接口。
- 不再调大固定缓冲。
- 重点重写接收端播放策略。

### 需要补的媒体层能力

- Per-peer jitter estimator。
- Per-peer adaptive target delay。
- Packet loss concealment：
  - 单帧丢失：复制上一帧并衰减。
  - 多帧丢失：渐入静音。
- AudioWorklet 内部水位控制：
  - 队列过低先平滑，不立即硬断。
  - 队列过高丢旧帧，不无限累积延迟。
- 语音发送高优先级：
  - TCP relay 下语音帧不应被大 IP 包长期阻塞。
  - IP 和 Voice 至少要有独立逻辑队列。

### 成功标准

- P2P 模式初始 jitter 不超过 160ms。
- Relay 模式初始 jitter 不超过 350ms。
- 网络抖动时允许短暂增大，但不能长期停留在 800ms。
- 自动 QA 能输出 underrun、concealment、late packet、jitter target。
- 实际听感不能比 WebRTC 实验差太多。

## NAT 打洞实验设计

### 当前缺口

LwyLink 现在能做 LAN/public candidate 和 UDP rendezvous，但还缺：

- STUN server-reflexive candidate。
- UPnP/NAT-PMP/PCP 端口映射尝试。
- 候选 pair 级别的诊断。
- 多 relay 节点选择。
- 用户可见的 NAT/路径解释。

### ICE-lite 改造方向

不用一次性完整实现 ICE RFC，但可以借鉴优先级：

1. Host/同 LAN candidate。
2. STUN server-reflexive candidate。
3. 已知公网 endpoint。
4. Relay candidate。

每个 candidate pair 应记录：

- local type。
- remote type。
- endpoint。
- first probe time。
- last success time。
- RTT。
- failure reason。
- selected/backup。

### 成功标准

- Smoke 输出每个 peer 的候选和最终路径。
- UI 能告诉用户“当前走 Relay，因为 STUN 候选探测失败/UDP 被阻断/对端疑似对称 NAT”。
- 新 peer 加入后能触发 bounded re-probe。
- 短时间多次失败后能进入本次启动禁用 UDP 的稳定状态。

## 不再继续做的事

- 不再单纯上调固定 jitter。
- 不再把“能听见声音”当作语音完成标准。
- 不再隐藏 P2P/Relay 失败原因。
- 不在没有实验数据时重写主线。
- 不在当前阶段维护两套长期并行的语音产品路线。

## 推荐执行顺序

1. 搭建 WebRTC 语音最小实验。
2. 同时加语音/打洞诊断，不改变正式体验。
3. 用两台本地客户端和测试服务器做 A/B：
   - WebRTC P2P。
   - WebRTC TURN。
   - 原生 Overlay P2P。
   - 原生 Overlay TCP Relay。
4. 对比：
   - 主观听感。
   - 往返延迟。
   - jitter。
   - packet loss。
   - tail clipping。
   - CPU。
   - relay 流量。
5. 选择主路线后再大改代码。

## 实验停止条件

### 可以停止继续调研，开始写实验代码的条件

- 已经明确 WebRTC 实验的最小服务端接口。
- 已经明确前端 `RTCPeerConnection` 集成位置。
- 已经明确使用现有 SSE 作为第一版 signaling transport。
- 已经明确和当前原生 Overlay 语音的 A/B 指标。
- 已经明确不发布、不强制更新、不删除原生语音。

目前这些条件已经基本满足，因此下一步可以开 `experiment/webrtc-voice` 分支写最小闭环。

### WebRTC 胜出的条件

- 小声和尾音明显优于当前原生 Overlay 语音。
- 端到端主观延迟明显低于固定大缓冲方案。
- P2P 和 TURN 路径都能被 `getStats()` 清楚解释。
- TURN 成本和部署复杂度可以接受。
- Wails/WebView2 下稳定。

### 原生 Overlay 胜出的条件

- WebRTC 在 Wails/WebView2 下设备兼容或权限体验不好。
- TURN 部署/计费不适合当前产品阶段。
- 原生 RTP-like 改造可以在较小复杂度下达到接近 WebRTC 的听感。
- 保持单一 overlay 传输带来的产品收益明显大于媒体栈复杂度。

## 当前建议

先做 WebRTC 语音最小实验，作为“成熟方案基准线”。如果 WebRTC 实验明显更好，就把房间语音主路线迁移到 WebRTC，并让 LwyLink Overlay 专注虚拟局域网和游戏/应用流量。如果 WebRTC 在 Wails/WebView2 或 TURN 计费上带来明显不可接受的问题，再回头按 RTP-like 方向重做原生 Overlay 语音。
