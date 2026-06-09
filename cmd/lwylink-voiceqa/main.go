package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
)

const (
	qaSampleRate      = 16000
	qaFrameDurationMS = 20
	qaFrameBytes      = qaSampleRate / 50 * 2
)

func main() {
	os.Exit(run())
}

func run() int {
	apiURL := flag.String("api", "https://www.lwylink.xyz", "HTTP API base URL")
	overlayAddr := flag.String("overlay", "", "Overlay TCP address, for example 64.83.34.53:35768")
	adminToken := flag.String("admin-token", os.Getenv("LWYLINK_ADMIN_TOKEN"), "admin token for smoke registration and cleanup")
	frames := flag.Int("frames", 180, "synthetic PCM16 frames per direction")
	runs := flag.Int("runs", 10, "voice QA loops; set 0 for infinite")
	jitterMS := flag.Int("jitter-ms", 800, "simulated client jitter buffer delay")
	outDir := flag.String("out", "build/voiceqa-auto", "artifact output directory")
	password := flag.String("password", "voiceqa-password", "temporary account password")
	flag.Parse()

	if strings.TrimSpace(*overlayAddr) == "" {
		log.Printf("missing -overlay")
		return 2
	}
	if *frames <= 0 {
		log.Printf("-frames must be greater than zero")
		return 2
	}
	if *jitterMS <= 0 {
		log.Printf("-jitter-ms must be greater than zero")
		return 2
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaner := newCleaner(*apiURL, *adminToken)
	defer cleaner.cleanup(context.Background())

	if err := runVoiceQA(ctx, qaConfig{
		APIURL:       *apiURL,
		OverlayAddr:  *overlayAddr,
		Password:     *password,
		Frames:       *frames,
		Runs:         *runs,
		JitterMS:     *jitterMS,
		OutDir:       *outDir,
		AdminCleaner: cleaner,
	}); err != nil {
		log.Printf("VOICE QA FAILED: %v", err)
		return 1
	}
	log.Printf("VOICE QA OK")
	return 0
}

type qaConfig struct {
	APIURL       string
	OverlayAddr  string
	Password     string
	Frames       int
	Runs         int
	JitterMS     int
	OutDir       string
	AdminCleaner *cleaner
}

func runVoiceQA(ctx context.Context, cfg qaConfig) error {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerAPI := client.NewAPI(cfg.APIURL)
	owner, err := registerQAUser(ctx, cfg.AdminCleaner, ownerAPI, "voiceqa-owner"+suffix, cfg.Password, "Voice QA Owner")
	if err != nil {
		return fmt.Errorf("register owner: %w", err)
	}
	playerAPI := client.NewAPI(cfg.APIURL)
	player, err := registerQAUser(ctx, cfg.AdminCleaner, playerAPI, "voiceqa-player"+suffix, cfg.Password, "Voice QA Player")
	if err != nil {
		return fmt.Errorf("register player: %w", err)
	}
	roomInfo, err := ownerAPI.CreateRoom(ctx, "Voice QA "+suffix, 2)
	if err != nil {
		return fmt.Errorf("create room: %w", err)
	}
	defer func() {
		_ = ownerAPI.DeleteRoom(context.Background(), roomInfo.ID)
	}()
	joined, err := playerAPI.JoinRoom(ctx, roomInfo.RoomPublicKey)
	if err != nil {
		return fmt.Errorf("join room: %w", err)
	}
	if _, err := ownerAPI.JoinRoomVoice(ctx, roomInfo.ID); err != nil {
		return fmt.Errorf("owner join voice: %w", err)
	}
	if _, err := playerAPI.JoinRoomVoice(ctx, roomInfo.ID); err != nil {
		return fmt.Errorf("player join voice: %w", err)
	}
	if _, err := ownerAPI.SetRoomVoiceMic(ctx, roomInfo.ID, true); err != nil {
		return fmt.Errorf("owner open mic: %w", err)
	}
	if _, err := playerAPI.SetRoomVoiceMic(ctx, roomInfo.ID, true); err != nil {
		return fmt.Errorf("player open mic: %w", err)
	}
	defer func() {
		_, _ = ownerAPI.SetRoomVoiceMic(context.Background(), roomInfo.ID, false)
		_, _ = playerAPI.SetRoomVoiceMic(context.Background(), roomInfo.ID, false)
		_, _ = ownerAPI.LeaveRoomVoice(context.Background(), roomInfo.ID)
		_, _ = playerAPI.LeaveRoomVoice(context.Background(), roomInfo.ID)
	}()

	ownerOverlay, err := connectOverlay(ctx, cfg.OverlayAddr, joined.NodePublicKey, ownerAPI.Token(), roomInfo.ID, roomInfo.RoomPublicKey)
	if err != nil {
		return fmt.Errorf("connect owner overlay: %w", err)
	}
	defer ownerOverlay.close()
	playerOverlay, err := connectOverlay(ctx, cfg.OverlayAddr, joined.NodePublicKey, playerAPI.Token(), joined.ID, joined.RoomPublicKey)
	if err != nil {
		return fmt.Errorf("connect player overlay: %w", err)
	}
	defer playerOverlay.close()

	totalRuns := cfg.Runs
	for runIndex := 1; totalRuns == 0 || runIndex <= totalRuns; runIndex++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		report := runVoiceQARound(ctx, qaRound{
			RoomID:        roomInfo.ID,
			RunIndex:      runIndex,
			Runs:          totalRuns,
			Frames:        cfg.Frames,
			JitterMS:      cfg.JitterMS,
			OwnerID:       owner.User.ID,
			PlayerID:      player.User.ID,
			OwnerOverlay:  ownerOverlay,
			PlayerOverlay: playerOverlay,
		})
		runDir := outputDir(cfg.OutDir, runIndex)
		if err := writeReport(runDir, report); err != nil {
			return err
		}
		if err := writeRoundWAVs(runDir, report); err != nil {
			return err
		}
		if err := reportError(report); err != nil {
			return err
		}
		log.Printf("voice qa run=%d/%d ok owner_to_player=%d frames player_to_owner=%d frames",
			runIndex, totalRuns, report.Directions["owner_to_player"].ReceivedFrames, report.Directions["player_to_owner"].ReceivedFrames)
	}
	return nil
}

