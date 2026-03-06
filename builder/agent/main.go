// builder/agent/main.go
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// --- Конфигурация ---
const serverURL = "ws://192.168.2.191:8000/ws/agent/agent1"

func init() {
	if runtime.GOOS == "windows" {
		initWindowsDPI()
	}
}

// --- Глобальные переменные ---
var (
	actualScreenWidth  int
	actualScreenHeight int
	activeDataChannel  *webrtc.DataChannel
)

// --- Отправка информации об экране ---
func sendScreenInfo(dc *webrtc.DataChannel) {
	// Предпочитаем физическое DPI‑разрешение
	w, h := getPhysicalScreenSize()
	if w == 0 || h == 0 {
		w, h = detectResolution()
	}
	actualScreenWidth, actualScreenHeight = w, h

	info := map[string]interface{}{
		"type":   "screen_info",
		"width":  w,
		"height": h,
	}
	b, _ := json.Marshal(info)
	_ = dc.SendText(string(b))
	log.Printf("[SCREEN] Reported size: %dx%d", w, h)
}

// --- Определение разрешения через ffmpeg ---
func detectResolution() (int, int) {
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-f", "gdigrab", "-i", "desktop", "-vframes", "1", "-f", "null", "-"}
	} else {
		args = []string{"-f", "x11grab", "-i", ":0.0", "-vframes", "1", "-f", "null", "-"}
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err == nil {
		re := regexp.MustCompile(`(\d{3,5})x(\d{3,5})`)
		if m := re.FindStringSubmatch(string(out)); len(m) == 3 {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			return w, h
		}
	}
	w, h := robotgo.GetScreenSize()
	return w, h
}

// --- Основной запуск ---
func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Connecting to signaling server: %s\n", serverURL)

	ws, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		log.Fatalf("WebSocket connect error: %v", err)
	}
	defer ws.Close()
	log.Println("Connected")

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
		log.Fatalf("Track create error: %v", err)
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

// --- SDP/ICE ---
func handleSDP(msg []byte, out chan []byte, pcs map[string]*webrtc.PeerConnection,
	lock *sync.Mutex, videoTrack *webrtc.TrackLocalStaticSample) bool {

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

	_ = pc.SetRemoteDescription(sdp)

	answer, _ := pc.CreateAnswer(nil)
	_ = pc.SetLocalDescription(answer)
	payload, _ := json.Marshal(answer)
	out <- payload

	return true
}

func newPeerConnection(out chan []byte,
	videoTrack *webrtc.TrackLocalStaticSample) (*webrtc.PeerConnection, error) {

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
		activeDataChannel = dc
		dc.OnOpen(func() {
			log.Println("DataChannel opened")
			sendScreenInfo(dc)
			startScreenWatcher(dc)
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

// --- Монитор размеров экрана ---
func startScreenWatcher(dc *webrtc.DataChannel) {
	go func() {
		prevW, prevH := actualScreenWidth, actualScreenHeight
		for {
			time.Sleep(2 * time.Second)
			w, h := getPhysicalScreenSize()
			if w == 0 || h == 0 {
				w, h = detectResolution()
			}
			if w != prevW || h != prevH {
				prevW, prevH = w, h
				actualScreenWidth, actualScreenHeight = w, h
				info := map[string]interface{}{
					"type":   "screen_info",
					"width":  w,
					"height": h,
				}
				b, _ := json.Marshal(info)
				_ = dc.SendText(string(b))
				log.Printf("[SCREEN] Updated: %dx%d", w, h)
			}
		}
	}()
}

// --- FFmpeg видеопоток ---
func startFFmpeg(videoTrack *webrtc.TrackLocalStaticSample) {
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-f", "gdigrab", "-framerate", "60", "-draw_mouse", "0", "-i", "desktop"}
	} else {
		args = []string{"-f", "x11grab", "-framerate", "30", "-draw_mouse", "0", "-i", ":0.0"}
	}
	args = append(args,
		"-vcodec", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", "30", "-keyint_min", "30", "-f", "h264", "-",
	)
	cmd := exec.Command("ffmpeg", args...)
	stdout, _ := cmd.StdoutPipe()
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
				log.Printf("FFmpeg read error: %v", err)
			}
			return
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

// --- Управление вводом ---
func handleControl(data []byte) {
	var ctl map[string]interface{}
	if err := json.Unmarshal(data, &ctl); err != nil {
		log.Printf("[CONTROL] bad json: %v", err)
		return
	}

	t, _ := ctl["type"].(string)
	log.Printf("[CONTROL] %+v", ctl)

	switch t {

	case "request_screen_info":
		if activeDataChannel != nil {
			sendScreenInfo(activeDataChannel)
		}

	case "mouse_move":
		x, okX := ctl["x"].(float64)
		y, okY := ctl["y"].(float64)
		if !okX || !okY {
			return
		}
		w, h := robotgo.GetScreenSize()
		pw, ph := getPhysicalScreenSize()
		if pw > 0 && ph > 0 {
			w, h = pw, ph
		}
		tw := actualScreenWidth
		th := actualScreenHeight
		if tw == 0 || th == 0 {
			tw, th = w, h
		}
		sx := float64(w) / float64(tw)
		sy := float64(h) / float64(th)
		safeX := clampInt(int(x*sx), 0, w-1)
		safeY := clampInt(int(y*sy), 0, h-1)
		robotgo.MoveMouse(safeX, safeY)

	case "mouse_down", "mouse_up":
		btn := int(ctl["button"].(float64))
		names := []string{"left", "middle", "right"}
		if btn < 0 || btn >= len(names) {
			return
		}
		if t == "mouse_down" {
			robotgo.MouseDown(names[btn])
		} else {
			robotgo.MouseUp(names[btn])
		}

	case "mouse_click":
		btn := int(ctl["button"].(float64))
		names := []string{"left", "middle", "right"}
		if btn >= 0 && btn < len(names) {
			robotgo.Click(names[btn])
		}

	default:
		log.Printf("[CONTROL] Unhandled event %s", t)
	}
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
