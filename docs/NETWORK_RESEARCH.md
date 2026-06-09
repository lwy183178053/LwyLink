# LwyLink 网络与语音调研笔记

本文件用于记录 LwyLink 网络与语音路线的调研依据。重点不是“照抄某个项目”，而是判断 LwyLink 当前路线哪里正确、哪里可能陷入误区，以及后续应该优先补哪些产品能力。

进一步的路线评分和实验标准见 `docs/VOICE_NAT_DECISION_MATRIX.md`。

WebRTC 语音决策记录见 `docs/WEBRTC_VOICE_EXPERIMENT_PLAN.md`。

NAT 打洞改进 backlog 见 `docs/NAT_TRAVERSAL_BACKLOG.md`。

开源/成熟项目与 LwyLink 模块映射见 `docs/OPEN_SOURCE_PROJECT_MAPPING.md`。

## 结论摘要

当前 LwyLink 的大方向没有错：房间成员鉴权、虚拟网卡 Overlay、UDP P2P 优先、失败后 Relay 保底、控制信号走 TCP，这些都符合成熟虚拟组网产品的基本形态。

语音这块已经做过一次重要路线收敛：把客户端产品语音从自研 Overlay 媒体路径切到 WebRTC。原生 Overlay 语音证明了房间成员、P2P/relay 路由和流量统计可以承载业务帧，但也暴露了实时媒体系统的复杂度：自适应 jitter buffer、PLC 丢包隐藏、时钟漂移、回声消除、拥塞控制、音频设备兼容和网络切换都不是“小功能”。

当前合理边界是：

1. 虚拟局域网流量继续走 LwyLink Overlay。
2. 房间语音使用成熟 WebRTC 媒体通道，LwyLink 负责房间、鉴权、信令、麦位限制和 UI。
3. `FrameVoice / UDPTypeVoice` 只保留为底层传输 smoke 和未来实验能力，不再作为 Windows 客户端主语音媒体路径。

后续优化重点应放在 TURN 兜底、WebRTC 路径可观测性、账号流量统计、UDP 打洞诊断和持续删除被替代的实验代码。

## 资料来源

- Tailscale connection types: https://tailscale.com/docs/reference/connection-types
- Tailscale DERP servers: https://tailscale.com/docs/reference/derp-servers
- Tailscale NAT traversal improvements: https://tailscale.com/blog/nat-traversal-improvements-pt-1
- NetBird NAT and connectivity: https://docs.netbird.io/about-netbird/understanding-nat-and-connectivity
- NetBird ports and firewalls: https://docs.netbird.io/about-netbird/ports-and-firewalls
- NetBird architecture: https://docs.netbird.io/about-netbird/how-netbird-works
- ZeroTier protocol: https://docs.zerotier.com/protocol/
- ZeroTier roots/moons: https://docs.zerotier.com/roots
- ZeroTier TCP relay: https://docs.zerotier.com/relay/
- Nebula relay: https://nebula.defined.net/docs/config/relay/
- Nebula lighthouse: https://nebula.defined.net/docs/config/lighthouse/
- ICE RFC 8445: https://www.rfc-editor.org/rfc/rfc8445
- TURN RFC 8656: https://www.rfc-editor.org/rfc/rfc8656.html
- WebRTC architecture: https://webrtc.github.io/webrtc-org/architecture/
- WebRTC NetEQ docs: https://webrtc.googlesource.com/src/+/main/modules/audio_coding/neteq/g3doc/index.md
- WebRTC NetEQ deep dive: https://webrtchacks.com/how-webrtcs-neteq-jitter-buffer-provides-smooth-audio/
- Mumble source/project: https://github.com/mumble-voip/mumble

## 成熟项目怎么做打洞

### Tailscale

可学习点：

- 所有连接先有 DERP relay 保底，再尝试升级到 direct。
- direct 不是一次性状态，而是持续探测、持续切换。
- 用户和运维能看到连接到底是 direct 还是 relay。
- DERP 主要是协调和兜底，不把“走 relay”当成错误。

对 LwyLink 的启发：

