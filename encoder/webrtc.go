package encoder

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// WebRTCBroadcaster distributes low-latency Opus audio to peer connections via UDP RTP reception.
type WebRTCBroadcaster struct {
	stationID string
	natIP     string
	udpConn   *net.UDPConn
	udpPort   int
	peers     map[*webrtc.PeerConnection]*webrtc.TrackLocalStaticRTP
	track     *webrtc.TrackLocalStaticRTP
	mu        sync.RWMutex
	closed    chan struct{}
}

func NewWebRTCBroadcaster(stationID string, natIP string) (*WebRTCBroadcaster, error) {
	// Listen on an OS-allocated dynamic local UDP port to prevent port collisions
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	port := conn.LocalAddr().(*net.UDPAddr).Port
	log.Printf("[WebRTC] Resolved local UDP listener on port %d for station %s", port, stationID)

	// Create local audio track (Opus, 48000Hz, stereo) standard WebRTC track
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"streamer",
	)
	if err != nil {
		conn.Close()
		return nil, err
	}

	b := &WebRTCBroadcaster{
		stationID: stationID,
		natIP:     natIP,
		udpConn:   conn,
		udpPort:   port,
		peers:     make(map[*webrtc.PeerConnection]*webrtc.TrackLocalStaticRTP),
		track:     track,
		closed:    make(chan struct{}),
	}

	// Start reading UDP RTP packets from FFmpeg
	go b.readLoop()

	return b, nil
}

func (b *WebRTCBroadcaster) UDPPort() int {
	return b.udpPort
}

func (b *WebRTCBroadcaster) Close() {
	close(b.closed)
	b.udpConn.Close()

	b.mu.Lock()
	defer b.mu.Unlock()
	for pc := range b.peers {
		pc.Close()
	}
	b.peers = make(map[*webrtc.PeerConnection]*webrtc.TrackLocalStaticRTP)
}

func (b *WebRTCBroadcaster) readLoop() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-b.closed:
			return
		default:
		}

		n, _, err := b.udpConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-b.closed:
				return
			default:
				log.Printf("[WebRTC] Error reading UDP: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		if n > 0 {
			packet := &rtp.Packet{}
			if err := packet.Unmarshal(buf[:n]); err == nil {
				// Inject the structured RTP packet directly to the Track
				if writeErr := b.track.WriteRTP(packet); writeErr != nil {
					// Ignore write errors to closed/silent peers
				}
			} else {
				log.Printf("[WebRTC] Error unmarshaling RTP: %v", err)
			}
		}
	}
}

// HandleOffer takes an SDP Offer, sets up a PeerConnection, binds the track, and returns the SDP Answer.
func (b *WebRTCBroadcaster) HandleOffer(sdpOffer string) (string, error) {
	// Configure ICE Servers (STUN only for light-weight local/WAN setups)
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Configure ICE SettingEngine to limit UDP port allocation for firewall simplicity
	se := webrtc.SettingEngine{}
	se.SetEphemeralUDPPortRange(50000, 50100)
	if b.natIP != "" {
		se.SetNAT1To1IPs([]string{b.natIP}, webrtc.ICECandidateTypeHost)
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return "", err
	}

	// Add static Opus track to peer connection
	sender, err := pc.AddTrack(b.track)
	if err != nil {
		pc.Close()
		return "", err
	}

	// Keep-alive reading from the RTCP track (ensures WebRTC connection doesn't stall)
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := sender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Handle Peer Connection status changes
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("[WebRTC] Peer Connection state changed to: %s", s)
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			b.mu.Lock()
			delete(b.peers, pc)
			b.mu.Unlock()
			pc.Close()
			log.Printf("[WebRTC] Peer Connection closed for station %s", b.stationID)
		}
	})

	// Handle ICE Connection status changes
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		log.Printf("[WebRTC] ICE Connection state changed to: %s", s)
	})

	// Set remote SDP description
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpOffer,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		return "", err
	}

	// Create SDP Answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return "", err
	}

	// Set local SDP description
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return "", err
	}

	// Wait for server-side ICE candidates gathering to complete so they are embedded in the SDP
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	// Retrieve the finalized SDP containing all network candidates
	finalAnswer := pc.LocalDescription()

	// Save peer connection securely
	b.mu.Lock()
	b.peers[pc] = b.track
	b.mu.Unlock()

	return finalAnswer.SDP, nil
}

