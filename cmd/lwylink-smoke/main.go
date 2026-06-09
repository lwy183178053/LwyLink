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
	"slices"
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
	voiceQAFrames := flag.Int("voice-qa-frames", 160, "number of synthetic PCM16 voice frames to send in each direction; set 0 to disable")
	voiceQARuns := flag.Int("voice-qa-runs", 1, "number of voice QA send/compare runs")
	voiceQAJitterMS := flag.Int("voice-qa-jitter-ms", 260, "simulated client jitter buffer delay in milliseconds")
	voiceQAOut := flag.String("voice-qa-out", "", "optional directory for voice QA JSON/WAV artifacts")
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
	verifyVoiceRelay(ctx, roomInfo.ID, owner.User.ID, player.User.ID, ownerOverlay, playerOverlay)
	if *voiceQAFrames > 0 && *voiceQARuns > 0 {
		for runIndex := 1; runIndex <= *voiceQARuns; runIndex++ {
			verifyVoiceQuality(ctx, voiceQACase{
				RoomID:        roomInfo.ID,
				OwnerID:       owner.User.ID,
				PlayerID:      player.User.ID,
				OwnerAPI:      ownerAPI,
				PlayerAPI:     playerAPI,
				OwnerOverlay:  ownerOverlay,
				PlayerOverlay: playerOverlay,
				Frames:        *voiceQAFrames,
				RunIndex:      runIndex,
				Runs:          *voiceQARuns,
				JitterMS:      *voiceQAJitterMS,
				OutDir:        *voiceQAOut,
			})
		}
	}
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

func verifyVoiceRelay(ctx context.Context, roomID, fromUserID, toUserID int64, from, to *overlaySmokeClient) {
	voice := overlay.VoicePayload{
		RoomID:      roomID,
		FromUserID:  fromUserID,
		ToUserID:    toUserID,
		Seq:         uint64(time.Now().UnixNano()),
		TimestampMS: time.Now().UnixMilli(),
		Codec:       "pcm16",
		SampleRate:  16000,
		Payload:     []byte("smoke-voice-relay"),
	}
	raw, err := json.Marshal(voice)
	must("marshal voice relay", err)
	if err := from.client.Write(overlay.PackFrame(overlay.FrameVoice, raw)); err != nil {
		failf("send voice relay: %v", err)
	}
	select {
	case got := <-to.voiceFrames:
		var forwarded overlay.VoicePayload
		if err := json.Unmarshal(got, &forwarded); err != nil {
			failf("voice relay returned bad payload: %v", err)
		}
		if forwarded.RoomID != roomID || forwarded.FromUserID != fromUserID || forwarded.ToUserID != toUserID || !bytes.Equal(forwarded.Payload, voice.Payload) {
			failf("voice relay mismatch room=%d from=%d to=%d payload=%q", forwarded.RoomID, forwarded.FromUserID, forwarded.ToUserID, forwarded.Payload)
		}
		log.Printf("overlay voice relay ok from=%d to=%d", fromUserID, toUserID)
	case <-time.After(3 * time.Second):
		failf("voice relay was not forwarded")
	case <-ctx.Done():
		failf("voice relay: %v", ctx.Err())
	}
}

type voiceQACase struct {
	RoomID        int64
	OwnerID       int64
	PlayerID      int64
	OwnerAPI      *client.API
	PlayerAPI     *client.API
	OwnerOverlay  *overlaySmokeClient
	PlayerOverlay *overlaySmokeClient
	Frames        int
	RunIndex      int
	Runs          int
	JitterMS      int
	OutDir        string
}

type voiceQAReport struct {
	StartedAt       string                    `json:"startedAt"`
	RoomID          int64                     `json:"roomID"`
	RunIndex        int                       `json:"runIndex"`
	Runs            int                       `json:"runs"`
	FrameCount      int                       `json:"frameCount"`
	SampleRate      int                       `json:"sampleRate"`
	FrameDurationMS int                       `json:"frameDurationMS"`
	JitterMS        int                       `json:"jitterMs"`
	Directions      map[string]voiceQAMetrics `json:"directions"`
}

