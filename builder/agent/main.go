package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os/exec"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

const ServerURL = "ws://192.168.88.127:8000/ws/agent/agent1"

func main() {
	ws, _, err := websocket.DefaultDialer.Dial(ServerURL, nil)
	if err != nil {
		log.Fatal("WebSocket connect error:", err)
	}
	log.Println("Connected to signaling server:", ServerURL)

	writeChan := make(chan []byte, 100)
	go func() {
		for msg := range writeChan {
			if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Println("WebSocket write error:", err)
				return
			}
		}
	}()

	// Map of PeerConnections per viewer
	pcs := make(map[string]*webrtc.PeerConnection)

	// Single video track for all viewers
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"rmm",
	)
	if err != nil {
		log.Fatal(err)
	}

	// --- FFmpeg ---
	cmd := exec.Command(
		"C:\\ffmpeg\\bin\\ffmpeg.exe",
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
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		log.Fatal("FFmpeg start error:", err)
	}
	log.Println("FFmpeg started")

	go func() {
		reader := bufio.NewReader(stdout)
		for {
			nal, err := readNALUnit(reader)
			if err != nil {
				log.Println("NAL read error:", err)
				return
			}
			err = videoTrack.WriteSample(media.Sample{
				Data:     nal,
				Duration: time.Second / 30,
			})
			if err != nil {
				log.Println("WriteSample error:", err)
				return
			}
		}
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("WebSocket read error:", err)
			break
		}

		// Try to decode as SDP offer
		var sdp webrtc.SessionDescription
		if err := json.Unmarshal(msg, &sdp); err == nil && sdp.Type == webrtc.SDPTypeOffer {
			log.Println("Received offer")

			viewerID := "viewer" // single viewer; can parse from message if multiple
			// Close old PC if exists
			if oldPC, ok := pcs[viewerID]; ok {
				oldPC.Close()
			}

			pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{URLs: []string{"stun:stun.l.google.com:19302"}},
				},
			})
			if err != nil {
				log.Println("PeerConnection error:", err)
				continue
			}
			pcs[viewerID] = pc

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
			continue
		}

		// ICE candidates
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

			// Apply to all active PCs
			for _, pc := range pcs {
				if err := pc.AddICECandidate(candidate); err != nil {
					log.Println("AddICECandidate error:", err)
				}
			}
		}
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