type qaRound struct {
	RoomID        int64
	RunIndex      int
	Runs          int
	Frames        int
	JitterMS      int
	OwnerID       int64
	PlayerID      int64
	OwnerOverlay  *qaOverlayClient
	PlayerOverlay *qaOverlayClient
}

type qaReport struct {
	StartedAt       string               `json:"startedAt"`
	RoomID          int64                `json:"roomID"`
	RunIndex        int                  `json:"runIndex"`
	Runs            int                  `json:"runs"`
	FrameCount      int                  `json:"frameCount"`
	SampleRate      int                  `json:"sampleRate"`
	FrameDurationMS int                  `json:"frameDurationMS"`
	JitterMS        int                  `json:"jitterMs"`
	Directions      map[string]qaMetrics `json:"directions"`
	SentPCM         map[string][]byte    `json:"-"`
	ReceivedPCM     map[string][]byte    `json:"-"`
}

type qaMetrics struct {
	FromUserID             int64        `json:"fromUserID"`
	ToUserID               int64        `json:"toUserID"`
	SentFrames             int          `json:"sentFrames"`
	ReceivedFrames         int          `json:"receivedFrames"`
	MissingFrames          int          `json:"missingFrames"`
	UnexpectedFrames       int          `json:"unexpectedFrames"`
	OutOfOrderFrames       int          `json:"outOfOrderFrames"`
	PayloadMismatches      int          `json:"payloadMismatches"`
	SentSHA256             string       `json:"sentSha256"`
	ReceivedSHA256         string       `json:"receivedSha256"`
	MinGapMS               int64        `json:"minGapMs"`
	MaxGapMS               int64        `json:"maxGapMs"`
	AverageGapMS           float64      `json:"averageGapMs"`
	P95GapMS               int64        `json:"p95GapMs"`
	P99GapMS               int64        `json:"p99GapMs"`
	GapSpikeFrames         int          `json:"gapSpikeFrames"`
	SimulatedUnderruns     int          `json:"simulatedUnderruns"`
	SimulatedMaxBufferedMS int64        `json:"simulatedMaxBufferedMs"`
	SendDurationMS         int64        `json:"sendDurationMs"`
	SendErrors             int          `json:"sendErrors"`
	GapSpikes              []qaGapSpike `json:"gapSpikes,omitempty"`
	Passed                 bool         `json:"passed"`
}

