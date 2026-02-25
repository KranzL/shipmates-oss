package discord

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hraban/opus"
	"golang.org/x/time/rate"
)

const (
	OpusSampleRate         = 48000
	OpusChannels           = 2
	OpusFrameSize          = 960
	OpusFrameBytes         = OpusFrameSize * OpusChannels * 2
	MaxPCMBufferBytes      = 48000 * 2 * 2 * 30
	MinPCMBufferBytes      = 9600
	SilenceTimeout         = 2000 * time.Millisecond
	VoiceRatePerUser       = 10
	VoiceBurstPerUser      = 3
	VoiceRatePerGuild      = 30
	VoiceBurstPerGuild     = 10
	OpusPacketMaxBytes     = 4000
	maxConcurrentSpeakers           = 50
	voiceRateLimiterEvictAfter      = 30 * time.Minute
	voiceRateLimiterCleanupInterval = 10 * time.Minute
)

var pcmPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, MaxPCMBufferBytes)
		return &buf
	},
}

type voiceRateLimiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

type VoiceManager struct {
	connections     map[string]*discordgo.VoiceConnection
	activeListeners map[string]context.CancelFunc
	encoder         *opus.Encoder
	encoderOnce     sync.Once
	encoderMu       sync.Mutex
	mu              sync.Mutex
	cancel          context.CancelFunc

	voiceRateLimiters   map[string]voiceRateLimiterEntry
	voiceRateLimitersMu sync.Mutex
}

func NewVoiceManager(ctx context.Context) *VoiceManager {
	vmCtx, cancel := context.WithCancel(ctx)
	vm := &VoiceManager{
		connections:       make(map[string]*discordgo.VoiceConnection),
		activeListeners:   make(map[string]context.CancelFunc),
		voiceRateLimiters: make(map[string]voiceRateLimiterEntry),
		cancel:            cancel,
	}
	go vm.cleanupVoiceRateLimiters(vmCtx)
	return vm
}

func (vm *VoiceManager) Close() {
	if vm.cancel != nil {
		vm.cancel()
	}
}

func (vm *VoiceManager) cleanupVoiceRateLimiters(ctx context.Context) {
	ticker := time.NewTicker(voiceRateLimiterCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			vm.voiceRateLimitersMu.Lock()
			for key, entry := range vm.voiceRateLimiters {
				if now.Sub(entry.lastUsed) > voiceRateLimiterEvictAfter {
					delete(vm.voiceRateLimiters, key)
				}
			}
			vm.voiceRateLimitersMu.Unlock()
		}
	}
}

func (vm *VoiceManager) Register(guildID string, vc *discordgo.VoiceConnection) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if existing, ok := vm.connections[guildID]; ok {
		existing.Disconnect()
	}

	vm.connections[guildID] = vc
}

func (vm *VoiceManager) Disconnect(guildID string) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if cancelFn, ok := vm.activeListeners[guildID]; ok {
		cancelFn()
		delete(vm.activeListeners, guildID)
	}

	if vc, ok := vm.connections[guildID]; ok {
		vc.Disconnect()
		delete(vm.connections, guildID)
	}
}

func (vm *VoiceManager) DisconnectAll() {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	for guildID, cancelFn := range vm.activeListeners {
		cancelFn()
		delete(vm.activeListeners, guildID)
	}

	for guildID, vc := range vm.connections {
		vc.Disconnect()
		delete(vm.connections, guildID)
	}
}

func (vm *VoiceManager) GetConnection(guildID string) (*discordgo.VoiceConnection, bool) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vc, ok := vm.connections[guildID]
	return vc, ok
}