// HandleIngressOffer sets up a WebRTC connection to receive audio from the browser,
// decodes it using a dynamic FFmpeg pipeline, and writes the PCM stream to Channel 0 (Priority Channel) with Auto-Ducking.
func (b *WebRTCBroadcaster) HandleIngressOffer(sdpOffer string, mixer *AudioMixer) (string, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	se := webrtc.SettingEngine{}
	se.SetEphemeralUDPPortRange(50000, 50100)
	if b.natIP != "" {
		se.SetNAT1To1IPs([]string{b.natIP}, webrtc.ICECandidateTypeHost)
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return "", err
	}

	// Tell PeerConnection we want to receive audio
	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		pc.Close()
		return "", err
	}

	var ffmpegCmd *exec.Cmd
	var localUDP *net.UDPConn
	var doneOnce sync.Once

	cleanupIngress := func() {
		doneOnce.Do(func() {
			log.Printf("[WebRTC-Ingress] Cleaning up ingress session for station %s...", b.stationID)
			if ffmpegCmd != nil && ffmpegCmd.Process != nil {
				ffmpegCmd.Process.Kill()
			}
			if localUDP != nil {
				localUDP.Close()
			}
			if mixer != nil && len(mixer.Channels) > 0 {
				mixer.Channels[0].mu.Lock()
				mixer.Channels[0].active = false
				mixer.Channels[0].buffer = nil
				mixer.Channels[0].mu.Unlock()
				mixer.Channels[0].SetLabel("")
			}
		})
	}

	// When an audio track is received from the browser
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[WebRTC-Ingress] Received remote track: ID=%s, PayloadType=%d, Kind=%s", track.ID(), track.PayloadType(), track.Kind().String())

		// Start a local UDP socket on a random port
		localAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		if err != nil {
			log.Printf("[WebRTC-Ingress] Error resolving local UDP: %v", err)
			cleanupIngress()
			return
		}
		conn, err := net.ListenUDP("udp", localAddr)
		if err != nil {
			log.Printf("[WebRTC-Ingress] Error listening on local UDP: %v", err)
			cleanupIngress()
			return
		}
		localUDP = conn
		localPort := conn.LocalAddr().(*net.UDPAddr).Port

		// Write temporary SDP file for FFmpeg to know how to decode
		sdpPath := filepath.Join(os.TempDir(), fmt.Sprintf("webrtc_ingress_%s_%d.sdp", b.stationID, localPort))
		sdpContent := fmt.Sprintf(`v=0
o=- 0 0 IN IP4 127.0.0.1
s=WebRTC Ingress
c=IN IP4 127.0.0.1
t=0 0
m=audio %d RTP/AVP %d
a=rtpmap:%d opus/48000/2
`, localPort, track.PayloadType(), track.PayloadType())

		if err := os.WriteFile(sdpPath, []byte(sdpContent), 0644); err != nil {
			log.Printf("[WebRTC-Ingress] Error writing SDP file: %v", err)
			cleanupIngress()
			return
		}

		// Forward RTP packets to local UDP
		go func() {
			defer cleanupIngress()
			defer os.Remove(sdpPath)

			for {
				rtpPacket, _, err := track.ReadRTP()
				if err != nil {
					log.Printf("[WebRTC-Ingress] Remote track read stopped: %v", err)
					return
				}
				payload, err := rtpPacket.Marshal()
				if err == nil {
					conn.Write(payload)
				}
			}
		}()

		// Start FFmpeg to decode local UDP RTP to raw PCM S16LE Stereo 44.1kHz
		ffmpegCmd = exec.Command(
			"ffmpeg",
			"-protocol_whitelist", "file,udp,rtp",
			"-i", sdpPath,
			"-f", "s16le",
			"-ar", "44100",
			"-ac", "2",
			"-",
		)

		stdoutPipe, err := ffmpegCmd.StdoutPipe()
		if err != nil {
			log.Printf("[WebRTC-Ingress] Error getting FFmpeg stdout: %v", err)
			cleanupIngress()
			return
		}

		if err := ffmpegCmd.Start(); err != nil {
			log.Printf("[WebRTC-Ingress] Error starting FFmpeg: %v", err)
			cleanupIngress()
			return
		}

		// Read decoded PCM data from FFmpeg stdout and write directly into Mixer Channel 0
		go func() {
			defer cleanupIngress()
			pcmBuf := make([]byte, 4096)
			mixer.Channels[0].SetLabel("WebRTC-Announcer")

			for {
				n, err := stdoutPipe.Read(pcmBuf)
				if n > 0 {
					mixer.Channels[0].Write(pcmBuf[:n])
				}
				if err != nil {
					break
				}
			}
			ffmpegCmd.Wait()
		}()
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("[WebRTC-Ingress] Peer Connection state changed to: %s", s)
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			cleanupIngress()
			pc.Close()
		}
	})

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpOffer,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		return "", err
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return "", err
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return "", err
	}

	// Wait for server-side ICE candidates gathering to complete so they are embedded in the SDP
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	// Retrieve the finalized SDP containing all network candidates
	finalAnswer := pc.LocalDescription()

	return finalAnswer.SDP, nil
}