- 现在“TCP relay 保底 + UDP direct 优化”的方向是对的。
- 但我们缺少类似 `tailscale ping` 的路径诊断：为什么没直连、当前候选有哪些、最近一次 direct 成功/失败是什么时候。
- 不应该为了追求“无缝 P2P”而频繁抖动切换。用户更需要稳定。

### NetBird

可学习点：

- 使用 ICE 思路收集候选：host、本地候选；srflx，STUN 看到的公网映射；relay，中继候选。
- 客户端不要求公网入站端口，所有连接都可以从出站开始。
- Relay 不是失败，而是候选之一。
- Keepalive 是连接生命周期的一部分。

对 LwyLink 的启发：

- 现在 LwyLink 的候选还比较简化，应该补 STUN/reflexive candidate。
- 后续可以把 UDP 打洞逻辑往 ICE-lite 方向靠，而不是继续手写“发几次探测包就认为成功/失败”。
- 可以增加“强制 relay”开关，用于用户网络很差时避免反复尝试。

### ZeroTier

可学习点：

- root/moon 负责定位和辅助，真实流量尽量 peer-to-peer。
- 协议层同时使用 LAN discovery、端口预测、UPnP/NAT-PMP 等多种方式提升直连率。
- TCP relay 是极端网络下的 fallback。

对 LwyLink 的启发：

- 你的“控制端 + 房间节点 + 房间 overlay”思路是成立的，类似轻量 root/relay 体系。
- 但想提高打洞率，不能只靠当前公网候选和 LAN 候选，应考虑 STUN、UPnP/NAT-PMP/PCP。
- 需要把“房间节点”从单纯 relay，升级为路径诊断/候选协商中心。

### Nebula

可学习点：

- Lighthouse 负责 peer discovery。
- Relay 节点可以是专门的公开节点，也可以由配置指定。
- Punchy keepalive 用于维持 NAT 映射。

对 LwyLink 的启发：

- 房间节点可以显式承担 Lighthouse + Relay 的角色。
- 可以允许未来多节点部署，让用户选低延迟 relay，而不是所有 relay 都压到一个节点。
- Keepalive 应该按路径状态自适应，而不是无脑固定。

## 成熟项目怎么做语音

### WebRTC / NetEQ

关键点：

- WebRTC 语音不是只有 Opus 编码。
- NetEQ 同时处理 jitter buffer、packet loss concealment、乱序、迟到包、缓冲水位和播放决策。
- 接收端播放线程按固定节奏取音频，不是每来一个包就排一个播放片段。
- NetEQ 的目标是低延迟和少音频伪影之间动态平衡。

对 LwyLink 的启发：

- 当前 AudioWorklet 连续播放是正确方向，但还远远不是 NetEQ。
- 固定 800ms jitter 能掩盖很多网络抖动，但代价是语音通话延迟偏大，不像 QQ/微信电话。
- 如果目标是“优质通话”，成熟 WebRTC 媒体通道天然占优。

### Mumble

关键点：

- 专注低延迟语音房。
- 使用 Opus。
- 语音系统本身是完整产品能力，不只是网络包转发。

对 LwyLink 的启发：

- “房间语音”应该被当成核心实时媒体功能设计，而不是 IP Overlay 的一个 payload 类型附属品。
- 麦位、静音、说话状态、音量、设备、丢包、延迟，都应该形成一套独立状态。

## 当前可能的误区

### 误区一：统一传输路径等于更好

语音和游戏流量共用 P2P/Relay 路径是好事，但不代表语音应该和游戏包使用同一个发送队列和同一种可靠性策略。

语音需要：

- 低延迟优先。
- 旧包直接丢弃。
- 不被大 IP 包或 TCP 队头阻塞拖住。
- 有独立 jitter/PLC 策略。

游戏/IP 流量需要：

- 尽量透明。
- 保持包序和吞吐。
- 由上层游戏协议自己处理重传。

因此合理设计应该是：共享路径选择，分离媒体队列。

### 误区二：固定大缓冲能解决语音问题

固定 800ms 的确能降低断续，但语音延迟会明显上升。成熟语音系统不会只靠大缓冲，而是动态调整。

下一步不应该继续简单增加缓冲，而应该验证：