func (vm *VoiceManager) ListenForSpeech(ctx context.Context, guildID string, handler SpeechHandler) {
	vm.mu.Lock()
	vc, ok := vm.connections[guildID]
	if !ok {
		vm.mu.Unlock()
		return
	}

	listenCtx, cancel := context.WithCancel(ctx)
	if existingCancel, ok := vm.activeListeners[guildID]; ok {
		existingCancel()
	}
	vm.activeListeners[guildID] = cancel
	vm.mu.Unlock()

	defer cancel()

	type ssrcState struct {
		decoder  *opus.Decoder
		pcmBuf   *[]byte
		frameBuf []int16
		timer    *time.Timer
	}

	states := make(map[uint32]*ssrcState)
	var statesMu sync.Mutex
	ssrcCapLogged := false

	flushSSRC := func(ssrc uint32) {
		statesMu.Lock()
		state, ok := states[ssrc]
		if !ok {
			statesMu.Unlock()
			return
		}

		pcmBuf := state.pcmBuf
		bufLen := len(*pcmBuf)
		state.pcmBuf = acquirePCMBuffer()
		delete(states, ssrc)
		statesMu.Unlock()

		if bufLen < MinPCMBufferBytes {
			releasePCMBuffer(pcmBuf)
			return
		}

		userKey := fmt.Sprintf("%d", ssrc)
		if !vm.allowVoiceRequest(guildID, userKey) {
			releasePCMBuffer(pcmBuf)
			return
		}

		pcmCopy := make([]byte, bufLen)
		copy(pcmCopy, *pcmBuf)
		releasePCMBuffer(pcmBuf)

		go vm.processSpeech(listenCtx, guildID, userKey, pcmCopy, handler)
	}

	for {
		select {
		case <-listenCtx.Done():
			statesMu.Lock()
			for _, state := range states {
				if state.timer != nil {
					state.timer.Stop()
				}
				releasePCMBuffer(state.pcmBuf)
			}
			statesMu.Unlock()
			return

		case packet, ok := <-vc.OpusRecv:
			if !ok {
				return
			}

			if packet == nil || len(packet.Opus) == 0 {
				continue
			}

			if len(packet.Opus) > 4000 {
				continue
			}

			ssrc := packet.SSRC

			statesMu.Lock()
			state, exists := states[ssrc]
			if !exists {
				if len(states) >= maxConcurrentSpeakers {
					statesMu.Unlock()
					if !ssrcCapLogged {
						log.Printf("[voice] concurrent speaker limit reached for guild %s, dropping new SSRC", guildID)
						ssrcCapLogged = true
					}
					continue
				}
				decoder, decErr := opus.NewDecoder(OpusSampleRate, OpusChannels)
				if decErr != nil {
					log.Printf("[voice] create opus decoder: %v", decErr)
					statesMu.Unlock()
					continue
				}
				state = &ssrcState{
					decoder:  decoder,
					pcmBuf:   acquirePCMBuffer(),
					frameBuf: make([]int16, OpusFrameSize*OpusChannels),
				}
				states[ssrc] = state
			}
			statesMu.Unlock()

			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[voice] opus decode panic: %v", r)
					}
				}()

				samplesDecoded, decErr := state.decoder.Decode(packet.Opus, state.frameBuf)
				if decErr != nil {
					return
				}

				pcmSamples := state.frameBuf[:samplesDecoded*OpusChannels]
				byteLen := len(pcmSamples) * 2

				if len(*state.pcmBuf)+byteLen > MaxPCMBufferBytes {
					if state.timer != nil {
						state.timer.Stop()
					}
					flushSSRC(ssrc)
					return
				}

				offset := len(*state.pcmBuf)
				*state.pcmBuf = (*state.pcmBuf)[:offset+byteLen]
				for i, sample := range pcmSamples {
					binary.LittleEndian.PutUint16((*state.pcmBuf)[offset+i*2:], uint16(sample))
				}

				if state.timer != nil {
					state.timer.Stop()
				}
				capturedSSRC := ssrc
				state.timer = time.AfterFunc(SilenceTimeout, func() {
					flushSSRC(capturedSSRC)
				})
			}()
		}
	}
}

func (vm *VoiceManager) getOrCreateEncoder() (*opus.Encoder, error) {
	var err error
	vm.encoderOnce.Do(func() {
		vm.encoder, err = opus.NewEncoder(OpusSampleRate, OpusChannels, opus.AppVoIP)
		if err != nil {
			err = fmt.Errorf("create opus encoder: %w", err)
		}
	})
	if err != nil {
		return nil, err
	}
	return vm.encoder, nil
}