type qaGapSpike struct {
	Seq   uint64 `json:"seq"`
	GapMS int64  `json:"gapMs"`
}

func runVoiceQARound(ctx context.Context, round qaRound) qaReport {
	ownerFrames := syntheticFrames(round.Frames, uint64(round.RunIndex)*100000+1000, 440)
	playerFrames := syntheticFrames(round.Frames, uint64(round.RunIndex)*100000+5000, 660)
	report := qaReport{
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		RoomID:          round.RoomID,
		RunIndex:        round.RunIndex,
		Runs:            round.Runs,
		FrameCount:      round.Frames,
		SampleRate:      qaSampleRate,
		FrameDurationMS: qaFrameDurationMS,
		JitterMS:        round.JitterMS,
		Directions:      make(map[string]qaMetrics),
		SentPCM:         make(map[string][]byte),
		ReceivedPCM:     make(map[string][]byte),
	}
	report.Directions["owner_to_player"], report.SentPCM["owner_to_player"], report.ReceivedPCM["owner_to_player"] = runDirection(ctx, directionCase{
		Name:     "owner_to_player",
		RoomID:   round.RoomID,
		FromID:   round.OwnerID,
		ToID:     round.PlayerID,
		From:     round.OwnerOverlay,
		To:       round.PlayerOverlay,
		Frames:   ownerFrames,
		JitterMS: round.JitterMS,
	})
	report.Directions["player_to_owner"], report.SentPCM["player_to_owner"], report.ReceivedPCM["player_to_owner"] = runDirection(ctx, directionCase{
		Name:     "player_to_owner",
		RoomID:   round.RoomID,
		FromID:   round.PlayerID,
		ToID:     round.OwnerID,
		From:     round.PlayerOverlay,
		To:       round.OwnerOverlay,
		Frames:   playerFrames,
		JitterMS: round.JitterMS,
	})
	return report
}

type directionCase struct {
	Name     string
	RoomID   int64
	FromID   int64
	ToID     int64
	From     *qaOverlayClient
	To       *qaOverlayClient
	Frames   []qaFrame
	JitterMS int
}

type qaFrame struct {
	Seq         uint64
	TimestampMS int64
	Payload     []byte
}

type qaArrival struct {
	Seq       uint64
	ArrivedAt time.Time
}

func runDirection(ctx context.Context, tt directionCase) (qaMetrics, []byte, []byte) {
	expected := make(map[uint64][]byte, len(tt.Frames))
	sentPCM := make([]byte, 0, len(tt.Frames)*qaFrameBytes)
	for _, frame := range tt.Frames {
		expected[frame.Seq] = frame.Payload
		sentPCM = append(sentPCM, frame.Payload...)
	}
	sentHash := sha256.Sum256(sentPCM)
	receivedPCM := make([]byte, 0, len(sentPCM))
	received := make(map[uint64]bool, len(tt.Frames))
	metrics := qaMetrics{
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
	go sendFrames(tt, sendStats)

	var lastSeq uint64
	var lastArrival time.Time
	gaps := make([]int64, 0, len(tt.Frames))
	arrivals := make([]qaArrival, 0, len(tt.Frames))
	deadline := time.After(time.Duration(len(tt.Frames))*qaFrameDurationMS*time.Millisecond + 8*time.Second)
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
					metrics.GapSpikes = append(metrics.GapSpikes, qaGapSpike{Seq: voice.Seq, GapMS: gap})
				}
				gaps = append(gaps, gap)
				metrics.AverageGapMS += float64(gap)
			}
			lastArrival = now
			if lastSeq > 0 && voice.Seq < lastSeq {
				metrics.OutOfOrderFrames++
			}
			lastSeq = voice.Seq
			arrivals = append(arrivals, qaArrival{Seq: voice.Seq, ArrivedAt: now})
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
	metrics.P95GapMS = percentile(gaps, 95)
	metrics.P99GapMS = percentile(gaps, 99)
	metrics.SimulatedUnderruns, metrics.SimulatedMaxBufferedMS = simulatePlayout(arrivals, tt.Frames, tt.JitterMS)
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
	return metrics, sentPCM, receivedPCM
}

