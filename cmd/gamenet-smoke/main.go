package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/client"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/overlay"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/room"
	"github.com/LwyV-Labs/LwyV-GameNet/internal/tcpx"
)

func main() {
	apiURL := flag.String("api", "http://127.0.0.1:8080", "HTTP API base URL")
	overlayAddr := flag.String("overlay", "127.0.0.1:35768", "Overlay address")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(*apiURL)
	owner, err := ownerAPI.Register(ctx, "owner"+suffix, "smoke-password", "Smoke Owner")
	must("register owner", err)
	log.Printf("registered owner user=%s", owner.User.Username)

	dupAPI := client.NewAPI(*apiURL)
	if _, dupErr := dupAPI.Register(ctx, owner.User.Username, "smoke-password", "Duplicate Owner"); dupErr == nil {
		log.Fatalf("expected duplicate username registration to fail")
	} else {
		log.Printf("duplicate username rejected as expected: %v", dupErr)
	}

	ownerSecondAPI := client.NewAPI(*apiURL)
	_, err = ownerSecondAPI.Login(ctx, owner.User.Username, "smoke-password")
	must("second login owner", err)
	if _, staleErr := ownerAPI.Rooms(ctx); staleErr == nil {
		log.Fatalf("expected first owner token to be rejected after second login")
	} else {
		log.Printf("single-login rule rejected old owner token as expected: %v", staleErr)
	}
	ownerAPI = ownerSecondAPI

	roomInfo, err := ownerAPI.CreateRoom(ctx, "Smoke Room "+suffix, 2)
	must("create room", err)
	log.Printf("created room id=%d publicKey=%s cidr=%s maxMembers=%d", roomInfo.ID, roomInfo.RoomPublicKey, roomInfo.NetworkCIDR, roomInfo.MaxMembers)

	playerAPI := client.NewAPI(*apiURL)
	player, err := playerAPI.Register(ctx, "player"+suffix, "smoke-password", "Smoke Player")
	must("register player", err)
	log.Printf("registered player user=%s", player.User.Username)

	joined, err := playerAPI.JoinRoom(ctx, roomInfo.RoomPublicKey)
	must("join room with public key", err)
	log.Printf("joined room id=%d", joined.ID)
	if _, createErr := playerAPI.CreateRoom(ctx, "Should Fail "+suffix, 2); createErr == nil {
		log.Fatalf("expected joined player to be unable to create another room")
	} else {
		log.Printf("single-room rule rejected joined player create as expected: %v", createErr)
	}

	lateAPI := client.NewAPI(*apiURL)
	_, err = lateAPI.Register(ctx, "late"+suffix, "smoke-password", "Late Player")
	must("register late player", err)
	if _, joinErr := lateAPI.JoinRoom(ctx, roomInfo.RoomPublicKey); joinErr == nil {
		log.Fatalf("expected full room to reject late player, but join succeeded")
	} else {
		log.Printf("full room rejected late player as expected: %v", joinErr)
	}

	members, err := ownerAPI.RoomMembers(ctx, roomInfo.ID)
	must("list room members", err)
	if len(members) != 2 {
		log.Fatalf("expected 2 members, got %d", len(members))
	}
	log.Printf("room members ok count=%d owner=%s member=%s", len(members), members[0].Username, members[1].Username)

	verifyRoomChatEvents(ctx, ownerAPI, playerAPI, roomInfo.ID)

	if joined.NodePublicKey == "" {
		log.Fatalf("joined room has no node public key")
	}
	ownerOverlay := mustOverlayConnect(ctx, *overlayAddr, joined.NodePublicKey, ownerAPI.Token(), roomInfo.ID, roomInfo.RoomPublicKey)
	defer ownerOverlay.close()
	playerOverlay := mustOverlayConnect(ctx, *overlayAddr, joined.NodePublicKey, playerAPI.Token(), joined.ID, joined.RoomPublicKey)
	defer playerOverlay.close()
	log.Printf("overlay dhcp ok ownerIP=%s playerIP=%s gateway=%s mtu=%d dns=%v", ownerOverlay.lease.IP, playerOverlay.lease.IP, playerOverlay.lease.Gateway, playerOverlay.lease.MTU, playerOverlay.lease.DNS)
	waitForMemberPresence(ctx, ownerAPI, roomInfo.ID, owner.User.ID, true, ownerOverlay.lease.IP)
	waitForMemberPresence(ctx, ownerAPI, roomInfo.ID, player.User.ID, true, playerOverlay.lease.IP)

	verifyDiscoveryFanout(ctx, playerOverlay, ownerOverlay)

	playerOverlay.close()
	waitForMemberPresence(ctx, ownerAPI, roomInfo.ID, player.User.ID, false, "")

	_, err = ownerAPI.TransferOwner(ctx, roomInfo.ID, player.User.ID)
	must("transfer room owner", err)
	log.Printf("transferred owner to user=%s", player.User.Username)

	must("kick old owner", playerAPI.KickMember(ctx, roomInfo.ID, owner.User.ID))
	log.Printf("new owner kicked old owner as expected")

	must("delete room", playerAPI.DeleteRoom(ctx, roomInfo.ID))
	log.Printf("deleted room id=%d", roomInfo.ID)

	if _, joinErr := lateAPI.JoinRoom(ctx, roomInfo.RoomPublicKey); joinErr == nil {
		log.Fatalf("expected deleted room public key to fail, but join succeeded")
	} else {
		log.Printf("deleted room public key rejected as expected: %v", joinErr)
	}

	verifyRoomAppHeartbeat(ctx, *apiURL)
	verifyKickKeepsOwnerRoom(ctx, *apiURL)
	verifyKickDisconnectsOverlayAndSSE(ctx, *apiURL, *overlayAddr)
	verifyEmptyRoomCleanup(ctx, *apiURL)
	log.Printf("SMOKE OK")
}

