package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/tun"
)

const (
	defaultAPIBaseURL = "https://www.lwylink.xyz"
	defaultIfName     = "LwyLink"
)

type outputWriter struct {
	mu sync.Mutex
}

type netEvent struct {
	Type             string `json:"type"`
	Message          string `json:"message,omitempty"`
	RoomID           int64  `json:"roomID,omitempty"`
	RoomName         string `json:"roomName,omitempty"`
	NetworkCIDR      string `json:"networkCIDR,omitempty"`
	OverlayAddr      string `json:"overlayAddr,omitempty"`
	UDPOverlayAddr   string `json:"udpOverlayAddr,omitempty"`
	IP               string `json:"ip,omitempty"`
	Gateway          string `json:"gateway,omitempty"`
	MTU              int    `json:"mtu,omitempty"`
	TransportMode    string `json:"transportMode,omitempty"`
	TransportMessage string `json:"transportMessage,omitempty"`
	DirectPeers      int    `json:"directPeers,omitempty"`
	Time             string `json:"time"`
}

type connectConfig struct {
	apiBaseURL      string
	token           string
	roomID          int64
	ifName          string
	p2p             bool
	userID          int64
	roomName        string
	roomPublicKey   string
	networkCIDR     string
	overlayAddr     string
	udpOverlayAddr  string
	serverPublicKey string
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	writer := &outputWriter{}
	var err error
	switch os.Args[1] {
	case "connect":
		err = runConnect(os.Args[2:], writer)
	case "cleanup":
		err = runCleanup(os.Args[2:], writer)
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		writer.emit(netEvent{Type: "error", Message: friendlyError(err)})
		os.Exit(1)
	}
}

func runConnect(args []string, writer *outputWriter) error {
	cfg, err := parseConnectFlags(args)
	if err != nil {
		return err
	}
	if cfg.token == "" {
		return fmt.Errorf("missing token: pass --token or set LWYLINK_TOKEN")
	}
	if cfg.ifName == "" {
		cfg.ifName = defaultIfName
	}

	writer.emit(netEvent{Type: "preparing", Message: "正在准备 Wintun 组件"})
	if err := tun.EnsureWintunDLL(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	userID, selected, err := resolveRoom(ctx, cfg)
	if err != nil {
		return err
	}
	writer.emit(netEvent{
		Type:           "connecting",
		Message:        "正在连接虚拟网络",
		RoomID:         selected.ID,
		RoomName:       selected.Name,
		NetworkCIDR:    selected.NetworkCIDR,
		OverlayAddr:    selected.NodeOverlayAddr,
		UDPOverlayAddr: selected.NodeUDPAddr,
	})

	err = client.ConnectRoom(ctx, client.ConnectOptions{
		OverlayAddr:     selected.NodeOverlayAddr,
		UDPOverlayAddr:  selected.NodeUDPAddr,
		ServerPublicKey: selected.NodePublicKey,
		IfName:          cfg.ifName,
		Token:           cfg.token,
		UserID:          userID,
		Room:            selected,
		P2PEnabled: func() bool {
			return cfg.p2p
		},
		OnConnected: func(lease overlay.DHCPResponse) {
			writer.emit(netEvent{
				Type:           "connected",
				Message:        "虚拟网络已连接",
				RoomID:         selected.ID,
				RoomName:       selected.Name,
				NetworkCIDR:    selected.NetworkCIDR,
				OverlayAddr:    selected.NodeOverlayAddr,
				UDPOverlayAddr: selected.NodeUDPAddr,
				IP:             lease.IP,
				Gateway:        lease.Gateway,
				MTU:            lease.MTU,
			})
		},
		OnTransportState: func(status client.TransportStatus) {
			writer.emit(netEvent{
				Type:             "transport",
				TransportMode:    status.Mode,
				TransportMessage: status.Message,
				DirectPeers:      len(status.DirectIPs),
			})
		},
	})
	if errors.Is(err, context.Canceled) {
		writer.emit(netEvent{Type: "disconnected", Message: "虚拟网络已断开"})
		return nil
	}
	return err
}

func parseConnectFlags(args []string) (connectConfig, error) {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := connectConfig{
		apiBaseURL: envString("LWYLINK_API", defaultAPIBaseURL),
		token:      envString("LWYLINK_TOKEN", ""),
		roomID:     envInt64("LWYLINK_ROOM_ID", 0),
		ifName:     envString("LWYLINK_IF", defaultIfName),
		p2p:        envBool("LWYLINK_P2P", true),
		userID:     envInt64("LWYLINK_USER_ID", 0),
	}
	fs.StringVar(&cfg.apiBaseURL, "api", cfg.apiBaseURL, "control plane HTTP API base URL")
	fs.StringVar(&cfg.token, "token", cfg.token, "login token; prefer LWYLINK_TOKEN for production")
	fs.Int64Var(&cfg.roomID, "room-id", cfg.roomID, "room ID to connect; defaults to the current/first joined room")
	fs.StringVar(&cfg.ifName, "if", cfg.ifName, "Windows Wintun adapter name")
	fs.BoolVar(&cfg.p2p, "p2p", cfg.p2p, "enable UDP direct path when the node provides it")
	fs.Int64Var(&cfg.userID, "user-id", cfg.userID, "direct mode user ID")
	fs.StringVar(&cfg.roomName, "room-name", envString("LWYLINK_ROOM_NAME", ""), "direct mode room name")
	fs.StringVar(&cfg.roomPublicKey, "room-public-key", envString("LWYLINK_ROOM_PUBLIC_KEY", ""), "direct mode room public key")
	fs.StringVar(&cfg.networkCIDR, "network-cidr", envString("LWYLINK_NETWORK_CIDR", ""), "direct mode room network CIDR")
	fs.StringVar(&cfg.overlayAddr, "overlay-addr", envString("LWYLINK_OVERLAY_ADDR", ""), "direct mode overlay address")
	fs.StringVar(&cfg.udpOverlayAddr, "udp-overlay-addr", envString("LWYLINK_UDP_OVERLAY_ADDR", ""), "direct mode UDP rendezvous address; defaults to overlay address")
	fs.StringVar(&cfg.serverPublicKey, "server-public-key", envString("LWYLINK_SERVER_PUBLIC_KEY", ""), "direct mode overlay server public key")
	if err := fs.Parse(args); err != nil {
		return connectConfig{}, err
	}
	cfg.apiBaseURL = strings.TrimRight(strings.TrimSpace(cfg.apiBaseURL), "/")
	cfg.token = strings.TrimSpace(cfg.token)
	cfg.ifName = strings.TrimSpace(cfg.ifName)
	return cfg, nil
}

func resolveRoom(ctx context.Context, cfg connectConfig) (int64, room.Room, error) {
	if cfg.userID > 0 || cfg.roomPublicKey != "" || cfg.networkCIDR != "" || cfg.overlayAddr != "" || cfg.serverPublicKey != "" {
		if cfg.userID <= 0 || cfg.roomID <= 0 || cfg.roomPublicKey == "" || cfg.networkCIDR == "" || cfg.overlayAddr == "" || cfg.serverPublicKey == "" {
			return 0, room.Room{}, fmt.Errorf("direct mode requires --user-id, --room-id, --room-public-key, --network-cidr, --overlay-addr and --server-public-key")
		}
		name := strings.TrimSpace(cfg.roomName)
		if name == "" {
			name = fmt.Sprintf("room-%d", cfg.roomID)
		}
		return cfg.userID, room.Room{
			ID:              cfg.roomID,
			Name:            name,
			RoomPublicKey:   cfg.roomPublicKey,
			NetworkCIDR:     cfg.networkCIDR,
			NodeOverlayAddr: cfg.overlayAddr,
			NodeUDPAddr:     cfg.udpOverlayAddr,
			NodePublicKey:   cfg.serverPublicKey,
		}, nil
	}

	if cfg.apiBaseURL == "" {
		return 0, room.Room{}, fmt.Errorf("missing --api for API mode")
	}
	api := client.NewAPI(cfg.apiBaseURL)
	api.SetToken(cfg.token)
	user, err := api.Me(ctx)
	if err != nil {
		return 0, room.Room{}, fmt.Errorf("load current user: %w", err)
	}
	rooms, err := api.Rooms(ctx)
	if err != nil {
		return 0, room.Room{}, fmt.Errorf("load joined rooms: %w", err)
	}
	if len(rooms) == 0 {
		return 0, room.Room{}, fmt.Errorf("current user has not joined a room")
	}
	if cfg.roomID <= 0 {
		return user.ID, rooms[0], nil
	}
	for _, item := range rooms {
		if item.ID == cfg.roomID {
			return user.ID, item, nil
		}
	}
	return 0, room.Room{}, fmt.Errorf("room %d is not in current user's room list", cfg.roomID)
}

func runCleanup(args []string, writer *outputWriter) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	ifName := fs.String("if", envString("LWYLINK_IF", defaultIfName), "Windows Wintun adapter name")
	cidr := fs.String("cidr", envString("LWYLINK_NETWORK_CIDR", ""), "room network CIDR to remove routes for")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*cidr) != "" {
		if err := tun.CleanupRoute(strings.TrimSpace(*ifName), strings.TrimSpace(*cidr)); err != nil {
			return err
		}
	}
	if err := tun.EnableFirewallProfiles(); err != nil {
		return err
	}
	writer.emit(netEvent{Type: "cleanup", Message: "网络清理完成"})
	return nil
}