func sendFrames(tt directionCase, stats chan<- struct {
	duration time.Duration
	errors   int
}) {
	base := time.Now()
	errors := 0
	for i, frame := range tt.Frames {
		target := base.Add(time.Duration(i*qaFrameDurationMS) * time.Millisecond)
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
			SampleRate:  qaSampleRate,
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
	stats <- struct {
		duration time.Duration
		errors   int
	}{duration: time.Since(base), errors: errors}
}

func syntheticFrames(count int, seqBase uint64, hz int) []qaFrame {
	frames := make([]qaFrame, 0, count)
	start := time.Now().UnixMilli()
	phase := 0
	for i := 0; i < count; i++ {
		payload := make([]byte, qaFrameBytes)
		for s := 0; s < qaFrameBytes/2; s++ {
			phase++
			v := int16(((phase*hz*73)%24000 - 12000) / 3)
			payload[s*2] = byte(v)
			payload[s*2+1] = byte(v >> 8)
		}
		frames = append(frames, qaFrame{
			Seq:         seqBase + uint64(i+1),
			TimestampMS: start + int64(i*qaFrameDurationMS),
			Payload:     payload,
		})
	}
	return frames
}

func simulatePlayout(arrivals []qaArrival, frames []qaFrame, jitterMS int) (underruns int, maxBufferedMS int64) {
	if len(arrivals) == 0 || len(frames) == 0 {
		return len(frames), 0
	}
	arrivals = append([]qaArrival(nil), arrivals...)
	slices.SortFunc(arrivals, func(a, b qaArrival) int {
		return a.ArrivedAt.Compare(b.ArrivedAt)
	})
	available := make(map[uint64]bool, len(frames))
	arrivalIndex := 0
	playAt := arrivals[0].ArrivedAt.Add(time.Duration(jitterMS) * time.Millisecond)
	for _, frame := range frames {
		for arrivalIndex < len(arrivals) && !arrivals[arrivalIndex].ArrivedAt.After(playAt) {
			available[arrivals[arrivalIndex].Seq] = true
			arrivalIndex++
		}
		if bufferedMS := int64(len(available) * qaFrameDurationMS); bufferedMS > maxBufferedMS {
			maxBufferedMS = bufferedMS
		}
		if !available[frame.Seq] {
			underruns++
		} else {
			delete(available, frame.Seq)
		}
		playAt = playAt.Add(qaFrameDurationMS * time.Millisecond)
	}
	return underruns, maxBufferedMS
}

func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	slices.Sort(copyValues)
	if p <= 0 {
		return copyValues[0]
	}
	if p >= 100 {
		return copyValues[len(copyValues)-1]
	}
	idx := (len(copyValues)*p + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	return copyValues[idx-1]
}

func reportError(report qaReport) error {
	for name, metrics := range report.Directions {
		if !metrics.Passed {
			return fmt.Errorf("%s failed: received=%d/%d missing=%d unexpected=%d outOfOrder=%d mismatches=%d underruns=%d sent=%s got=%s",
				name, metrics.ReceivedFrames, metrics.SentFrames, metrics.MissingFrames, metrics.UnexpectedFrames,
				metrics.OutOfOrderFrames, metrics.PayloadMismatches, metrics.SimulatedUnderruns,
				metrics.SentSHA256, metrics.ReceivedSHA256)
		}
	}
	return nil
}

func writeReport(dir string, report qaReport) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out := report
	out.SentPCM = nil
	out.ReceivedPCM = nil
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dir+"/report.json", raw, 0o644)
}

func writeRoundWAVs(dir string, report qaReport) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, pcm := range report.SentPCM {
		if err := writeWAV(dir+"/"+name+"_sent.wav", pcm, qaSampleRate); err != nil {
			return err
		}
	}
	for name, pcm := range report.ReceivedPCM {
		if err := writeWAV(dir+"/"+name+"_received.wav", pcm, qaSampleRate); err != nil {
			return err
		}
	}
	return nil
}

