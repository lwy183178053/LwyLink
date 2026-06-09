package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
	"github.com/LwyV-Labs/LwyLink/internal/tun"
)

type ConnectOptions struct {
	OverlayAddr      string
	UDPOverlayAddr   string
	STUNAddr         string
	ServerPublicKey  string
	IfName           string
	Token            string
	UserID           int64
	Room             room.Room
	OnConnected      func(overlay.DHCPResponse)
	OnTransportState func(TransportStatus)
}

type connectionRuntime struct {
	mu          sync.RWMutex
	secureReady bool
	tunnel      *tun.Tunnel
	udp         *udpManager
	ipSeq       uint64
	ipIDPrefix  uint64
	seenIP      *recentPacketIDs
}

func (r *connectionRuntime) setSecureReady(ready bool) {
	r.mu.Lock()
	r.secureReady = ready
	r.mu.Unlock()
}

func (r *connectionRuntime) isSecureReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.secureReady
}

func (r *connectionRuntime) setTunnel(t *tun.Tunnel) {
	r.mu.Lock()
	r.tunnel = t
	r.mu.Unlock()
}

func (r *connectionRuntime) writeTunnelPayload(payload []byte) {
	pkt, ok := r.acceptIPPayload(payload)
	if !ok {
		return
	}
	r.writeTunnelPacket(pkt)
}

func (r *connectionRuntime) writeTunnelPacket(pkt []byte) {
	r.mu.RLock()
	t := r.tunnel
	r.mu.RUnlock()
	if t != nil {
		_ = t.Write(pkt)
	}
}

func (r *connectionRuntime) setUDP(manager *udpManager) {
	r.mu.Lock()
	r.udp = manager
	r.mu.Unlock()
}

func (r *connectionRuntime) udpManager() *udpManager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.udp
}

func (r *connectionRuntime) closeUDP() {
	r.mu.Lock()
	manager := r.udp
	r.udp = nil
	r.mu.Unlock()
	if manager != nil {
		_ = manager.Close()
	}
}

func (r *connectionRuntime) nextIPPayload(pkt []byte) []byte {
	r.mu.Lock()
	if r.ipIDPrefix == 0 {
		r.ipIDPrefix = randomUint64()
		if r.ipIDPrefix == 0 {
			r.ipIDPrefix = uint64(time.Now().UnixNano())
		}
	}
	r.ipSeq++
	packetID := r.ipIDPrefix + r.ipSeq
	r.mu.Unlock()
	return overlay.PackIPPayload(packetID, pkt)
}

func (r *connectionRuntime) acceptIPPayload(payload []byte) ([]byte, bool) {
	packetID, pkt, err := overlay.UnpackIPPayload(payload)
	if err != nil {
		return nil, false
	}
	r.mu.Lock()
	if r.seenIP == nil {
		r.seenIP = newRecentPacketIDs(4096)
	}
	ok := r.seenIP.Accept(packetID)
	r.mu.Unlock()
	return pkt, ok
}