func must(label string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}

func waitForMemberPresence(ctx context.Context, api *client.API, roomID, userID int64, wantOnline bool, wantIP string) {
	waitCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for {
		members, err := api.RoomMembers(waitCtx, roomID)
		if err == nil {
			for _, member := range members {
				if member.UserID != userID {
					continue
				}
				last = fmt.Sprintf("online=%v ip=%s", member.Online, member.VirtualIP)
				if member.Online == wantOnline && (wantIP == "" || member.VirtualIP == wantIP) {
					log.Printf("presence ok user=%d online=%v ip=%s", userID, member.Online, member.VirtualIP)
					return
				}
			}
		} else {
			last = err.Error()
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			log.Fatalf("presence user=%d expected online=%v ip=%s, last=%s", userID, wantOnline, wantIP, last)
		}
	}
}

func verifyRoomChatEvents(ctx context.Context, senderAPI, receiverAPI *client.API, roomID int64) {
	eventCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()

	connected := make(chan struct{}, 1)
	events := make(chan room.RoomEvent, 8)
	go func() {
		_ = receiverAPI.SubscribeRoomEvents(eventCtx, func(event room.RoomEvent) {
			if event.Type == room.RoomEventConnected {
				select {
				case connected <- struct{}{}:
				default:
				}
				return
			}
			if event.RoomID == roomID {
				select {
				case events <- event:
				default:
				}
			}
		})
	}()

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		log.Fatalf("room chat event stream did not connect")
	case <-ctx.Done():
		log.Fatalf("room chat event stream: %v", ctx.Err())
	}

	message := "smoke chat " + fmt.Sprintf("%d", time.Now().UnixNano())
	chat, err := senderAPI.SendRoomChat(ctx, roomID, message)
	must("send room chat", err)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type != room.RoomEventChatMessage || event.Chat == nil {
				continue
			}
			if event.Chat.ID != chat.ID || event.Chat.Message != message {
				log.Fatalf("chat event mismatch id=%s message=%q", event.Chat.ID, event.Chat.Message)
			}
			log.Printf("room chat event ok id=%s", chat.ID)
			return
		case <-deadline:
			log.Fatalf("room chat event was not delivered")
		case <-ctx.Done():
			log.Fatalf("room chat event: %v", ctx.Err())
		}
	}
}