type voiceQAMetrics struct {
	FromUserID             int64             `json:"fromUserID"`
	ToUserID               int64             `json:"toUserID"`
	SentFrames             int               `json:"sentFrames"`
	ReceivedFrames         int               `json:"receivedFrames"`
	MissingFrames          int               `json:"missingFrames"`
	UnexpectedFrames       int               `json:"unexpectedFrames"`
	OutOfOrderFrames       int               `json:"outOfOrderFrames"`
	PayloadMismatches      int               `json:"payloadMismatches"`
	SentSHA256             string            `json:"sentSha256"`
	ReceivedSHA256         string            `json:"receivedSha256"`
	MinGapMS               int64             `json:"minGapMs"`
	MaxGapMS               int64             `json:"maxGapMs"`
	AverageGapMS           float64           `json:"averageGapMs"`
	P95GapMS               int64             `json:"p95GapMs"`
	P99GapMS               int64             `json:"p99GapMs"`
	GapSpikeFrames         int               `json:"gapSpikeFrames"`
	SimulatedUnderruns     int               `json:"simulatedUnderruns"`
	SimulatedMaxBufferedMS int64             `json:"simulatedMaxBufferedMs"`
	SendDurationMS         int64             `json:"sendDurationMs"`
	SendErrors             int               `json:"sendErrors"`
	GapSpikes              []voiceQAGapSpike `json:"gapSpikes,omitempty"`
	Passed                 bool              `json:"passed"`
}

type voiceQAFrame struct {
	Seq         uint64
	TimestampMS int64
	Payload     []byte
}

type voiceQAArrival struct {
	Seq       uint64
	ArrivedAt time.Time
}

type voiceQAGapSpike struct {
	Seq   uint64 `json:"seq"`
	GapMS int64  `json:"gapMs"`
}

func verifyVoiceQuality(ctx context.Context, tt voiceQACase) {
	must("owner join voice qa", joinVoiceForQA(ctx, tt.OwnerAPI, tt.RoomID))
	must("player join voice qa", joinVoiceForQA(ctx, tt.PlayerAPI, tt.RoomID))
	defer func() {
		_, _ = tt.OwnerAPI.SetRoomVoiceMic(context.Background(), tt.RoomID, false)
		_, _ = tt.OwnerAPI.LeaveRoomVoice(context.Background(), tt.RoomID)
		_, _ = tt.PlayerAPI.SetRoomVoiceMic(context.Background(), tt.RoomID, false)
		_, _ = tt.PlayerAPI.LeaveRoomVoice(context.Background(), tt.RoomID)
	}()

	ownerFrames := syntheticVoiceFrames(tt.Frames, 1000, 440)
	playerFrames := syntheticVoiceFrames(tt.Frames, 5000, 660)
	outDir := voiceQAOutputDir(tt.OutDir, tt.RunIndex)
	report := voiceQAReport{
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		RoomID:          tt.RoomID,
		RunIndex:        tt.RunIndex,
		Runs:            tt.Runs,
		FrameCount:      tt.Frames,
		SampleRate:      16000,
		FrameDurationMS: 20,
		JitterMS:        tt.JitterMS,
		Directions:      make(map[string]voiceQAMetrics),
	}

	report.Directions["owner_to_player"] = runVoiceQADirection(ctx, voiceQADirection{
		Name:     "owner_to_player",
		RoomID:   tt.RoomID,
		FromID:   tt.OwnerID,
		ToID:     tt.PlayerID,
		From:     tt.OwnerOverlay,
		To:       tt.PlayerOverlay,
		Frames:   ownerFrames,
		JitterMS: tt.JitterMS,
		OutDir:   outDir,
		WAVStem:  "owner_to_player",
		SentFreq: 440,
	})
	report.Directions["player_to_owner"] = runVoiceQADirection(ctx, voiceQADirection{
		Name:     "player_to_owner",
		RoomID:   tt.RoomID,
		FromID:   tt.PlayerID,
		ToID:     tt.OwnerID,
		From:     tt.PlayerOverlay,
		To:       tt.OwnerOverlay,
		Frames:   playerFrames,
		JitterMS: tt.JitterMS,
		OutDir:   outDir,
		WAVStem:  "player_to_owner",
		SentFreq: 660,
	})
	if strings.TrimSpace(outDir) != "" {
		writeVoiceQAReport(outDir, report)
	}
	for name, metrics := range report.Directions {
		if !metrics.Passed {
			failf("voice qa %s failed: received=%d/%d missing=%d unexpected=%d outOfOrder=%d mismatches=%d sent=%s got=%s",
				name, metrics.ReceivedFrames, metrics.SentFrames, metrics.MissingFrames, metrics.UnexpectedFrames,
				metrics.OutOfOrderFrames, metrics.PayloadMismatches, metrics.SentSHA256, metrics.ReceivedSHA256)
		}
		log.Printf("voice qa run=%d/%d %s ok frames=%d avgGap=%.2fms minGap=%dms maxGap=%dms sha=%s",
			tt.RunIndex, tt.Runs, name, metrics.ReceivedFrames, metrics.AverageGapMS, metrics.MinGapMS, metrics.MaxGapMS, metrics.ReceivedSHA256)
	}
}