- P2P 下是否可以 80-160ms。
- Relay 下是否需要 200-500ms。
- TCP relay 是否引入队头阻塞，是否应该增加 UDP/QUIC relay。
- 是否需要 PLC 或直接使用 WebRTC NetEQ。

### 误区三：自研语音栈工作量被低估

我们现在已经处理了 RNNoise、Opus、AudioWorklet、tail hangover，但这只是入口。真正难的是：

- 音频时钟和网络时钟不一致。
- 乱序和迟到包如何处理。
- 缓冲水位如何变化而不产生“轻微中断”。
- 丢包时如何补音。
- relay 抖动下如何避免 TCP 队头阻塞。
- 多人混音时如何避免爆音和延迟累积。

如果继续原生语音，就需要把它当成一个长期模块，而不是“小功能”。

## 候选方案

### 方案 A：语音使用 WebRTC，LwyLink 只做房间和信令

做法：

- 虚拟局域网仍走 LwyLink Overlay。
- 语音单独使用 WebRTC RTCPeerConnection。
- 控制服务提供 voice signaling。
- 自建 Coturn/TURN，relay 流量仍可计入账号额度。
- UI 仍显示“P2P 语音 / TURN 语音 / Relay 语音”。

优点：

- 直接获得 Opus、NetEQ、AEC、NS、AGC、ICE、STUN、TURN。
- 最接近成熟语音产品路线。
- 对“QQ/微信电话级体验”更现实。

缺点：

- 语音路径和游戏流量不再完全统一。
- 服务端要维护 WebRTC 信令和 TURN 计费。
- 调试路径变多。

适合目标：

- 优先把语音体验做对。

### 方案 B：继续原生 Overlay 语音，但做成 RTP-like 媒体层

做法：

- 保留 `FrameVoice / UDPTypeVoice`。
- VoicePayload 增加更明确的 media 字段：streamID、packetDuration、marker、audioLevel。
- 语音发送进入独立高优先级队列。
- TCP relay 下语音和 IP 分离写队列，避免被大包阻塞。
- 客户端实现 adaptive jitter + PLC + per-peer stats。
- P2P 和 Relay 使用不同初始 jitter。

优点：

- 保持 LwyLink overlay 一体化。
- 语音流量完全纳入现有 P2P/relay 体系。
- 不依赖浏览器 WebRTC 信令模型。

缺点：

- 工作量明显更大。
- 很容易继续踩 NetEQ 已经解决过的坑。
- 第一版体验可能仍不如 WebRTC。

适合目标：

- 产品长期坚持“所有业务都走 LwyLink 自研 overlay”。

### 方案 C：混合路线

做法：

- 默认 WebRTC 语音。
- Windows UI 不再保留原生 Overlay 语音 fallback，避免双媒体引擎带来的维护和回声问题。
- 底层 `FrameVoice / UDPTypeVoice` 可继续作为传输层 smoke/实验能力存在。
- 用同一套 UI 展示语音状态和 WebRTC 路径。

优点：

- 可快速获得成熟语音体验。
- 不放弃自研 overlay 语音的探索。
- 适合测试阶段做 A/B。

缺点：

- 短期复杂度最高。
- 需要明确不要长期维护两套同等优先级的实现。

适合目标：

- 保留历史依据，但不要重新引入第二套客户端媒体引擎。

## 当前后续重点

### WebRTC 语音产品化

- 部署并验证 TURN 兜底。
- 记录 ICE candidate 类型、RTT、jitter、packet loss。
- UI 展示 P2P/TURN/连接不佳的原因，而不是只显示笼统状态。
- TURN 流量后续纳入账号额度或至少进入可观测统计。

### 打洞路径诊断

- 增加 STUN reflexive candidate。
- 记录每个 peer 的候选、探测结果、选中路径、失败原因。
- 客户端/管理端能看到为什么走 relay。
- 用户不再只能看到“优化连接/稳定连接”这种模糊词。

### 代码瘦身

WebRTC 语音听感已经通过本地实测，Windows 客户端主路线切到 WebRTC。后续不再维护两套客户端媒体引擎；已经被 WebRTC 替代的实验代码应持续删除，底层 overlay voice frame 只保留为传输 smoke/实验能力。
