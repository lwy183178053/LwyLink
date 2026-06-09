package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/netutil"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
)

func main() {
	os.Exit(run())
}

func run() (code int) {
	apiURL := flag.String("api", "http://127.0.0.1:8080", "HTTP API base URL")
	overlayAddr := flag.String("overlay", "127.0.0.1:35768", "Overlay address")
	adminToken := flag.String("admin-token", os.Getenv("LWYLINK_ADMIN_TOKEN"), "Admin token used to delete smoke users after the run")
	flag.Parse()

	cleaner := newSmokeCleaner(*apiURL, *adminToken)
	defer func() {
		if recovered := recover(); recovered != nil {
			code = 1
			switch value := recovered.(type) {
			case smokeFailure:
				log.Printf("SMOKE FAILED: %s", value.Message)
			default:
				log.Printf("SMOKE PANIC: %v", recovered)
			}
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancelCleanup()
		cleaner.cleanup(cleanupCtx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(*apiURL)
	owner, err := registerSmokeUser(ctx, cleaner, ownerAPI, "owner"+suffix, "smoke-password", "Smoke Owner")
	must("register owner", err)
	log.Printf("registered owner user=%s", owner.User.Username)

	dupAPI := client.NewAPI(*apiURL)
	if _, dupErr := registerSmokeUser(ctx, cleaner, dupAPI, owner.User.Username, "smoke-password", "Duplicate Owner"); dupErr == nil {
		failf("expected duplicate username registration to fail")
	} else {
		log.Printf("duplicate username rejected as expected: %v", dupErr)
	}

	ownerSecondAPI := client.NewAPI(*apiURL)
	_, err = ownerSecondAPI.Login(ctx, owner.User.Username, "smoke-password")
	must("second login owner", err)
	if _, staleErr := ownerAPI.Rooms(ctx); staleErr == nil {
		failf("expected first owner token to be rejected after second login")
	} else {
		log.Printf("single-login rule rejected old owner token as expected: %v", staleErr)
	}
	ownerAPI = ownerSecondAPI

	roomInfo, err := ownerAPI.CreateRoom(ctx, "Smoke Room "+suffix, 2)
	must("create room", err)
	log.Printf("created room id=%d publicKey=%s cidr=%s maxMembers=%d", roomInfo.ID, roomInfo.RoomPublicKey, roomInfo.NetworkCIDR, roomInfo.MaxMembers)

	playerAPI := client.NewAPI(*apiURL)
	player, err := registerSmokeUser(ctx, cleaner, playerAPI, "player"+suffix, "smoke-password", "Smoke Player")
	must("register player", err)
	log.Printf("registered player user=%s", player.User.Username)

	joined, err := playerAPI.JoinRoom(ctx, roomInfo.RoomPublicKey)
	must("join room with public key", err)
	log.Printf("joined room id=%d", joined.ID)
	if _, createErr := playerAPI.CreateRoom(ctx, "Should Fail "+suffix, 2); createErr == nil {
		failf("expected joined player to be unable to create another room")
	} else {
		log.Printf("single-room rule rejected joined player create as expected: %v", createErr)
	}

	lateAPI := client.NewAPI(*apiURL)
	_, err = registerSmokeUser(ctx, cleaner, lateAPI, "late"+suffix, "smoke-password", "Late Player")
	must("register late player", err)
	if _, joinErr := lateAPI.JoinRoom(ctx, roomInfo.RoomPublicKey); joinErr == nil {
		failf("expected full room to reject late player, but join succeeded")
	} else {
		log.Printf("full room rejected late player as expected: %v", joinErr)
	}

	members, err := ownerAPI.RoomMembers(ctx, roomInfo.ID)
	must("list room members", err)
	if len(members) != 2 {
		failf("expected 2 members, got %d", len(members))
	}
	log.Printf("room members ok count=%d owner=%s member=%s", len(members), members[0].Username, members[1].Username)

	verifyRoomChatEvents(ctx, ownerAPI, playerAPI, roomInfo.ID)

	if joined.NodePublicKey == "" {
		failf("joined room has no node public key")
	}
	ownerOverlay := mustOverlayConnect(ctx, *overlayAddr, joined.NodePublicKey, ownerAPI.Token(), roomInfo.ID, roomInfo.RoomPublicKey)
	defer ownerOverlay.close()
	playerOverlay := mustOverlayConnect(ctx, *overlayAddr, joined.NodePublicKey, playerAPI.Token(), joined.ID, joined.RoomPublicKey)
	defer playerOverlay.close()
	log.Printf("overlay dhcp ok ownerIP=%s playerIP=%s gateway=%s mtu=%d dns=%v", ownerOverlay.lease.IP, playerOverlay.lease.IP, playerOverlay.lease.Gateway, playerOverlay.lease.MTU, playerOverlay.lease.DNS)
	waitForMemberPresence(ctx, ownerAPI, roomInfo.ID, owner.User.ID, true, ownerOverlay.lease.IP)
	waitForMemberPresence(ctx, ownerAPI, roomInfo.ID, player.User.ID, true, playerOverlay.lease.IP)

	verifyDiscoveryFanout(ctx, playerOverlay, ownerOverlay)
	verifyVoiceWebRTCSignal(ctx, roomInfo.ID, owner.User.ID, player.User.ID, ownerAPI, playerAPI)
	verifyUDPRendezvous(ctx, *overlayAddr, roomInfo.ID, owner.User.ID, player.User.ID, ownerOverlay, playerOverlay)

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
		failf("expected deleted room public key to fail, but join succeeded")
	} else {
		log.Printf("deleted room public key rejected as expected: %v", joinErr)
	}

	verifyRoomAppHeartbeat(ctx, cleaner, *apiURL)
	verifyKickKeepsOwnerRoom(ctx, cleaner, *apiURL)
	verifyKickDisconnectsOverlayAndSSE(ctx, cleaner, *apiURL, *overlayAddr)
	verifyEmptyRoomCleanup(ctx, cleaner, *apiURL)
	log.Printf("SMOKE OK")
	return code
}

func must(label string, err error) {
	if err != nil {
		failf("%s: %v", label, err)
	}
}

type smokeFailure struct {
	Message string
}

func failf(format string, args ...any) {
	panic(smokeFailure{Message: fmt.Sprintf(format, args...)})
}

func registerSmokeUser(ctx context.Context, cleaner *smokeCleaner, api *client.API, username, password, displayName string) (account.LoginResult, error) {
	email := smokeEmail(username)
	result, err := cleaner.register(ctx, api, email, password, displayName)
	if err == nil {
		cleaner.track(result.User.ID)
	}
	return result, err
}

func smokeEmail(seed string) string {
	seed = strings.TrimSpace(strings.ToLower(seed))
	if strings.Contains(seed, "@") {
		return seed
	}
	return seed + "@smoke.lwylink.test"
}

type smokeCleaner struct {
	apiURL     string
	adminToken string
	userIDs    []int64
}

func newSmokeCleaner(apiURL, adminToken string) *smokeCleaner {
	return &smokeCleaner{
		apiURL:     strings.TrimRight(strings.TrimSpace(apiURL), "/"),
		adminToken: strings.TrimSpace(adminToken),
	}
}

func (c *smokeCleaner) track(userID int64) {
	if c == nil || userID <= 0 {
		return
	}
	c.userIDs = append(c.userIDs, userID)
}

func (c *smokeCleaner) register(ctx context.Context, api *client.API, email, password, displayName string) (account.LoginResult, error) {
	if c == nil || c.adminToken == "" {
		return api.Register(ctx, email, password, displayName, "")
	}
	var result account.LoginResult
	err := postJSON(ctx, c.apiURL+"/api/smoke/register?token="+url.QueryEscape(c.adminToken), map[string]string{
		"email":       email,
		"password":    password,
		"displayName": displayName,
	}, &result)
	if err != nil {
		return account.LoginResult{}, err
	}
	api.SetToken(result.Token)
	return result, nil
}

func (c *smokeCleaner) cleanup(ctx context.Context) {
	if c == nil || len(c.userIDs) == 0 {
		return
	}
	if c.adminToken == "" {
		log.Printf("skip smoke user cleanup: admin token is not configured")
		return
	}
	for _, userID := range c.uniqueUserIDsDescending() {
		if err := c.deleteUser(ctx, userID); err != nil {
			log.Printf("cleanup smoke user id=%d failed: %v", userID, err)
			continue
		}
		log.Printf("cleanup smoke user id=%d ok", userID)
	}
}

func postJSON(ctx context.Context, endpoint string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *smokeCleaner) uniqueUserIDsDescending() []int64 {
	seen := make(map[int64]struct{}, len(c.userIDs))
	out := make([]int64, 0, len(c.userIDs))
	for i := len(c.userIDs) - 1; i >= 0; i-- {
		userID := c.userIDs[i]
		if userID <= 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		out = append(out, userID)
	}
	return out
}

func (c *smokeCleaner) deleteUser(ctx context.Context, userID int64) error {
	form := url.Values{}
	form.Set("token", c.adminToken)
	form.Set("userID", strconv.FormatInt(userID, 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/admin/users/delete", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return fmt.Errorf("admin delete http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
}

func waitForMemberPresence(ctx context.Context, api *client.API, roomID, userID int64, wantOnline bool, wantIP string) {
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for {
		requestCtx, cancelRequest := context.WithTimeout(waitCtx, 2*time.Second)
		members, err := api.RoomMembers(requestCtx, roomID)
		cancelRequest()
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
			failf("presence user=%d expected online=%v ip=%s, last=%s", userID, wantOnline, wantIP, last)
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
		failf("room chat event stream did not connect")
	case <-ctx.Done():
		failf("room chat event stream: %v", ctx.Err())
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
				failf("chat event mismatch id=%s message=%q", event.Chat.ID, event.Chat.Message)
			}
			log.Printf("room chat event ok id=%s", chat.ID)
			return
		case <-deadline:
			failf("room chat event was not delivered")
		case <-ctx.Done():
			failf("room chat event: %v", ctx.Err())
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
		if err := from.client.Write(overlay.PackFrame(overlay.FrameIP, overlay.PackIPPayload(uint64(time.Now().UnixNano()), pkt))); err != nil {
			failf("send discovery %s: %v", tt.name, err)
		}
		select {
		case payload := <-to.ipFrames:
			if _, got, err := overlay.UnpackIPPayload(payload); err != nil || len(got) == 0 {
				failf("overlay discovery %s returned bad payload: %v", tt.name, err)
			}
			log.Printf("overlay discovery %s forwarded within room dst=%s", tt.name, tt.dst)
		case <-time.After(3 * time.Second):
			failf("overlay discovery %s was not forwarded dst=%s", tt.name, tt.dst)
		case <-ctx.Done():
			failf("overlay discovery %s: %v", tt.name, ctx.Err())
		}
	}
}

func verifyVoiceWebRTCSignal(ctx context.Context, roomID, ownerUserID, playerUserID int64, ownerAPI, playerAPI *client.API) {
	eventsCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	events := make(chan room.RoomEvent, 8)
	go func() {
		_ = playerAPI.SubscribeRoomEvents(eventsCtx, func(event room.RoomEvent) {
			if event.Type == room.RoomEventVoiceSignal {
				select {
				case events <- event:
				default:
				}
			}
		})
	}()
	if _, err := ownerAPI.JoinRoomVoice(ctx, roomID); err != nil {
		failf("owner join voice for webrtc signal: %v", err)
	}
	if _, err := playerAPI.JoinRoomVoice(ctx, roomID); err != nil {
		failf("player join voice for webrtc signal: %v", err)
	}
	defer func() {
		_, _ = ownerAPI.LeaveRoomVoice(context.Background(), roomID)
		_, _ = playerAPI.LeaveRoomVoice(context.Background(), roomID)
	}()
	payload := json.RawMessage(`{"sdp":"smoke-offer","type":"offer"}`)
	signal, err := ownerAPI.SendRoomVoiceWebRTCSignal(ctx, roomID, playerUserID, "offer", payload)
	if err != nil {
		failf("send webrtc voice signal: %v", err)
	}
	if signal.FromUserID != ownerUserID || signal.ToUserID != playerUserID || signal.Type != "offer" || !bytes.Equal(signal.Payload, payload) {
		failf("webrtc signal response mismatch from=%d to=%d type=%s payload=%s", signal.FromUserID, signal.ToUserID, signal.Type, string(signal.Payload))
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			if event.VoiceSignal == nil {
				continue
			}
			got := event.VoiceSignal
			if got.RoomID == roomID && got.FromUserID == ownerUserID && got.ToUserID == playerUserID && got.Type == "offer" && bytes.Equal(got.Payload, payload) {
				log.Printf("webrtc voice signal ok from=%d to=%d", ownerUserID, playerUserID)
				return
			}
		case <-deadline:
			failf("webrtc voice signal event was not delivered")
		case <-ctx.Done():
			failf("webrtc voice signal: %v", ctx.Err())
		}
	}
}

func verifyUDPRendezvous(ctx context.Context, overlayAddr string, roomID, ownerID, playerID int64, owner, player *overlaySmokeClient) {
	ownerConfig := waitUDPConfig(ctx, owner)
	playerConfig := waitUDPConfig(ctx, player)
	ownerConn := listenSmokeUDP()
	defer ownerConn.Close()
	playerConn := listenSmokeUDP()
	defer playerConn.Close()
	serverAddr, err := net.ResolveUDPAddr("udp", overlayAddr)
	must("resolve udp overlay", err)

	sendUDPRegister(ownerConn, serverAddr, ownerConfig, roomID, ownerID, owner.lease.IP, smokeLANEndpoints(ownerConn))
	sendUDPRegister(playerConn, serverAddr, playerConfig, roomID, playerID, player.lease.IP, smokeLANEndpoints(playerConn))

	ownerPeer := waitUDPPeer(ctx, owner, player.lease.IP)
	playerPeer := waitUDPPeer(ctx, player, owner.lease.IP)
	log.Printf("udp rendezvous candidates ok ownerPeerLAN=%d playerPeerLAN=%d", len(ownerPeer.LANEndpoints), len(playerPeer.LANEndpoints))

	publicOK := tryUDPDirectPair(ctx, "public", ownerConn, playerConn, owner.lease.IP, player.lease.IP, ownerPeer, playerPeer, []string{ownerPeer.Endpoint}, []string{playerPeer.Endpoint})
	lanOK := false
	if len(ownerPeer.LANEndpoints) > 0 && len(playerPeer.LANEndpoints) > 0 {
		lanOK = tryUDPDirectPair(ctx, "lan", ownerConn, playerConn, owner.lease.IP, player.lease.IP, ownerPeer, playerPeer, ownerPeer.LANEndpoints, playerPeer.LANEndpoints)
	}
	if !publicOK && !lanOK {
		failf("udp direct probes failed for public and lan candidates")
	}
	log.Printf("udp direct probe ok public=%v lan=%v", publicOK, lanOK)
}

func waitUDPConfig(ctx context.Context, client *overlaySmokeClient) overlay.UDPControl {
	for {
		select {
		case config := <-client.udpConfigCh:
			if config.Token != "" && config.Key != "" {
				return config
			}
		case <-time.After(5 * time.Second):
			failf("timed out waiting for udp config")
		case <-ctx.Done():
			failf("wait udp config: %v", ctx.Err())
		}
	}
}

func waitUDPPeer(ctx context.Context, client *overlaySmokeClient, virtualIP string) overlay.UDPPeerCandidate {
	for {
		select {
		case peer := <-client.udpPeerCh:
			if peer.VirtualIP == virtualIP && peer.Endpoint != "" && peer.SessionID != "" && peer.Key != "" {
				return peer
			}
		case <-time.After(6 * time.Second):
			failf("timed out waiting for udp peer candidate ip=%s", virtualIP)
		case <-ctx.Done():
			failf("wait udp peer candidate: %v", ctx.Err())
		}
	}
}

func listenSmokeUDP() *net.UDPConn {
	conn, err := net.ListenUDP("udp", nil)
	must("listen smoke udp", err)
	return conn
}

func sendUDPRegister(conn *net.UDPConn, server *net.UDPAddr, config overlay.UDPControl, roomID, userID int64, virtualIP string, lanEndpoints []string) {
	key, err := overlay.DecodeUDPKey(config.Key)
	must("decode udp config key", err)
	payload := overlay.UDPRegisterPayload{
		RoomID:       roomID,
		UserID:       userID,
		VirtualIP:    virtualIP,
		LANEndpoints: lanEndpoints,
	}
	raw, err := overlay.EncodeUDPJSON(overlay.UDPTypeRegister, config.Token, key, uint64(time.Now().UnixNano()), payload)
	must("encode udp register", err)
	if _, err := conn.WriteToUDP(raw, server); err != nil {
		failf("send udp register: %v", err)
	}
}

func smokeLANEndpoints(conn *net.UDPConn) []string {
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.Port <= 0 {
		return nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, item := range addrs {
			var ip net.IP
			switch value := item.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip4 := ip.To4()
			if overlay.IsLANIPv4(ip4) {
				out = append(out, net.JoinHostPort(ip4.String(), strconv.Itoa(addr.Port)))
			}
		}
	}
	return overlay.NormalizeUDPLANEndpoints(out)
}

func tryUDPDirectPair(ctx context.Context, label string, ownerConn, playerConn *net.UDPConn, ownerIP, playerIP string, ownerPeer, playerPeer overlay.UDPPeerCandidate, ownerTargets, playerTargets []string) bool {
	ownerAddrs := resolveUDPTargets(ownerTargets)
	playerAddrs := resolveUDPTargets(playerTargets)
	if len(ownerAddrs) == 0 || len(playerAddrs) == 0 {
		return false
	}
	ownerPairKey, err := overlay.DecodeUDPKey(ownerPeer.Key)
	must("decode owner udp pair key", err)
	playerPairKey, err := overlay.DecodeUDPKey(playerPeer.Key)
	must("decode player udp pair key", err)
	ownerSend, ownerRecv := smokeUDPDirectionalKeys(ownerPairKey, ownerIP, playerIP)
	playerSend, playerRecv := smokeUDPDirectionalKeys(playerPairKey, playerIP, ownerIP)
	ownerPayload := []byte("owner-probe-" + label + "-" + requestID())
	playerPayload := []byte("player-probe-" + label + "-" + requestID())

	ownerGot := make(chan struct{}, 1)
	playerGot := make(chan struct{}, 1)
	attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go waitUDPProbe(attemptCtx, ownerConn, ownerRecv, playerPayload, ownerGot)
	go waitUDPProbe(attemptCtx, playerConn, playerRecv, ownerPayload, playerGot)

	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	ownerReceived := false
	playerReceived := false
	for {
		sendUDPProbe(ownerConn, ownerAddrs, ownerPeer.SessionID, ownerSend, ownerPayload)
		sendUDPProbe(playerConn, playerAddrs, playerPeer.SessionID, playerSend, playerPayload)
		select {
		case <-ownerGot:
			ownerReceived = true
			if playerReceived {
				return true
			}
		case <-playerGot:
			playerReceived = true
			if ownerReceived {
				return true
			}
		case <-ticker.C:
		case <-attemptCtx.Done():
			return false
		}
	}
}

func resolveUDPTargets(values []string) []*net.UDPAddr {
	out := make([]*net.UDPAddr, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		addr, err := net.ResolveUDPAddr("udp", value)
		if err != nil || addr == nil {
			continue
		}
		key := addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, addr)
	}
	return out
}

func sendUDPProbe(conn *net.UDPConn, targets []*net.UDPAddr, sessionID string, key []byte, payload []byte) {
	sendUDPPayload(conn, targets, sessionID, key, overlay.UDPTypeProbe, payload)
}

func sendUDPPayload(conn *net.UDPConn, targets []*net.UDPAddr, sessionID string, key []byte, packetType byte, payload []byte) {
	for _, target := range targets {
		raw, err := overlay.EncodeUDPPacket(packetType, sessionID, key, uint64(time.Now().UnixNano()), payload)
		if err == nil {
			_, _ = conn.WriteToUDP(raw, target)
		}
	}
}

func waitUDPProbe(ctx context.Context, conn *net.UDPConn, key []byte, want []byte, ok chan<- struct{}) {
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			continue
		}
		packet, err := overlay.DecodeUDPPacket(buf[:n], key)
		if err == nil && packet.Type == overlay.UDPTypeProbe && bytes.Equal(packet.Payload, want) {
			select {
			case ok <- struct{}{}:
			default:
			}
			return
		}
	}
}

