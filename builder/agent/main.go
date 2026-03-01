package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

const ServerURL = "ws://192.168.88.127:8000/ws/agent/agent1"

func main() {
	log.Println("Connecting to signaling server:", ServerURL)

	ws, _, err := websocket.DefaultDialer.Dial(ServerURL, nil)
	if err != nil {
		log.Fatal("WebSocket connect error:", err)
	}
	defer ws.Close()

	log.Println("Connected to signaling server")

	// Thread-safe writer
	writeChan := make(chan []byte, 100)
	go func() {
		for msg := range writeChan {
			if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Println("WebSocket write error:", err)
				return
			}
		}
	}()

	// Map of active PeerConnections
	pcs := make(map[string]*webrtc.PeerConnection)
	var pcsLock sync.Mutex

	// Shared video track (sent to all viewers)
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"rmm",
	)
	if err != nil {
		log.Fatal("Track creation error:", err)
	}

	// Start FFmpeg streaming loop
	go startFFmpeg(videoTrack)

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("WebSocket read error:", err)
			break
		}

		// Try to parse as SDP
		var sdp webrtc.SessionDescription
		if err := json.Unmarshal(msg, &sdp); err == nil && sdp.Type == webrtc.SDPTypeOffer {
			log.Println("Received new offer (viewer refresh or connect)")

			viewerID := "viewer" // single viewer system

			pcsLock.Lock()
			if oldPC, ok := pcs[viewerID]; ok {
				log.Println("Closing old PeerConnection")
				oldPC.Close()
			}

			pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{URLs: []string{"stun:stun.l.google.com:19302"}},
				},
			})
			if err != nil {
				log.Println("PeerConnection error:", err)
				pcsLock.Unlock()
				continue
			}

			pcs[viewerID] = pc
			pcsLock.Unlock()

			_, err = pc.AddTrack(videoTrack)
			if err != nil {
				log.Println("AddTrack error:", err)
				continue
			}

			pc.OnICECandidate(func(c *webrtc.ICECandidate) {
				if c != nil {
					payload, _ := json.Marshal(c.ToJSON())
					writeChan <- payload
				}
			})

			if err := pc.SetRemoteDescription(sdp); err != nil {
				log.Println("SetRemoteDescription error:", err)
				continue
			}

			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				log.Println("CreateAnswer error:", err)
				continue
			}

			if err := pc.SetLocalDescription(answer); err != nil {
				log.Println("SetLocalDescription error:", err)
				continue
			}

			payload, _ := json.Marshal(answer)
			writeChan <- payload

			log.Println("Sent SDP answer")
			continue
		}

		// Try to parse as ICE
		var ice map[string]interface{}
		if err := json.Unmarshal(msg, &ice); err == nil && ice["candidate"] != nil {

			candidate := webrtc.ICECandidateInit{
				Candidate: ice["candidate"].(string),
			}

			if ice["sdpMid"] != nil {
				sdpMid := ice["sdpMid"].(string)
				candidate.SDPMid = &sdpMid
			}
			if ice["sdpMLineIndex"] != nil {
				idx := uint16(ice["sdpMLineIndex"].(float64))
				candidate.SDPMLineIndex = &idx
			}

			pcsLock.Lock()
			for _, pc := range pcs {
				if err := pc.AddICECandidate(candidate); err != nil {
					log.Println("AddICECandidate error:", err)
				}
			}
			pcsLock.Unlock()
		}
	}

	log.Println("Agent shutting down")
}

func startFFmpeg(videoTrack *webrtc.TrackLocalStaticSample) {
	for {
		log.Println("Starting FFmpeg...")

		cmd := exec.Command(
			"C:\\ffmpeg\\bin\\ffmpeg.exe",
			"-loglevel", "warning",
			"-f", "gdigrab",
			"-framerate", "30",
			"-i", "desktop",
			"-vcodec", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-f", "h264",
			"pipe:1",
		)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Println("FFmpeg stdout error:", err)
			time.Sleep(3 * time.Second)
			continue
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			log.Println("FFmpeg stderr error:", err)
			time.Sleep(3 * time.Second)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Println("FFmpeg start error:", err)
			time.Sleep(3 * time.Second)
			continue
		}

		log.Println("FFmpeg started (PID:", cmd.Process.Pid, ")")

		// Log FFmpeg stderr
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				log.Println("[FFmpeg]", scanner.Text())
			}
		}()

		reader := bufio.NewReader(stdout)

		for {
			nal, err := readNALUnit(reader)
			if err != nil {
				log.Println("NAL read error:", err)
				break
			}

			err = videoTrack.WriteSample(media.Sample{
				Data:     nal,
				Duration: time.Second / 30,
			})
			if err != nil {
				log.Println("WriteSample error:", err)
				break
			}
		}

		log.Println("FFmpeg exited. Restarting in 3 seconds...")
		cmd.Process.Kill()
		cmd.Wait()
		time.Sleep(3 * time.Second)
	}
}

func readNALUnit(r *bufio.Reader) ([]byte, error) {
	var nal []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		nal = append(nal, b)
		if len(nal) >= 4 && string(nal[len(nal)-4:]) == "\x00\x00\x00\x01" {
			return nal[:len(nal)-4], nil
		}
	}
}