func verifyDiscoveryFanout(ctx context.Context, from, to *overlaySmokeClient) {
	tests := []struct {
		name string
		dst  string
	}{
		{name: "limited broadcast", dst: "255.255.255.255"},
		{name: "subnet broadcast", dst: subnetBroadcast(from.lease.IP, from.lease.SubnetMask)},
		{name: "multicast", dst: "239.255.0.1"},
	}
	for _, tt := range tests {
		pkt := ipv4Packet(from.lease.IP, tt.dst)
		if err := from.client.Write(overlay.PackFrame(overlay.FrameIP, pkt)); err != nil {
			log.Fatalf("send discovery %s: %v", tt.name, err)
		}
		select {
		case <-to.ipFrames:
			log.Printf("overlay discovery %s forwarded within room dst=%s", tt.name, tt.dst)
		case <-time.After(3 * time.Second):
			log.Fatalf("overlay discovery %s was not forwarded dst=%s", tt.name, tt.dst)
		case <-ctx.Done():
			log.Fatalf("overlay discovery %s: %v", tt.name, ctx.Err())
		}
	}
}

func verifyEmptyRoomCleanup(ctx context.Context, apiURL string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	api := client.NewAPI(apiURL)
	_, err := api.Register(ctx, "cleanup"+suffix, "smoke-password", "Cleanup Smoke")
	must("register cleanup user", err)
	roomInfo, err := api.CreateRoom(ctx, "Cleanup Room "+suffix, 2)
	must("create cleanup room", err)
	log.Printf("created cleanup room id=%d; waiting without app heartbeat", roomInfo.ID)

	waitCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	start := time.Now()
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-waitCtx.Done():
		log.Fatalf("empty room cleanup wait failed room id=%d: %v", roomInfo.ID, waitCtx.Err())
	}
	rooms, err := api.Rooms(waitCtx)
	must("check cleanup room list", err)
	if len(rooms) != 0 {
		log.Fatalf("empty room cleanup did not remove room id=%d after %.0fs", roomInfo.ID, time.Since(start).Seconds())
	}
	log.Printf("empty room cleanup removed room id=%d after %.0fs", roomInfo.ID, time.Since(start).Seconds())
}

func verifyRoomAppHeartbeat(ctx context.Context, apiURL string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	api := client.NewAPI(apiURL)
	_, err := api.Register(ctx, "active"+suffix, "smoke-password", "Active Room Smoke")
	must("register active user", err)
	roomInfo, err := api.CreateRoom(ctx, "Active Room "+suffix, 2)
	must("create active room", err)
	log.Printf("created active room id=%d; keeping it alive with app heartbeat", roomInfo.ID)

	waitCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.After(28 * time.Second)
	for {
		select {
		case <-ticker.C:
			rooms, err := api.Rooms(waitCtx)
			must("heartbeat active room", err)
			if len(rooms) != 1 || rooms[0].ID != roomInfo.ID {
				log.Fatalf("active app heartbeat room disappeared, got %d room(s)", len(rooms))
			}
		case <-deadline:
			must("delete active heartbeat room", api.DeleteRoom(waitCtx, roomInfo.ID))
			log.Printf("app heartbeat kept room id=%d alive without overlay", roomInfo.ID)
			return
		case <-waitCtx.Done():
			log.Fatalf("active app heartbeat timed out: %v", waitCtx.Err())
		}
	}
}