func ConnectRoom(ctx context.Context, opts ConnectOptions) error {
	opts = normalizeConnectOptions(opts)
	if opts.Token == "" {
		return fmt.Errorf("not logged in")
	}
	if opts.UserID <= 0 {
		return fmt.Errorf("bad user")
	}
	if opts.Room.ID <= 0 {
		return fmt.Errorf("bad room")
	}
	if opts.OverlayAddr == "" {
		return fmt.Errorf("overlay address is empty")
	}
	if opts.ServerPublicKey == "" {
		return fmt.Errorf("overlay server public key is empty")
	}
	serverPubHex, err := tcpx.NormalizePublicKeyHex(opts.ServerPublicKey)
	if err != nil {
		return fmt.Errorf("bad overlay server public key: %w", err)
	}
	requestID := newRequestID()

	runtimeState := &connectionRuntime{ipIDPrefix: randomUint64(), seenIP: newRecentPacketIDs(4096)}
	var (
		dhcpCh       = make(chan overlay.DHCPResponse, 1)
		udpConfigCh  = make(chan overlay.UDPControl, 4)
		udpPeerCh    = make(chan overlay.UDPPeerCandidate, 64)
		udpResetCh   = make(chan struct{}, 1)
		udpDisableCh = make(chan struct{}, 1)
		errCh        = make(chan error, 1)
	)

	transport, err := tcpx.NewClient(tcpx.ClientConfig{
		Addr:               opts.OverlayAddr,
		ServerPublicKeyHex: serverPubHex,
		BaseConfig: tcpx.BaseConfig{
			ReadTimeout:       18 * time.Second,
			WriteTimeout:      8 * time.Second,
			HeartbeatBase:     5 * time.Second,
			HeartbeatJitter:   time.Second,
			KeyRotateInterval: 45 * time.Second,
			OldKeyGrace:       20 * time.Second,
		},
		ReconnectBase:   time.Second,
		ReconnectJitter: 3 * time.Second,
		OnConnect: func(conn *tcpx.SecureConn) {
			// Auth is resent after every tcpx reconnect. The server will return
			// the same vDHCP lease for this room/user when possible.
			raw, err := overlay.PackJSON(overlay.FrameAuth, overlay.AuthRequest{
				Token:         opts.Token,
				RoomID:        opts.Room.ID,
				RoomPublicKey: opts.Room.RoomPublicKey,
				RequestID:     requestID,
			})
			if err != nil {
				_ = conn.Close()
				pushErr(errCh, err)
				return
			}
			if err := conn.Write(raw); err != nil {
				_ = conn.Close()
				pushErr(errCh, err)
			}
		},
		OnDisconnect: func(err error) {
			if err != nil {
				log.Printf("overlay disconnected, tcpx will retry: %v", err)
			}
			runtimeState.setSecureReady(false)
			select {
			case udpResetCh <- struct{}{}:
			default:
			}
		},
		OnMessage: func(conn *tcpx.SecureConn, raw []byte) {
			typ, payload, err := overlay.UnpackFrame(raw)
			if err != nil {
				pushErr(errCh, err)
				_ = conn.Close()
				return
			}
			switch typ {
			case overlay.FrameDHCP:
				var lease overlay.DHCPResponse
				if err := json.Unmarshal(payload, &lease); err != nil {
					pushErr(errCh, err)
					_ = conn.Close()
					return
				}
				if lease.RequestID != requestID {
					// Ignore stale DHCP responses from an earlier connection
					// attempt; otherwise the client could configure the wrong IP.
					pushErr(errCh, fmt.Errorf("dhcp request id mismatch"))
					_ = conn.Close()
					return
				}
				runtimeState.setSecureReady(true)
				select {
				case dhcpCh <- lease:
				default:
				}
			case overlay.FrameIP:
				runtimeState.writeTunnelPayload(payload)
			case overlay.FrameError:
				var e map[string]string
				_ = json.Unmarshal(payload, &e)
				if e["error"] == "" {
					e["error"] = string(payload)
				}
				pushErr(errCh, fmt.Errorf("overlay error: %s", e["error"]))
				_ = conn.Close()
			case overlay.FramePing:
			case overlay.FrameUDPControl:
				var control overlay.UDPControl
				if err := json.Unmarshal(payload, &control); err != nil {
					return
				}
				switch control.Type {
				case overlay.UDPControlConfig:
					sendLatestUDPConfig(udpConfigCh, control)
				case overlay.UDPControlPeer:
					if control.Peer != nil {
						sendLatestUDPPeer(udpPeerCh, *control.Peer)
					}
				}
			default:
				log.Printf("unknown overlay frame: %d", typ)
			}
		},
	})
	if err != nil {
		return err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	go func() {
		if err := transport.Run(runCtx); err != nil && runCtx.Err() == nil {
			pushErr(errCh, err)
		}
	}()
	defer transport.Close()

	var lease overlay.DHCPResponse
	select {
	case lease = <-dhcpCh:
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	t, err := tun.NewTunnel(opts.IfName, lease.MTU)
	if err != nil {
		return err
	}
	defer t.Close()
	defer tun.CleanupRoute(opts.IfName, opts.Room.NetworkCIDR)
	runtimeState.setTunnel(t)

	// Route configuration happens only after the server has authenticated this
	// token and assigned a lease. On non-Windows builds this returns a stub error.
	if err := tun.ConfigureAddressAndRoute(opts.IfName, lease.IP, lease.SubnetMask, opts.Room.NetworkCIDR, lease.Gateway); err != nil {
		return err
	}
	if opts.OnConnected != nil {
		opts.OnConnected(lease)
	}
	if opts.OnTransportState != nil {
		opts.OnTransportState(TransportStatus{Mode: TransportModeTCPRelay, Message: "中继连接"})
	}

	log.Printf("虚拟网络已连接: room=%s ip=%s cidr=%s if=%s", opts.Room.Name, lease.IP, opts.Room.NetworkCIDR, opts.IfName)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go manageUDPTransport(childCtx, opts, lease, runtimeState, udpConfigCh, udpPeerCh, udpResetCh, udpDisableCh)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tunToOverlay(childCtx, transport, t, runtimeState)
		cancel()
	}()
	go func() {
		defer wg.Done()
		<-childCtx.Done()
		cancel()
	}()
	select {
	case <-childCtx.Done():
	case err := <-errCh:
		cancel()
		runtimeState.closeUDP()
		_ = transport.Close()
		_ = t.Close()
		wg.Wait()
		return err
	}
	runtimeState.closeUDP()
	_ = transport.Close()
	_ = t.Close()
	wg.Wait()
	return nil
}

func sendLatestUDPConfig(ch chan overlay.UDPControl, control overlay.UDPControl) {
	sendLatest(ch, control)
}

func sendLatestUDPPeer(ch chan overlay.UDPPeerCandidate, peer overlay.UDPPeerCandidate) {
	sendLatest(ch, peer)
}

func sendLatest[T any](ch chan T, value T) {
	select {
	case ch <- value:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- value:
	default:
	}
}

func normalizeConnectOptions(opts ConnectOptions) ConnectOptions {
	if opts.IfName == "" {
		opts.IfName = "LwyLink"
	}
	if opts.OverlayAddr == "" {
		opts.OverlayAddr = opts.Room.NodeOverlayAddr
	}
	if opts.UDPOverlayAddr == "" {
		opts.UDPOverlayAddr = opts.Room.NodeUDPAddr
	}
	if opts.UDPOverlayAddr == "" {
		opts.UDPOverlayAddr = opts.OverlayAddr
	}
	if opts.STUNAddr == "" {
		opts.STUNAddr = opts.UDPOverlayAddr
	}
	if opts.ServerPublicKey == "" {
		opts.ServerPublicKey = opts.Room.NodePublicKey
	}
	return opts
}

func manageUDPTransport(ctx context.Context, opts ConnectOptions, lease overlay.DHCPResponse, runtimeState *connectionRuntime, udpConfigCh <-chan overlay.UDPControl, udpPeerCh <-chan overlay.UDPPeerCandidate, udpResetCh <-chan struct{}, udpDisableCh chan struct{}) {
	var manager *udpManager
	var latestConfig *overlay.UDPControl
	knownPeers := make(map[string]overlay.UDPPeerCandidate)
	udpDisabledForRun := false
	emitDisabled := func() {
		if opts.OnTransportState != nil {
			opts.OnTransportState(TransportStatus{Mode: TransportModeUDPDisabled, Message: "中继连接"})
		}
	}
	closeManager := func() {
		if manager != nil {
			_ = manager.Close()
			manager = nil
		}
		runtimeState.setUDP(nil)
	}
	startManager := func() {
		if manager != nil || latestConfig == nil || udpDisabledForRun {
			return
		}
		key, err := overlay.DecodeUDPKey(latestConfig.Key)
		if err != nil {
			return
		}
		manager, err = startUDPManager(ctx, udpManagerConfig{
			ServerAddr: opts.UDPOverlayAddr,
			Token:      latestConfig.Token,
			Key:        key,
			STUNAddr:   opts.STUNAddr,
			RoomID:     opts.Room.ID,
			UserID:     opts.UserID,
			VirtualIP:  lease.IP,
			OnPacket: func(pkt []byte) {
				runtimeState.writeTunnelPayload(pkt)
			},
			OnStatus: opts.OnTransportState,
			OnDisabled: func() {
				select {
				case udpDisableCh <- struct{}{}:
				default:
				}
			},
		})
		if err != nil {
			emitDisabled()
			return
		}
		runtimeState.setUDP(manager)
		for _, peer := range knownPeers {
			manager.AddPeer(peer)
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			closeManager()
			return
		case peer := <-udpPeerCh:
			knownPeers[peer.VirtualIP] = peer
			if manager != nil {
				manager.AddPeer(peer)
			}
		case config := <-udpConfigCh:
			latestConfig = &config
			startManager()
		case <-udpResetCh:
			closeManager()
			latestConfig = nil
			knownPeers = make(map[string]overlay.UDPPeerCandidate)
		case <-udpDisableCh:
			closeManager()
			latestConfig = nil
			knownPeers = make(map[string]overlay.UDPPeerCandidate)
			udpDisabledForRun = true
			emitDisabled()
		case <-ticker.C:
			startManager()
		}
	}
}

func tunToOverlay(ctx context.Context, transport *tcpx.Client, t *tun.Tunnel, runtimeState *connectionRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		case pkt, ok := <-t.ReadChan():
			if !ok {
				return
			}
			// Drop packets while tcpx is reconnecting; the game can retransmit,
			// and this avoids writing traffic before the session is authenticated.
			if !runtimeState.isSecureReady() {
				continue
			}
			payload := runtimeState.nextIPPayload(pkt)
			_, dst, err := overlay.IPv4SrcDst(pkt)
			if err == nil {
				if manager := runtimeState.udpManager(); manager != nil {
					switch manager.SendToPeer(dst, payload) {
					case udpSendDirect:
						continue
					case udpSendMirror:
					case udpSendRelay:
					}
				}
			}
			if err := transport.Write(overlay.PackFrame(overlay.FrameIP, payload)); err != nil {
				return
			}
		}
	}
}

type recentPacketIDs struct {
	limit int
	seen  map[uint64]struct{}
	order []uint64
}

func newRecentPacketIDs(limit int) *recentPacketIDs {
	if limit <= 0 {
		limit = 4096
	}
	return &recentPacketIDs{
		limit: limit,
		seen:  make(map[uint64]struct{}, limit),
	}
}

func (r *recentPacketIDs) Accept(id uint64) bool {
	if r == nil {
		return true
	}
	if _, ok := r.seen[id]; ok {
		return false
	}
	r.seen[id] = struct{}{}
	r.order = append(r.order, id)
	for len(r.order) > r.limit {
		delete(r.seen, r.order[0])
		copy(r.order, r.order[1:])
		r.order = r.order[:len(r.order)-1]
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func randomUint64() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(b[:])
}

func pushErr(ch chan error, err error) {
	select {
	case ch <- err:
	default:
	}
}