func smokeUDPDirectionalKeys(pairKey []byte, localIP, peerIP string) (sendKey, recvKey []byte) {
	return smokeUDPDirectionalKey(pairKey, localIP, peerIP), smokeUDPDirectionalKey(pairKey, peerIP, localIP)
}

func smokeUDPDirectionalKey(pairKey []byte, fromIP, toIP string) []byte {
	mac := hmac.New(sha256.New, pairKey)
	_, _ = mac.Write([]byte("LwyLink UDP directional key v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(fromIP))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(toIP))
	return mac.Sum(nil)
}

func verifyEmptyRoomCleanup(ctx context.Context, cleaner *smokeCleaner, apiURL string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	api := client.NewAPI(apiURL)
	_, err := registerSmokeUser(ctx, cleaner, api, "cleanup"+suffix, "smoke-password", "Cleanup Smoke")
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
		failf("empty room cleanup wait failed room id=%d: %v", roomInfo.ID, waitCtx.Err())
	}
	rooms, err := api.Rooms(waitCtx)
	must("check cleanup room list", err)
	if len(rooms) != 0 {
		failf("empty room cleanup did not remove room id=%d after %.0fs", roomInfo.ID, time.Since(start).Seconds())
	}
	log.Printf("empty room cleanup removed room id=%d after %.0fs", roomInfo.ID, time.Since(start).Seconds())
}

func verifyRoomAppHeartbeat(ctx context.Context, cleaner *smokeCleaner, apiURL string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	api := client.NewAPI(apiURL)
	_, err := registerSmokeUser(ctx, cleaner, api, "active"+suffix, "smoke-password", "Active Room Smoke")
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
				failf("active app heartbeat room disappeared, got %d room(s)", len(rooms))
			}
		case <-deadline:
			must("delete active heartbeat room", api.DeleteRoom(waitCtx, roomInfo.ID))
			log.Printf("app heartbeat kept room id=%d alive without overlay", roomInfo.ID)
			return
		case <-waitCtx.Done():
			failf("active app heartbeat timed out: %v", waitCtx.Err())
		}
	}
}