func (vm *VoiceManager) processSpeech(ctx context.Context, guildID, userID string, pcmBuffer []byte, handler SpeechHandler) {
	speechCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	responseAudio, err := handler.HandleSpeech(speechCtx, pcmBuffer, guildID, userID)
	if err != nil {
		log.Printf("[voice] speech processing error for guild %s: %v", guildID, err)
		return
	}

	if len(responseAudio) > 0 {
		playErr := vm.playAudio(guildID, responseAudio)
		if playErr != nil {
			log.Printf("[voice] audio playback error for guild %s: %v", guildID, playErr)
		}
	}
}

func (vm *VoiceManager) playAudio(guildID string, audioData []byte) error {
	vm.mu.Lock()
	vc, ok := vm.connections[guildID]
	vm.mu.Unlock()

	if !ok {
		return fmt.Errorf("no voice connection for guild %s", guildID)
	}

	vc.Speaking(true)
	defer vc.Speaking(false)

	encoder, err := vm.getOrCreateEncoder()
	if err != nil {
		return err
	}

	stereo48k := resampleMono24kToStereo48k(audioData)

	frameSamples := OpusFrameSize * OpusChannels
	opusBuf := make([]byte, OpusPacketMaxBytes)
	samples := bytesToInt16(stereo48k)

	for i := 0; i+frameSamples <= len(samples); i += frameSamples {
		frame := samples[i : i+frameSamples]

		vm.encoderMu.Lock()
		n, encErr := encoder.Encode(frame, opusBuf)
		vm.encoderMu.Unlock()

		if encErr != nil {
			return fmt.Errorf("opus encode: %w", encErr)
		}

		select {
		case vc.OpusSend <- opusBuf[:n]:
		case <-time.After(20 * time.Millisecond):
		}

		time.Sleep(20 * time.Millisecond)
	}

	return nil
}

func resampleMono24kToStereo48k(data []byte) []byte {
	monoSamples := len(data) / 2
	out := make([]byte, monoSamples*2*2*2)

	for i := 0; i < monoSamples; i++ {
		sample := binary.LittleEndian.Uint16(data[i*2:])

		outIdx := i * 8
		binary.LittleEndian.PutUint16(out[outIdx:], sample)
		binary.LittleEndian.PutUint16(out[outIdx+2:], sample)
		binary.LittleEndian.PutUint16(out[outIdx+4:], sample)
		binary.LittleEndian.PutUint16(out[outIdx+6:], sample)
	}

	return out
}

func (vm *VoiceManager) allowVoiceRequest(guildID, userID string) bool {
	userKey := "voice:" + guildID + ":" + userID
	guildKey := "voice:" + guildID

	vm.voiceRateLimitersMu.Lock()
	defer vm.voiceRateLimitersMu.Unlock()

	now := time.Now()

	userEntry, exists := vm.voiceRateLimiters[userKey]
	if !exists {
		userEntry = voiceRateLimiterEntry{
			limiter: rate.NewLimiter(rate.Every(time.Minute/VoiceRatePerUser), VoiceBurstPerUser),
		}
	}
	userEntry.lastUsed = now
	vm.voiceRateLimiters[userKey] = userEntry

	guildEntry, exists := vm.voiceRateLimiters[guildKey]
	if !exists {
		guildEntry = voiceRateLimiterEntry{
			limiter: rate.NewLimiter(rate.Every(time.Minute/VoiceRatePerGuild), VoiceBurstPerGuild),
		}
	}
	guildEntry.lastUsed = now
	vm.voiceRateLimiters[guildKey] = guildEntry

	if !userEntry.limiter.Allow() {
		return false
	}
	return guildEntry.limiter.Allow()
}

func acquirePCMBuffer() *[]byte {
	buf := pcmPool.Get().(*[]byte)
	*buf = (*buf)[:0]
	return buf
}

func releasePCMBuffer(buf *[]byte) {
	if buf == nil {
		return
	}
	if cap(*buf) <= MaxPCMBufferBytes*2 {
		*buf = (*buf)[:0]
		pcmPool.Put(buf)
	}
}

func bytesToInt16(data []byte) []int16 {
	count := len(data) / 2
	samples := make([]int16, count)
	for i := 0; i < count; i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}
