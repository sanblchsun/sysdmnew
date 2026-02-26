package main

import (
	"log"
	"net"
	"os/exec"

	"github.com/pion/webrtc/v3"
)

func main() {

	// 1️⃣ Запуск FFmpeg (H.265 NVENC, 60fps)
	cmd := exec.Command(
		"ffmpeg",
		"-f", "gdigrab",
		"-framerate", "60",
		"-i", "desktop",
		"-c:v", "hevc_nvenc",
		"-preset", "p3",
		"-tune", "ull",
		"-b:v", "6M",
		"-g", "60",
		"-f", "rtp",
		"rtp://127.0.0.1:5004",
	)

	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	// 2️⃣ RTP listener
	conn, err := net.ListenPacket("udp", "127.0.0.1:5004")
	if err != nil {
		log.Fatal(err)
	}

	// 3️⃣ WebRTC config
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatal(err)
	}

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeH265,
			ClockRate: 90000,
		},
		"video",
		"rmm",
	)
	if err != nil {
		log.Fatal(err)
	}

	_, err = pc.AddTrack(track)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		buffer := make([]byte, 1600)
		for {
			n, _, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			track.Write(buffer[:n])
		}
	}()

	select {}
}