func (w *outputWriter) emit(event netEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	event.Time = time.Now().Format(time.RFC3339)
	_ = json.NewEncoder(os.Stdout).Encode(event)
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func friendlyError(err error) string {
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "access is denied") || strings.Contains(lower, "拒绝访问"):
		return "启动虚拟网卡需要管理员权限，请用管理员身份运行"
	case strings.Contains(lower, "wintun client is only implemented"):
		return "虚拟网卡客户端只能在 Windows 上运行"
	case strings.Contains(lower, "release wintun.dll"):
		return "释放 Wintun 组件失败，请确认 exe 所在目录可写，并用管理员身份运行"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused") || strings.Contains(lower, "connectex"):
		return "无法连接房间节点，请检查节点地址和端口"
	case strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "timed out"):
		return "连接房间节点超时，请检查网络连通性"
	}
	return text
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `LwyLink virtual network engine

Usage:
  lwylink-net.exe connect [options]
  lwylink-net.exe cleanup [options]

Connect with API lookup:
  set LWYLINK_TOKEN=<login-token>
  lwylink-net.exe connect --api https://www.lwylink.xyz --room-id 123 --if LwyLink

Direct connect without HTTP room lookup:
  set LWYLINK_TOKEN=<login-token>
  lwylink-net.exe connect --user-id 1 --room-id 123 --room-public-key <key> --network-cidr 172.16.1.0/24 --overlay-addr host:35768 --udp-overlay-addr host:35768 --server-public-key <key>

Cleanup routes/firewall:
  lwylink-net.exe cleanup --if LwyLink --cidr 172.16.1.0/24

P2P is enabled by default. Pass --p2p=false or set LWYLINK_P2P=false to force TCP relay.
Status is written to stdout as one JSON object per line.`)
}
