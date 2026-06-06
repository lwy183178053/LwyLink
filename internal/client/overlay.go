package client

import (
	"context"
	"crypto/rand"
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
	OverlayAddr     string
	ServerPublicKey string
	IfName          string
	Token           string
	Room            room.Room
	OnConnected     func(overlay.DHCPResponse)
}

func ConnectRoom(ctx context.Context, opts ConnectOptions) error {
	if opts.Token == "" {
		return fmt.Errorf("not logged in")
	}
	if opts.Room.ID <= 0 {
		return fmt.Errorf("bad room")
	}
	if opts.IfName == "" {
		opts.IfName = "LwyLink"
	}
	if opts.OverlayAddr == "" {
		opts.OverlayAddr = opts.Room.NodeOverlayAddr
	}
	if opts.ServerPublicKey == "" {
		opts.ServerPublicKey = opts.Room.NodePublicKey
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

	var (
		mu          sync.RWMutex
		secureReady bool
		tunnel      *tun.Tunnel
		dhcpCh      = make(chan overlay.DHCPResponse, 1)
		errCh       = make(chan error, 1)
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
			mu.Lock()
			secureReady = false
			mu.Unlock()
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
				mu.Lock()
				secureReady = true
				mu.Unlock()
				select {
				case dhcpCh <- lease:
				default:
				}
			case overlay.FrameIP:
				mu.RLock()
				t := tunnel
				mu.RUnlock()
				if t != nil {
					_ = t.Write(payload)
				}
			case overlay.FrameError:
				var e map[string]string
				_ = json.Unmarshal(payload, &e)
				if e["error"] == "" {
					e["error"] = string(payload)
				}
				pushErr(errCh, fmt.Errorf("overlay error: %s", e["error"]))
				_ = conn.Close()
			case overlay.FramePing:
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
	mu.Lock()
	tunnel = t
	mu.Unlock()

	// Route configuration happens only after the server has authenticated this
	// token and assigned a lease. On non-Windows builds this returns a stub error.
	if err := tun.ConfigureAddressAndRoute(opts.IfName, lease.IP, lease.SubnetMask, opts.Room.NetworkCIDR, lease.Gateway); err != nil {
		return err
	}
	if opts.OnConnected != nil {
		opts.OnConnected(lease)
	}

	log.Printf("虚拟网络已连接: room=%s ip=%s cidr=%s if=%s", opts.Room.Name, lease.IP, opts.Room.NetworkCIDR, opts.IfName)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tunToOverlay(childCtx, transport, t, &mu, &secureReady)
		cancel()
	}()
	go func() {
		defer wg.Done()
		<-childCtx.Done()
		cancel()
	}()
	<-childCtx.Done()
	_ = transport.Close()
	_ = t.Close()
	wg.Wait()
	return nil
}

func tunToOverlay(ctx context.Context, transport *tcpx.Client, t *tun.Tunnel, mu *sync.RWMutex, secureReady *bool) {
	for {
		select {
		case <-ctx.Done():
			return
		case pkt, ok := <-t.ReadChan():
			if !ok {
				return
			}
			mu.RLock()
			ready := *secureReady
			mu.RUnlock()
			// Drop packets while tcpx is reconnecting; the game can retransmit,
			// and this avoids writing traffic before the session is authenticated.
			if !ready {
				continue
			}
			if err := transport.Write(overlay.PackFrame(overlay.FrameIP, pkt)); err != nil {
				return
			}
		}
	}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func pushErr(ch chan error, err error) {
	select {
	case ch <- err:
	default:
	}
}