func outputDir(root string, runIndex int) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if runIndex <= 1 {
		return root
	}
	return fmt.Sprintf("%s/run-%03d", strings.TrimRight(root, `/\`), runIndex)
}

func writeWAV(path string, pcm []byte, sampleRate int) error {
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
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func writeLE16(w io.Writer, v uint16) {
	_, _ = w.Write([]byte{byte(v), byte(v >> 8)})
}

func writeLE32(w io.Writer, v uint32) {
	_, _ = w.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

type qaOverlayClient struct {
	client      *tcpx.Client
	lease       overlay.DHCPResponse
	voiceFrames chan []byte
	cancel      context.CancelFunc
}

func (c *qaOverlayClient) close() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.client != nil {
		_ = c.client.Close()
	}
}

func connectOverlay(ctx context.Context, addr, serverPublicKey, token string, roomID int64, roomPublicKey string) (*qaOverlayClient, error) {
	serverPubHex, err := tcpx.NormalizePublicKeyHex(serverPublicKey)
	if err != nil {
		return nil, err
	}
	requestID := requestID()
	dhcpCh := make(chan overlay.DHCPResponse, 1)
	voiceFrames := make(chan []byte, 256)
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
					return
				}
				if lease.RequestID != requestID {
					pushErr(errCh, fmt.Errorf("dhcp request id mismatch"))
					return
				}
				select {
				case dhcpCh <- lease:
				default:
				}
			case overlay.FrameVoice:
				cp := append([]byte(nil), payload...)
				select {
				case voiceFrames <- cp:
				default:
				}
			case overlay.FrameError:
				var e map[string]string
				_ = json.Unmarshal(payload, &e)
				pushErr(errCh, fmt.Errorf("overlay error: %s", e["error"]))
			}
		},
	})
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		if err := c.Run(runCtx); err != nil && runCtx.Err() == nil {
			pushErr(errCh, err)
		}
	}()
	select {
	case lease := <-dhcpCh:
		return &qaOverlayClient{client: c, lease: lease, voiceFrames: voiceFrames, cancel: cancel}, nil
	case err := <-errCh:
		cancel()
		_ = c.Close()
		return nil, err
	case <-ctx.Done():
		cancel()
		_ = c.Close()
		return nil, ctx.Err()
	}
}

func registerQAUser(ctx context.Context, cleaner *cleaner, api *client.API, username, password, displayName string) (account.LoginResult, error) {
	result, err := cleaner.register(ctx, api, username+"@smoke.lwylink.test", password, displayName)
	if err == nil {
		cleaner.track(result.User.ID)
	}
	return result, err
}

type cleaner struct {
	apiURL     string
	adminToken string
	userIDs    []int64
}

func newCleaner(apiURL, adminToken string) *cleaner {
	return &cleaner{apiURL: strings.TrimRight(strings.TrimSpace(apiURL), "/"), adminToken: strings.TrimSpace(adminToken)}
}

func (c *cleaner) track(userID int64) {
	if c != nil && userID > 0 {
		c.userIDs = append(c.userIDs, userID)
	}
}

func (c *cleaner) register(ctx context.Context, api *client.API, email, password, displayName string) (account.LoginResult, error) {
	if c == nil || c.adminToken == "" {
		return api.Register(ctx, email, password, displayName, "")
	}
	var result account.LoginResult
	err := postJSON(ctx, c.apiURL+"/api/smoke/register?token="+url.QueryEscape(c.adminToken), map[string]string{
		"email":       email,
		"password":    password,
		"displayName": displayName,
	}, &result)
	if err == nil {
		api.SetToken(result.Token)
	}
	return result, err
}

func (c *cleaner) cleanup(ctx context.Context) {
	if c == nil || c.adminToken == "" {
		return
	}
	ids := append([]int64(nil), c.userIDs...)
	slices.Sort(ids)
	for i := len(ids) - 1; i >= 0; i-- {
		_ = c.deleteUser(ctx, ids[i])
	}
}

func (c *cleaner) deleteUser(ctx context.Context, userID int64) error {
	return postJSON(ctx, c.apiURL+"/api/admin/users/delete?token="+url.QueryEscape(c.adminToken), map[string]int64{"userID": userID}, nil)
}

func postJSON(ctx context.Context, target string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
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
