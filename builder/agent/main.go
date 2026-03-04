// builder/agent/main.go
package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

const ServerURL = "ws://192.168.2.191:8000/ws/agent/agent1"

func main() {
	log.Println("Connecting to signaling server:", ServerURL)

	ws, _, err := websocket.DefaultDialer.Dial(ServerURL, nil)
	if err != nil {
		log.Fatal("WebSocket connect error:", err)
	}
	defer ws.Close()

	log.Println("Connected to signaling server")

	writeChan := make(chan []byte, 100)
	go func() {
		for msg := range writeChan {
			ws.WriteMessage(websocket.TextMessage, msg)
		}
	}()

	pcs := make(map[string]*webrtc.PeerConnection)
	var pcsLock sync.Mutex

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"rmm",
	)
	if err != nil {
		log.Fatal("Track creation error:", err)
	}

	go startFFmpeg(videoTrack)

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Println("WebSocket read error:", err)
			break
		}

		var sdp webrtc.SessionDescription
		if err := json.Unmarshal(msg, &sdp); err == nil && sdp.Type == webrtc.SDPTypeOffer {

			viewerID := "viewer"

			pcsLock.Lock()
			if oldPC, ok := pcs[viewerID]; ok {
				oldPC.Close()
			}

			pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{URLs: []string{"stun:stun.l.google.com:19302"}},
				},
			})
			if err != nil {
				pcsLock.Unlock()
				continue
			}

			pcs[viewerID] = pc
			pcsLock.Unlock()

			_, err = pc.AddTrack(videoTrack)
			if err != nil {
				continue
			}

			pc.OnDataChannel(func(dc *webrtc.DataChannel) {

				dc.OnOpen(func() {
					log.Println("Control channel open")

					w, h := robotgo.GetScreenSize()

					info := map[string]interface{}{
						"type":   "screen_info",
						"width":  w,
						"height": h,
					}

					payload, _ := json.Marshal(info)
					dc.Send(payload)
				})

				dc.OnMessage(func(msg webrtc.DataChannelMessage) {
					handleControl(msg.Data)
				})
			})

			pc.OnICECandidate(func(c *webrtc.ICECandidate) {
				if c != nil {
					payload, _ := json.Marshal(c.ToJSON())
					writeChan <- payload
				}
			})

			pc.SetRemoteDescription(sdp)
			answer, _ := pc.CreateAnswer(nil)
			pc.SetLocalDescription(answer)

			payload, _ := json.Marshal(answer)
			writeChan <- payload
		}

		var ice map[string]interface{}
		if err := json.Unmarshal(msg, &ice); err == nil && ice["candidate"] != nil {

			candidate := webrtc.ICECandidateInit{
				Candidate: ice["candidate"].(string),
			}

			pcsLock.Lock()
			for _, pc := range pcs {
				pc.AddICECandidate(candidate)
			}
			pcsLock.Unlock()
		}
	}
}

func startFFmpeg(videoTrack *webrtc.TrackLocalStaticSample) {
	log.Println("Starting FFmpeg...")

	cmd := exec.Command(
		"C:\\ffmpeg\\bin\\ffmpeg.exe",
		"-f", "gdigrab",
		"-framerate", "60",
		"-draw_mouse", "0",
		"-i", "desktop",
		"-vcodec", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", "30",
		"-keyint_min", "30",
		"-f", "h264",
		"-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal("FFmpeg stdout error:", err)
	}

	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		log.Fatal("Failed to start FFmpeg:", err)
	}

	log.Println("FFmpeg started (PID:", cmd.Process.Pid, ")")

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Println("[FFmpeg]", scanner.Text())
		}
	}()

	reader := bufio.NewReader(stdout)

	var buffer []byte
	tmp := make([]byte, 4096)

	for {
		n, err := reader.Read(tmp)
		if err != nil {
			log.Println("FFmpeg read error:", err)
			break
		}

		buffer = append(buffer, tmp[:n]...)

		for {
			start := findStartCode(buffer)
			if start == -1 {
				break
			}

			next := findStartCode(buffer[start+4:])
			if next == -1 {
				break
			}

			next += start + 4

			nalu := buffer[start:next]

			err = videoTrack.WriteSample(media.Sample{
				Data:     nalu,
				Duration: time.Second / 30,
			})
			if err != nil {
				log.Println("Track write error:", err)
				return
			}

			buffer = buffer[next:]
		}
	}
}

func findStartCode(data []byte) int {
	for i := 0; i < len(data)-3; i++ {
		if data[i] == 0x00 &&
			data[i+1] == 0x00 &&
			data[i+2] == 0x00 &&
			data[i+3] == 0x01 {
			return i
		}
	}
	return -1
}

func handleControl(data []byte) {
	var control map[string]interface{}
	if err := json.Unmarshal(data, &control); err != nil {
		return
	}

	switch control["type"] {

	case "mouse_move":
		x := int(control["x"].(float64))
		y := int(control["y"].(float64))
		robotgo.MoveMouse(x, y)

	case "mouse_down":
		switch int(control["button"].(float64)) {
		case 0:
			robotgo.MouseDown("left")
		case 1:
			robotgo.MouseDown("center")
		case 2:
			robotgo.MouseDown("right")
		}

	case "mouse_up":
		switch int(control["button"].(float64)) {
		case 0:
			robotgo.MouseUp("left")
		case 1:
			robotgo.MouseUp("center")
		case 2:
			robotgo.MouseUp("right")
		}

	case "key_down":
		key := mapKey(control["key"].(string))
		if key != "" {
			robotgo.KeyDown(key)
		}

	case "key_up":
		key := mapKey(control["key"].(string))
		if key != "" {
			robotgo.KeyUp(key)
		}
	}
}

func mapKey(code string) string {

	if strings.HasPrefix(code, "Key") {
		return strings.ToLower(code[3:])
	}

	if strings.HasPrefix(code, "Digit") {
		return code[5:]
	}

	switch code {
	case "Space":
		return "space"
	case "Enter":
		return "enter"
	case "Escape":
		return "esc"
	case "ShiftLeft", "ShiftRight":
		return "shift"
	case "ControlLeft", "ControlRight":
		return "ctrl"
	case "AltLeft", "AltRight":
		return "alt"
	case "ArrowLeft":
		return "left"
	case "ArrowRight":
		return "right"
	case "ArrowUp":
		return "up"
	case "ArrowDown":
		return "down"
	default:
		return ""
	}
}