func joinVoiceForQA(ctx context.Context, api *client.API, roomID int64) error {
	if _, err := api.JoinRoomVoice(ctx, roomID); err != nil {
		return err
	}
	_, err := api.SetRoomVoiceMic(ctx, roomID, true)
	return err
}

type voiceQADirection struct {
	Name     string
	RoomID   int64
	FromID   int64
	ToID     int64
	From     *overlaySmokeClient
	To       *overlaySmokeClient
	Frames   []voiceQAFrame
	JitterMS int
	OutDir   string
	WAVStem  string
	SentFreq int
}

func runVoiceQADirection(ctx context.Context, tt voiceQADirection) voiceQAMetrics {
	expected := make(map[uint64][]byte, len(tt.Frames))
	sentPCM := make([]byte, 0, len(tt.Frames)*640)
	for _, frame := range tt.Frames {
		expected[frame.Seq] = frame.Payload
		sentPCM = append(sentPCM, frame.Payload...)
	}
	sentHash := sha256.Sum256(sentPCM)
	receivedPCM := make([]byte, 0, len(sentPCM))
	received := make(map[uint64]bool, len(tt.Frames))
	metrics := voiceQAMetrics{
		FromUserID: tt.FromID,
		ToUserID:   tt.ToID,
		SentFrames: len(tt.Frames),
		SentSHA256: hex.EncodeToString(sentHash[:]),
		MinGapMS:   1<<63 - 1,
	}

	sendStats := make(chan struct {
		duration time.Duration
		errors   int
	}, 1)
	go func() {
		base := time.Now()
		errors := 0
		for i, frame := range tt.Frames {
			target := base.Add(time.Duration(i*20) * time.Millisecond)
			if sleep := time.Until(target); sleep > 0 {
				time.Sleep(sleep)
			}
			voice := overlay.VoicePayload{
				RoomID:      tt.RoomID,
				FromUserID:  tt.FromID,
				ToUserID:    tt.ToID,
				Seq:         frame.Seq,
				TimestampMS: frame.TimestampMS,
				Codec:       "pcm16",
				SampleRate:  16000,
				Payload:     frame.Payload,
			}
			raw, err := json.Marshal(voice)
			if err != nil {
				errors++
				continue
			}
			if err := tt.From.client.Write(overlay.PackFrame(overlay.FrameVoice, raw)); err != nil {
				errors++
			}
		}
		sendStats <- struct {
			duration time.Duration
			errors   int
		}{duration: time.Since(base), errors: errors}
	}()

	var lastSeq uint64
	var lastArrival time.Time
	gaps := make([]int64, 0, len(tt.Frames))
	arrivals := make([]voiceQAArrival, 0, len(tt.Frames))
	deadline := time.After(time.Duration(len(tt.Frames))*20*time.Millisecond + 8*time.Second)
	for metrics.ReceivedFrames < len(tt.Frames) {
		select {
		case raw := <-tt.To.voiceFrames:
			var voice overlay.VoicePayload
			if err := json.Unmarshal(raw, &voice); err != nil {
				metrics.UnexpectedFrames++
				continue
			}
			if voice.RoomID != tt.RoomID || voice.FromUserID != tt.FromID || voice.ToUserID != tt.ToID {
				metrics.UnexpectedFrames++
				continue
			}
			now := time.Now()
			if !lastArrival.IsZero() {
				gap := now.Sub(lastArrival).Milliseconds()
				if gap < metrics.MinGapMS {
					metrics.MinGapMS = gap
				}
				if gap > metrics.MaxGapMS {
					metrics.MaxGapMS = gap
				}
				if gap > 80 {
					metrics.GapSpikeFrames++
					metrics.GapSpikes = append(metrics.GapSpikes, voiceQAGapSpike{Seq: voice.Seq, GapMS: gap})
				}
				gaps = append(gaps, gap)
				metrics.AverageGapMS += float64(gap)
			}
			lastArrival = now
			if lastSeq > 0 && voice.Seq < lastSeq {
				metrics.OutOfOrderFrames++
			}
			lastSeq = voice.Seq
			arrivals = append(arrivals, voiceQAArrival{Seq: voice.Seq, ArrivedAt: now})
			want, ok := expected[voice.Seq]
			if !ok {
				metrics.UnexpectedFrames++
				continue
			}
			if !bytes.Equal(want, voice.Payload) {
				metrics.PayloadMismatches++
			}
			if !received[voice.Seq] {
				received[voice.Seq] = true
				receivedPCM = append(receivedPCM, voice.Payload...)
				metrics.ReceivedFrames++
			}
		case <-deadline:
			metrics.MissingFrames = len(tt.Frames) - metrics.ReceivedFrames
			goto finish
		case <-ctx.Done():
			metrics.MissingFrames = len(tt.Frames) - metrics.ReceivedFrames
			goto finish
		}
	}

finish:
	select {
	case stats := <-sendStats:
		metrics.SendDurationMS = stats.duration.Milliseconds()
		metrics.SendErrors = stats.errors
	case <-time.After(2 * time.Second):
		metrics.SendErrors++
	}
	if metrics.MinGapMS == 1<<63-1 {
		metrics.MinGapMS = 0
	}
	if metrics.ReceivedFrames > 1 {
		metrics.AverageGapMS = metrics.AverageGapMS / float64(metrics.ReceivedFrames-1)
	}
	metrics.P95GapMS = percentileGap(gaps, 95)
	metrics.P99GapMS = percentileGap(gaps, 99)
	metrics.SimulatedUnderruns, metrics.SimulatedMaxBufferedMS = simulateVoicePlayout(arrivals, tt.Frames, tt.JitterMS)
	recvHash := sha256.Sum256(receivedPCM)
	metrics.ReceivedSHA256 = hex.EncodeToString(recvHash[:])
	metrics.MissingFrames = len(tt.Frames) - metrics.ReceivedFrames
	metrics.Passed = metrics.MissingFrames == 0 &&
		metrics.UnexpectedFrames == 0 &&
		metrics.OutOfOrderFrames == 0 &&
		metrics.PayloadMismatches == 0 &&
		metrics.SendErrors == 0 &&
		metrics.SentSHA256 == metrics.ReceivedSHA256 &&
		metrics.SimulatedUnderruns == 0
	if strings.TrimSpace(tt.OutDir) != "" {
		writeVoiceQAWAV(tt.OutDir, tt.WAVStem+"_sent.wav", sentPCM, 16000)
		writeVoiceQAWAV(tt.OutDir, tt.WAVStem+"_received.wav", receivedPCM, 16000)
	}
	return metrics
}