func verifyKickKeepsOwnerRoom(ctx context.Context, apiURL string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(apiURL)
	owner, err := ownerAPI.Register(ctx, "kickowner"+suffix, "smoke-password", "Kick Owner Smoke")
	must("register kick owner", err)
	roomInfo, err := ownerAPI.CreateRoom(ctx, "Kick Room "+suffix, 2)
	must("create kick room", err)

	playerAPI := client.NewAPI(apiURL)
	player, err := playerAPI.Register(ctx, "kickplayer"+suffix, "smoke-password", "Kick Player Smoke")
	must("register kick player", err)
	_, err = playerAPI.JoinRoom(ctx, roomInfo.RoomPublicKey)
	must("join kick room", err)
	must("kick player without overlay", ownerAPI.KickMember(ctx, roomInfo.ID, player.User.ID))

	waitCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.After(18 * time.Second)
	for {
		select {
		case <-ticker.C:
			rooms, err := ownerAPI.Rooms(waitCtx)
			must("refresh kick owner room", err)
			if len(rooms) != 1 || rooms[0].ID != roomInfo.ID {
				log.Fatalf("kick removed owner room unexpectedly, owner=%d got %d room(s)", owner.User.ID, len(rooms))
			}
			members, err := ownerAPI.RoomMembers(waitCtx, roomInfo.ID)
			must("list kick room members", err)
			if len(members) != 1 || members[0].UserID != owner.User.ID {
				log.Fatalf("kick room members mismatch after kick: got %d member(s)", len(members))
			}
		case <-deadline:
			must("delete kick room", ownerAPI.DeleteRoom(waitCtx, roomInfo.ID))
			log.Printf("kick kept owner room id=%d alive without overlay", roomInfo.ID)
			return
		case <-waitCtx.Done():
			log.Fatalf("kick owner room check timed out: %v", waitCtx.Err())
		}
	}
}

func verifyKickDisconnectsOverlayAndSSE(ctx context.Context, apiURL, overlayAddr string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(apiURL)
	_, err := ownerAPI.Register(ctx, "eventowner"+suffix, "smoke-password", "Event Owner Smoke")
	must("register event owner", err)
	roomInfo, err := ownerAPI.CreateRoom(ctx, "Event Room "+suffix, 2)
	must("create event room", err)

	playerAPI := client.NewAPI(apiURL)
	player, err := playerAPI.Register(ctx, "eventplayer"+suffix, "smoke-password", "Event Player Smoke")
	must("register event player", err)
	joined, err := playerAPI.JoinRoom(ctx, roomInfo.RoomPublicKey)
	must("join event room", err)

	eventCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	playerEvents := make(chan room.RoomEvent, 8)
	go func() {
		_ = playerAPI.SubscribeRoomEvents(eventCtx, func(event room.RoomEvent) {
			if event.RoomID == roomInfo.ID {
				select {
				case playerEvents <- event:
				default:
				}
			}
		})
	}()

	playerOverlay := mustOverlayConnect(ctx, overlayAddr, joined.NodePublicKey, playerAPI.Token(), joined.ID, joined.RoomPublicKey)
	defer playerOverlay.close()
	must("kick event player", ownerAPI.KickMember(ctx, roomInfo.ID, player.User.ID))

	waitCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	gotKickEvent := false
	gotOverlayClose := false
	for !gotKickEvent || !gotOverlayClose {
		select {
		case event := <-playerEvents:
			if event.Type == room.RoomEventMemberKicked && event.TargetUserID == player.User.ID {
				gotKickEvent = true
				log.Printf("sse kick event received for user=%s", player.User.Username)
			}
		case <-playerOverlay.done:
			gotOverlayClose = true
			log.Printf("overlay was actively closed after kick user=%s", player.User.Username)
		case <-waitCtx.Done():
			log.Fatalf("kick event/overlay close timeout kickEvent=%v overlayClose=%v", gotKickEvent, gotOverlayClose)
		}
	}
	must("delete event room", ownerAPI.DeleteRoom(ctx, roomInfo.ID))
}

