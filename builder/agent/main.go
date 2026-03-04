package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// --- Configuration constants ---
const serverURL = "ws://192.168.2.191:8000/ws/agent/agent1"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
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
		"video", "rmm",
	)
	if err != nil {
		log.Fatalf("Failed creating track: %v", err)
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

// --- SDP and ICE handling ---

func handleSDP(msg []byte, out chan []byte, pcs map[string]*webrtc.PeerConnection, lock *sync.Mutex, videoTrack *webrtc.TrackLocalStaticSample) bool {
	var sdp webrtc.SessionDescription
	if err := json.Unmarshal(msg, &sdp); err != nil || sdp.Type != webrtc.SDPTypeOffer {
		return false
	}

	lock.Lock()
	if old, ok := pcs["viewer"]; ok {
		old.Close()
	}
	pc, err := newPeerConnection(out, videoTrack)
	if err != nil {
		lock.Unlock()
		log.Printf("PeerConnection error: %v", err)
		return true
	}
	pcs["viewer"] = pc
	lock.Unlock()

	if err := pc.SetRemoteDescription(sdp); err != nil {
		log.Printf("SetRemoteDescription error: %v", err)
		return true
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("CreateAnswer error: %v", err)
		return true
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		log.Printf("SetLocalDescription error: %v", err)
		return true
	}
	payload, _ := json.Marshal(answer)
	out <- payload
	return true
}

func newPeerConnection(out chan []byte, videoTrack *webrtc.TrackLocalStaticSample) (*webrtc.PeerConnection, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		return nil, err
	}

	if _, err := pc.AddTrack(videoTrack); err != nil {
		log.Printf("AddTrack error: %v", err)
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			log.Println("Control channel opened")
			sendScreenInfo(dc)
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			handleControl(msg.Data)
		})
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			if payload, err := json.Marshal(c.ToJSON()); err == nil {
				out <- payload
			}
		}
	})
	return pc, nil
}

func handleICE(msg []byte, pcs map[string]*webrtc.PeerConnection, lock *sync.Mutex) {
	var ice webrtc.ICECandidateInit
	if err := json.Unmarshal(msg, &ice); err != nil || ice.Candidate == "" {
		return
	}
	lock.Lock()
	defer lock.Unlock()
	for _, pc := range pcs {
		_ = pc.AddICECandidate(ice)
	}
}

// --- Screen control helpers ---

func sendScreenInfo(dc *webrtc.DataChannel) {
	w, h := robotgo.GetScreenSize()
	info := map[string]interface{}{"type": "screen_info", "width": w, "height": h}
	b, _ := json.Marshal(info)
	_ = dc.Send(b)
}

// --- FFmpeg video streaming ---

func startFFmpeg(videoTrack *webrtc.TrackLocalStaticSample) {
	// OS-specific FFmpeg input
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-f", "gdigrab", "-framerate", "30", "-draw_mouse", "0", "-i", "desktop"}
	} else if runtime.GOOS == "linux" {
		// Linux: use x11grab
		args = []string{"-f", "x11grab", "-framerate", "30", "-draw_mouse", "0", "-i", ":0.0"}
	} else {
		// Fallback
		args = []string{"-f", "gdigrab", "-framerate", "30", "-draw_mouse", "0", "-i", "desktop"}
	}

	// common rest of args
	args = append(args,
		"-vcodec", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", "30",
		"-keyint_min", "30",
		"-f", "h264", "-",
	)

	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("FFmpeg pipe error: %v", err)
	}
	stderr, _ := cmd.StderrPipe()
	_ = cmd.Start()

	go logFFmpegOutput(stderr)
	streamVideo(stdout, videoTrack)
}

func logFFmpegOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Error") {
			log.Printf("[FFmpeg] %s", line)
		}
	}
}

func streamVideo(r io.Reader, videoTrack *webrtc.TrackLocalStaticSample) {
	reader := bufio.NewReader(r)
	buf := make([]byte, 0, 1<<16)
	tmp := make([]byte, 4096)

	for {
		n, err := reader.Read(tmp)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("FFmpeg stream read error: %v", err)
			}
			break
		}
		buf = append(buf, tmp[:n]...)
		for {
			start := findStartCode(buf)
			if start == -1 {
				break
			}
			next := findStartCode(buf[start+4:])
			if next == -1 {
				break
			}
			next += start + 4
			nalu := buf[start:next]
			_ = videoTrack.WriteSample(media.Sample{Data: nalu, Duration: time.Second / 30})
			buf = buf[next:]
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

// --- Remote input (keyboard/mouse) ---

func handleControl(data []byte) {
	var ctl map[string]interface{}
	if err := json.Unmarshal(data, &ctl); err != nil {
		return
	}

	switch ctl["type"] {
	case "mouse_move":
		x := int(ctl["x"].(float64))
		y := int(ctl["y"].(float64))
		w, h := robotgo.GetScreenSize()
		log.Printf("Координаты w: ", w)
		log.Printf("Координаты h: ", h)
		log.Printf("Координаты x: ", x)
		log.Printf("Координаты y: ", y)
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x > w {
			x = w
		}
		if y > h {
			y = h
		}
		robotgo.Move(x, y)
	case "mouse_down":
		mouseClick("down", int(ctl["button"].(float64)))
	case "mouse_up":
		mouseClick("up", int(ctl["button"].(float64)))
	case "key_down":
		if key := mapKey(ctl["key"].(string)); key != "" {
			robotgo.KeyDown(key)
		}
	case "key_up":
		if key := mapKey(ctl["key"].(string)); key != "" {
			robotgo.KeyUp(key)
		}
	}
}

func mouseClick(action string, btn int) {
	buttons := []string{"left", "center", "right"}
	if btn >= 0 && btn < len(buttons) {
		if action == "down" {
			robotgo.MouseDown(buttons[btn])
		} else {
			robotgo.MouseUp(buttons[btn])
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
