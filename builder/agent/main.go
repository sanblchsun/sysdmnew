package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
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

const serverURL = "ws://192.168.2.191:8000/ws/agent/agent1"

func main() {
	log.Printf("Connecting to signaling server: %s\n", serverURL)

	ws, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		log.Fatalf("WebSocket connect error: %v", err)
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
		"video", "rmm")
	if err != nil {
		log.Fatalf("Track creation error: %v", err)
	}

	go startFFmpeg(videoTrack)

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		if handleSDP(msg, writeChan, pcs, &pcsLock, videoTrack) {
			continue
		}
		handleICE(msg, pcs, &pcsLock)
	}
}

func handleSDP(msg []byte, writeChan chan []byte, pcs map[string]*webrtc.PeerConnection, pcsLock *sync.Mutex, videoTrack *webrtc.TrackLocalStaticSample) bool {
	var sdp webrtc.SessionDescription
	if err := json.Unmarshal(msg, &sdp); err != nil || sdp.Type != webrtc.SDPTypeOffer {
		return false
	}

	pcsLock.Lock()
	if old, ok := pcs["viewer"]; ok {
		old.Close()
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		pcsLock.Unlock()
		log.Printf("PeerConnection error: %v", err)
		return true
	}

	pcs["viewer"] = pc
	pcsLock.Unlock()

	_, err = pc.AddTrack(videoTrack)
	if err != nil {
		log.Printf("AddTrack error: %v", err)
		return true
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			log.Println("Control channel open")
			sendScreenInfo(dc)
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			handleControl(msg.Data)
		})
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			if payload, err := json.Marshal(c.ToJSON()); err == nil {
				writeChan <- payload
			}
		}
	})

	if err = pc.SetRemoteDescription(sdp); err != nil {
		log.Printf("SetRemoteDescription error: %v", err)
		return true
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("CreateAnswer error: %v", err)
		return true
	}
	pc.SetLocalDescription(answer)
	payload, _ := json.Marshal(answer)
	writeChan <- payload
	return true
}

func handleICE(msg []byte, pcs map[string]*webrtc.PeerConnection, pcsLock *sync.Mutex) {
	var ice webrtc.ICECandidateInit
	if err := json.Unmarshal(msg, &ice); err != nil || ice.Candidate == "" {
		return
	}
	pcsLock.Lock()
	defer pcsLock.Unlock()
	for _, pc := range pcs {
		pc.AddICECandidate(ice)
	}
}

func sendScreenInfo(dc *webrtc.DataChannel) {
	w, h := robotgo.GetScreenSize()
	info := map[string]any{"type": "screen_info", "width": w, "height": h}
	b, _ := json.Marshal(info)
	dc.Send(b)
}

func startFFmpeg(videoTrack *webrtc.TrackLocalStaticSample) {
	cmd := exec.Command(
		`C:\ffmpeg\bin\ffmpeg.exe`,
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
		"-f", "h264", "-",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("FFmpeg stdout error: %v", err)
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start FFmpeg: %v", err)
	}
	go logFFmpegOutput(stderr)
	streamVideo(stdout, videoTrack)
}

func logFFmpegOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("[FFmpeg] %s", scanner.Text())
	}
}

func streamVideo(r io.Reader, videoTrack *webrtc.TrackLocalStaticSample) {
	reader := bufio.NewReader(r)
	buffer := make([]byte, 0, 1<<16)
	tmp := make([]byte, 4096)

	for {
		n, err := reader.Read(tmp)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("FFmpeg read error: %v", err)
			}
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
			videoTrack.WriteSample(media.Sample{Data: nalu, Duration: time.Second / 30})
			buffer = buffer[next:]
		}
	}
}

func findStartCode(data []byte) int {
	for i := 0; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			return i
		}
	}
	return -1
}

func handleControl(data []byte) {
	var control map[string]any
	if err := json.Unmarshal(data, &control); err != nil {
		return
	}

	switch control["type"] {
	case "mouse_move":
		robotgo.MoveMouse(int(control["x"].(float64)), int(control["y"].(float64)))
	case "mouse_down":
		mouseClick("down", int(control["button"].(float64)))
	case "mouse_up":
		mouseClick("up", int(control["button"].(float64)))
	case "key_down":
		if key := mapKey(control["key"].(string)); key != "" {
			robotgo.KeyDown(key)
		}
	case "key_up":
		if key := mapKey(control["key"].(string)); key != "" {
			robotgo.KeyUp(key)
		}
	}
}

func mouseClick(action string, b int) {
	btns := []string{"left", "center", "right"}
	if b >= 0 && b < len(btns) {
		if action == "down" {
			robotgo.MouseDown(btns[b])
		} else {
			robotgo.MouseUp(btns[b])
		}
	}
}

func mapKey(code string) string {
	switch {
	case strings.HasPrefix(code, "Key"):
		return strings.ToLower(code[3:])
	case strings.HasPrefix(code, "Digit"):
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