func subnetBroadcast(ip, mask string) string {
	ip4 := net.ParseIP(ip).To4()
	mask4 := net.ParseIP(mask).To4()
	if ip4 == nil || mask4 == nil {
		log.Fatalf("bad lease ip/mask ip=%s mask=%s", ip, mask)
	}
	return net.IPv4(
		ip4[0]|^mask4[0],
		ip4[1]|^mask4[1],
		ip4[2]|^mask4[2],
		ip4[3]|^mask4[3],
	).String()
}

type overlaySmokeClient struct {
	client   *tcpx.Client
	lease    overlay.DHCPResponse
	ipFrames chan []byte
	done     chan error
	cancel   context.CancelFunc
}

func (c *overlaySmokeClient) close() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.client != nil {
		_ = c.client.Close()
	}
}

func mustOverlayConnect(ctx context.Context, addr, serverPublicKey, token string, roomID int64, roomPublicKey string) *overlaySmokeClient {
	serverPubHex, err := tcpx.NormalizePublicKeyHex(serverPublicKey)
	must("normalize overlay public key", err)
	requestID := requestID()
	dhcpCh := make(chan overlay.DHCPResponse, 1)
	ipFrames := make(chan []byte, 8)
	errCh := make(chan error, 1)

	c, err := tcpx.NewClient(tcpx.ClientConfig{
		Addr:               addr,
		ServerPublicKeyHex: serverPubHex,
		BaseConfig: tcpx.BaseConfig{
			ReadTimeout:       20 * time.Second,
			WriteTimeout:      8 * time.Second,
			HeartbeatBase:     4 * time.Second,
			HeartbeatJitter:   time.Second,
			KeyRotateInterval: 45 * time.Second,
			OldKeyGrace:       20 * time.Second,
		},
		OnConnect: func(conn *tcpx.SecureConn) {
			raw, err := overlay.PackJSON(overlay.FrameAuth, overlay.AuthRequest{
				Token:         token,
				RoomID:        roomID,
				RoomPublicKey: roomPublicKey,
				RequestID:     requestID,
			})
			if err != nil {
				pushErr(errCh, err)
				_ = conn.Close()
				return
			}
			if err := conn.Write(raw); err != nil {
				pushErr(errCh, err)
				_ = conn.Close()
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
					pushErr(errCh, fmt.Errorf("dhcp request id mismatch"))
					_ = conn.Close()
					return
				}
				select {
				case dhcpCh <- lease:
				default:
				}
			case overlay.FrameIP:
				frame := make([]byte, len(payload))
				copy(frame, payload)
				select {
				case ipFrames <- frame:
				default:
				}
			case overlay.FrameError:
				var e map[string]string
				_ = json.Unmarshal(payload, &e)
				pushErr(errCh, fmt.Errorf("overlay error: %s", e["error"]))
				_ = conn.Close()
			}
		},
	})
	must("new tcpx client", err)

	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		err := c.Run(runCtx)
		if runCtx.Err() == nil {
			pushErr(errCh, err)
		}
	}()

	select {
	case lease := <-dhcpCh:
		return &overlaySmokeClient{
			client:   c,
			lease:    lease,
			ipFrames: ipFrames,
			done:     errCh,
			cancel:   cancel,
		}
	case err := <-errCh:
		cancel()
		_ = c.Close()
		log.Fatalf("overlay connect: %v", err)
	case <-ctx.Done():
		cancel()
		_ = c.Close()
		log.Fatalf("overlay connect: %v", ctx.Err())
	}
	return nil
}

func ipv4Packet(src, dst string) []byte {
	src4 := net.ParseIP(src).To4()
	dst4 := net.ParseIP(dst).To4()
	if src4 == nil || dst4 == nil {
		log.Fatalf("bad smoke ipv4 packet src=%s dst=%s", src, dst)
	}
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	pkt[2] = 0
	pkt[3] = 20
	pkt[8] = 64
	pkt[9] = 17
	copy(pkt[12:16], src4)
	copy(pkt[16:20], dst4)
	return pkt
}

func requestID() string {
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