func verifyKickKeepsOwnerRoom(ctx context.Context, cleaner *smokeCleaner, apiURL string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(apiURL)
	owner, err := registerSmokeUser(ctx, cleaner, ownerAPI, "kickowner"+suffix, "smoke-password", "Kick Owner Smoke")
	must("register kick owner", err)
	roomInfo, err := ownerAPI.CreateRoom(ctx, "Kick Room "+suffix, 2)
	must("create kick room", err)

	playerAPI := client.NewAPI(apiURL)
	player, err := registerSmokeUser(ctx, cleaner, playerAPI, "kickplayer"+suffix, "smoke-password", "Kick Player Smoke")
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
				failf("kick removed owner room unexpectedly, owner=%d got %d room(s)", owner.User.ID, len(rooms))
			}
			members, err := ownerAPI.RoomMembers(waitCtx, roomInfo.ID)
			must("list kick room members", err)
			if len(members) != 1 || members[0].UserID != owner.User.ID {
				failf("kick room members mismatch after kick: got %d member(s)", len(members))
			}
		case <-deadline:
			must("delete kick room", ownerAPI.DeleteRoom(waitCtx, roomInfo.ID))
			log.Printf("kick kept owner room id=%d alive without overlay", roomInfo.ID)
			return
		case <-waitCtx.Done():
			failf("kick owner room check timed out: %v", waitCtx.Err())
		}
	}
}