func syntheticVoiceFrames(count int, seqBase uint64, hz int) []voiceQAFrame {
	if count <= 0 {
		return nil
	}
	const sampleRate = 16000
	const frameSamples = sampleRate / 50
	frames := make([]voiceQAFrame, 0, count)
	start := time.Now().UnixMilli()
	phase := 0
	for i := 0; i < count; i++ {
		payload := make([]byte, frameSamples*2)
		for s := 0; s < frameSamples; s++ {
			phase++
			// Deterministic pseudo-sine-like waveform without floating point drift.
			v := int16(((phase*hz*73)%24000 - 12000) / 3)
			payload[s*2] = byte(v)
			payload[s*2+1] = byte(v >> 8)
		}
		frames = append(frames, voiceQAFrame{
			Seq:         seqBase + uint64(i+1),
			TimestampMS: start + int64(i*20),
			Payload:     payload,
		})
	}
	return frames
}

func percentileGap(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	slices.Sort(copyValues)
	if percentile <= 0 {
		return copyValues[0]
	}
	if percentile >= 100 {
		return copyValues[len(copyValues)-1]
	}
	idx := (len(copyValues)*percentile + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	return copyValues[idx-1]
}

func simulateVoicePlayout(arrivals []voiceQAArrival, frames []voiceQAFrame, jitterMS int) (underruns int, maxBufferedMS int64) {
	if len(arrivals) == 0 || len(frames) == 0 {
		return len(frames), 0
	}
	if jitterMS <= 0 {
		jitterMS = 260
	}
	arrivals = append([]voiceQAArrival(nil), arrivals...)
	slices.SortFunc(arrivals, func(a, b voiceQAArrival) int {
		return a.ArrivedAt.Compare(b.ArrivedAt)
	})
	expectedSeqs := make([]uint64, 0, len(frames))
	for _, frame := range frames {
		expectedSeqs = append(expectedSeqs, frame.Seq)
	}
	available := make(map[uint64]bool, len(frames))
	arrivalIndex := 0
	playAt := arrivals[0].ArrivedAt.Add(time.Duration(jitterMS) * time.Millisecond)
	for _, seq := range expectedSeqs {
		for arrivalIndex < len(arrivals) && !arrivals[arrivalIndex].ArrivedAt.After(playAt) {
			available[arrivals[arrivalIndex].Seq] = true
			arrivalIndex++
		}
		bufferedFrames := len(available)
		if bufferedFrames > 0 {
			bufferedMS := int64(bufferedFrames * 20)
			if bufferedMS > maxBufferedMS {
				maxBufferedMS = bufferedMS
			}
		}
		if !available[seq] {
			underruns++
		} else {
			delete(available, seq)
		}
		playAt = playAt.Add(20 * time.Millisecond)
	}
	return underruns, maxBufferedMS
}

func writeVoiceQAReport(dir string, report voiceQAReport) {
	must("create voice qa dir", os.MkdirAll(dir, 0o755))
	raw, err := json.MarshalIndent(report, "", "  ")
	must("marshal voice qa report", err)
	must("write voice qa report", os.WriteFile(dir+"/report.json", raw, 0o644))
}

func voiceQAOutputDir(root string, runIndex int) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if runIndex <= 1 {
		return root
	}
	return fmt.Sprintf("%s/run-%03d", strings.TrimRight(root, `/\`), runIndex)
}

func writeVoiceQAWAV(dir, name string, pcm []byte, sampleRate int) {
	must("create voice qa dir", os.MkdirAll(dir, 0o755))
	var out bytes.Buffer
	dataLen := uint32(len(pcm))
	riffLen := uint32(36 + len(pcm))
	out.WriteString("RIFF")
	writeLE32(&out, riffLen)
	out.WriteString("WAVEfmt ")
	writeLE32(&out, 16)
	writeLE16(&out, 1)
	writeLE16(&out, 1)
	writeLE32(&out, uint32(sampleRate))
	writeLE32(&out, uint32(sampleRate*2))
	writeLE16(&out, 2)
	writeLE16(&out, 16)
	out.WriteString("data")
	writeLE32(&out, dataLen)
	out.Write(pcm)
	must("write voice qa wav", os.WriteFile(dir+"/"+name, out.Bytes(), 0o644))
}

func writeLE16(w io.Writer, v uint16) {
	_, _ = w.Write([]byte{byte(v), byte(v >> 8)})
}

func writeLE32(w io.Writer, v uint32) {
	_, _ = w.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
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
	voicePublicOK := tryUDPVoicePair(ctx, "public", ownerConn, playerConn, owner.lease.IP, player.lease.IP, ownerPeer, playerPeer, []string{ownerPeer.Endpoint}, []string{playerPeer.Endpoint})
	voiceLANOK := false
	if len(ownerPeer.LANEndpoints) > 0 && len(playerPeer.LANEndpoints) > 0 {
		voiceLANOK = tryUDPVoicePair(ctx, "lan", ownerConn, playerConn, owner.lease.IP, player.lease.IP, ownerPeer, playerPeer, ownerPeer.LANEndpoints, playerPeer.LANEndpoints)
	}
	if !voicePublicOK && !voiceLANOK {
		failf("udp direct voice packets failed")
	}
	log.Printf("udp direct probe ok public=%v lan=%v voicePublic=%v voiceLAN=%v", publicOK, lanOK, voicePublicOK, voiceLANOK)
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

func tryUDPVoicePair(ctx context.Context, label string, ownerConn, playerConn *net.UDPConn, ownerIP, playerIP string, ownerPeer, playerPeer overlay.UDPPeerCandidate, ownerTargets, playerTargets []string) bool {
	ownerAddrs := resolveUDPTargets(ownerTargets)
	playerAddrs := resolveUDPTargets(playerTargets)
	if len(ownerAddrs) == 0 || len(playerAddrs) == 0 {
		return false
	}
	ownerPairKey, err := overlay.DecodeUDPKey(ownerPeer.Key)
	must("decode owner voice udp pair key", err)
	playerPairKey, err := overlay.DecodeUDPKey(playerPeer.Key)
	must("decode player voice udp pair key", err)
	ownerSend, ownerRecv := smokeUDPDirectionalKeys(ownerPairKey, ownerIP, playerIP)
	playerSend, playerRecv := smokeUDPDirectionalKeys(playerPairKey, playerIP, ownerIP)
	ownerPayload := []byte(`{"roomID":1,"fromUserID":1,"toUserID":2,"seq":1,"timestampMS":1,"codec":"pcm16","sampleRate":16000,"payload":"dm9pY2Utb3duZXI="}`)
	playerPayload := []byte(`{"roomID":1,"fromUserID":2,"toUserID":1,"seq":1,"timestampMS":1,"codec":"pcm16","sampleRate":16000,"payload":"dm9pY2UtcGxheWVy"}`)

	ownerGot := make(chan struct{}, 1)
	playerGot := make(chan struct{}, 1)
	attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go waitUDPVoice(attemptCtx, ownerConn, ownerRecv, playerPayload, ownerGot)
	go waitUDPVoice(attemptCtx, playerConn, playerRecv, ownerPayload, playerGot)

	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	ownerReceived := false
	playerReceived := false
	for {
		sendUDPPayload(ownerConn, ownerAddrs, ownerPeer.SessionID, ownerSend, overlay.UDPTypeVoice, ownerPayload)
		sendUDPPayload(playerConn, playerAddrs, playerPeer.SessionID, playerSend, overlay.UDPTypeVoice, playerPayload)
		select {
		case <-ownerGot:
			ownerReceived = true
			if playerReceived {
				log.Printf("udp direct voice %s ok", label)
				return true
			}
		case <-playerGot:
			playerReceived = true
			if ownerReceived {
				log.Printf("udp direct voice %s ok", label)
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

func waitUDPVoice(ctx context.Context, conn *net.UDPConn, key []byte, want []byte, ok chan<- struct{}) {
	buf := make([]byte, 8192)
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
		if err == nil && packet.Type == overlay.UDPTypeVoice && bytes.Equal(packet.Payload, want) {
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
	voiceFrames chan []byte
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
	voiceFrames := make(chan []byte, 8)
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
			case overlay.FrameVoice:
				frame := make([]byte, len(payload))
				copy(frame, payload)
				select {
				case voiceFrames <- frame:
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
			voiceFrames: voiceFrames,
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