func verifyKickDisconnectsOverlayAndSSE(ctx context.Context, cleaner *smokeCleaner, apiURL, overlayAddr string) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(apiURL)
	_, err := registerSmokeUser(ctx, cleaner, ownerAPI, "eventowner"+suffix, "smoke-password", "Event Owner Smoke")
	must("register event owner", err)
	roomInfo, err := ownerAPI.CreateRoom(ctx, "Event Room "+suffix, 2)
	must("create event room", err)

	playerAPI := client.NewAPI(apiURL)
	player, err := registerSmokeUser(ctx, cleaner, playerAPI, "eventplayer"+suffix, "smoke-password", "Event Player Smoke")
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
			failf("kick event/overlay close timeout kickEvent=%v overlayClose=%v", gotKickEvent, gotOverlayClose)
		}
	}
	must("delete event room", ownerAPI.DeleteRoom(ctx, roomInfo.ID))
}

func subnetBroadcast(ip, mask string) string {
	broadcast := netutil.SubnetBroadcast(net.ParseIP(ip), net.ParseIP(mask))
	if broadcast == nil {
		failf("bad lease ip/mask ip=%s mask=%s", ip, mask)
	}
	return broadcast.String()
}

type overlaySmokeClient struct {
	client      *tcpx.Client
	lease       overlay.DHCPResponse
	ipFrames    chan []byte
	udpConfigCh chan overlay.UDPControl
	udpPeerCh   chan overlay.UDPPeerCandidate
	done        chan error
	cancel      context.CancelFunc
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
	udpConfigCh := make(chan overlay.UDPControl, 2)
	udpPeerCh := make(chan overlay.UDPPeerCandidate, 8)
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
			case overlay.FrameUDPControl:
				var control overlay.UDPControl
				if err := json.Unmarshal(payload, &control); err != nil {
					pushErr(errCh, err)
					return
				}
				switch control.Type {
				case overlay.UDPControlConfig:
					select {
					case udpConfigCh <- control:
					default:
					}
				case overlay.UDPControlPeer:
					if control.Peer != nil {
						select {
						case udpPeerCh <- *control.Peer:
						default:
						}
					}
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
			client:      c,
			lease:       lease,
			ipFrames:    ipFrames,
			udpConfigCh: udpConfigCh,
			udpPeerCh:   udpPeerCh,
			done:        errCh,
			cancel:      cancel,
		}
	case err := <-errCh:
		cancel()
		_ = c.Close()
		failf("overlay connect: %v", err)
	case <-ctx.Done():
		cancel()
		_ = c.Close()
		failf("overlay connect: %v", ctx.Err())
	}
	return nil
}

func ipv4Packet(src, dst string) []byte {
	src4 := net.ParseIP(src).To4()
	dst4 := net.ParseIP(dst).To4()
	if src4 == nil || dst4 == nil {
		failf("bad smoke ipv4 packet src=%s dst=%s", src, dst)
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
