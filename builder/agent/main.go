package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt" // Добавлен для fmt.Sprintf
	"io"
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall" // Добавлен для syscall.SIGTERM
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// --- Конфигурация ---
const serverURL = "ws://192.168.88.127:8000/ws/agent/agent1"

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
	resolutionUpdates  = make(chan [2]int, 1)
	reResolution       = regexp.MustCompile(`(\d{3,5})x(\d{3,5})`) // Исправлено экранирование и MustCompile

	// Канал для сигнализации о перезапуске FFmpeg
	ffmpegRestartSignal = make(chan struct{}, 1) // Буферизованный, чтобы sender не блокировался
	ffmpegMutex         sync.Mutex               // Мьютекс для защиты currentFFmpegCmd
)

// sendScreenInfo отправляет информацию о разрешении экрана через DataChannel
func sendScreenInfo(dc *webrtc.DataChannel) {
	w, h := getPhysicalScreenSize()
	if w == 0 || h == 0 {
		w, h = detectResolution()
	}
	actualScreenWidth, actualScreenHeight = w, h // Обновляем глобальные переменные
	info := map[string]interface{}{
		"type":   "screen_info",
		"width":  w,
		"height": h,
	}
	b, _ := json.Marshal(info)
	_ = dc.SendText(string(b))
	log.Printf("[SCREEN] Reported size: %dx%d", w, h)
}

// detectResolution определяет разрешение экрана, используя сначала ffmpeg (если есть),
// затем robotgo.
func detectResolution() (int, int) {
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-f", "gdigrab", "-i", "desktop", "-vframes", "1", "-f", "null", "-"}
	} else {
		args = []string{"-f", "x11grab", "-i", ":0.0", "-vframes", "1", "-f", "null", "-"}
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err == nil {
		if m := reResolution.FindStringSubmatch(string(out)); len(m) == 3 {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			if w > 0 && h > 0 {
				log.Printf("[SCREEN] Detected resolution via ffmpeg: %dx%d", w, h)
				return w, h
			}
		}
	}
	// Если ffmpeg не смог определить или произошла ошибка, используем robotgo
	w, h := robotgo.GetScreenSize()
	log.Printf("[SCREEN] Fallback to RobotGo screen size: %dx%d", w, h)
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
			err := ws.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				return // Завершаем горутину, если запись не удалась (например, WS закрыт)
			}
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

	// Инициализация глобальных переменных разрешения перед первым запуском FFmpeg
	currentW, currentH := getPhysicalScreenSize()
	if currentW == 0 && currentH == 0 {
		currentW, currentH = detectResolution()
	}
	actualScreenWidth, actualScreenHeight = currentW, currentH
	log.Printf("Initial screen size set to: %dx%d", actualScreenWidth, actualScreenHeight)

	// Запускаем горутину, которая управляет жизненным циклом FFmpeg
	go manageFFmpegProcess(videoTrack)

	// Горутина, которая слушает обновления разрешения от FFmpeg
	go func() {
		for res := range resolutionUpdates {
			if actualScreenWidth == res[0] && actualScreenHeight == res[1] {
				// Разрешение совпадает с текущим, игнорируем
				continue
			}
			log.Printf("[FFmpeg] Video stream size detected %dx%d. Current: %dx%d. Signaling FFmpeg restart.",
				res[0], res[1], actualScreenWidth, actualScreenHeight)

			actualScreenWidth, actualScreenHeight = res[0], res[1] // Обновляем глобальные переменные

			// Сигнализируем о необходимости перезапустить FFmpeg
			select {
			case ffmpegRestartSignal <- struct{}{}:
				// Отправили сигнал, если канал не заблокирован
			default:
				// Канал заблокирован, значит, сигнал уже в очереди или процесс перезапуска уже идет
				log.Println("[FFmpeg] Restart signal already pending, skipping.")
			}

			// Также сразу отправляем информацию об изменении разрешения через DataChannel
			if activeDataChannel != nil {
				info := map[string]interface{}{
					"type":   "screen_info",
					"width":  res[0],
					"height": res[1],
				}
				b, _ := json.Marshal(info)
				_ = activeDataChannel.SendText(string(b))
				log.Printf("[FFmpeg] Sent updated screen_info: %dx%d", res[0], res[1])
			}
		}
	}()

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

// manageFFmpegProcess управляет жизненным циклом процесса FFmpeg.
// Он запускает FFmpeg, следит за ним и перезапускает по сигналу или при сбое.
func manageFFmpegProcess(videoTrack *webrtc.TrackLocalStaticSample) {
	var currentFFmpegCmd *exec.Cmd // Переменная для хранения текущего процесса FFmpeg, управляется этим менеджером
	for {
		log.Println("[FFmpeg Manager] Starting new FFmpeg process cycle...")

		// Создаем новый канал для завершения текущего цикла FFmpeg.
		// Важно, чтобы это был новый канал для каждого "запуска" FFmpeg.
		currentFFmpegQuitSignal := make(chan struct{}, 1)

		var cmd *exec.Cmd
		var stdout io.ReadCloser
		var stderr io.ReadCloser
		var err error

		var args []string
		if runtime.GOOS == "windows" {
			args = []string{"-f", "gdigrab", "-framerate", "60", "-draw_mouse", "0", "-i", "desktop"}
		} else {
			args = []string{"-f", "x11grab", "-framerate", "30", "-draw_mouse", "0", "-i", ":0.0"}
		}

		// Добавляем параметр разрешения, если оно известно.
		// Это важно для правильного масштабирования на стороне FFmpeg.
		if actualScreenWidth > 0 && actualScreenHeight > 0 {
			args = append(args, "-s", fmt.Sprintf("%dx%d", actualScreenWidth, actualScreenHeight))
			log.Printf("[FFmpeg Manager] Adding -s %dx%d to FFmpeg command.", actualScreenWidth, actualScreenHeight)
		}

		args = append(args,
			"-vcodec", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-pix_fmt", "yuv420p", "-g", "30", "-keyint_min", "30",
			"-f", "h264", "-",
		)

		log.Printf("[FFmpeg Manager] Executing command: ffmpeg %v", strings.Join(args, " "))
		cmd = exec.Command("ffmpeg", args...)

		// Сохраняем ссылку на текущий процесс
		ffmpegMutex.Lock()
		currentFFmpegCmd = cmd
		ffmpegMutex.Unlock()

		stdout, err = cmd.StdoutPipe()
		if err != nil {
			log.Printf("[FFmpeg Manager] FFmpeg stdout pipe error: %v", err)
			time.Sleep(5 * time.Second) // Пауза перед попыткой перезапуска
			continue                    // Начать следующий цикл
		}
		stderr, err = cmd.StderrPipe()
		if err != nil {
			log.Printf("[FFmpeg Manager] FFmpeg stderr pipe error: %v", err)
			_ = stdout.Close() // Закрываем, что открыли
			time.Sleep(5 * time.Second)
			continue // Начать следующий цикл
		}

		if err = cmd.Start(); err != nil {
			log.Printf("[FFmpeg Manager] FFmpeg command start error: %v", err)
			_ = stdout.Close()
			_ = stderr.Close()
			time.Sleep(5 * time.Second)
			continue // Начать следующий цикл
		}
		log.Println("[FFmpeg Manager] FFmpeg process started successfully.")

		// Запускаем горутины для чтения вывода FFmpeg с новым quit-каналом
		var wg sync.WaitGroup
		wg.Add(2) // Одна для stdout (видео), одна для stderr (логи и разрешение)

		go func() {
			defer wg.Done()
			parseFFmpegResolution(stderr, currentFFmpegQuitSignal)
			log.Printf("[FFmpeg Manager] Stderr parser exited.")
		}()
		go func() {
			defer wg.Done()
			streamVideo(stdout, videoTrack, currentFFmpegQuitSignal)
			log.Printf("[FFmpeg Manager] Video streamer exited.")
		}()

		// Дополнительная горутина для ожидания завершения процесса FFmpeg
		// и сигнализирования об этом.
		ffmpegWaitDone := make(chan struct{})
		go func() {
			defer close(ffmpegWaitDone) // Закрываем канал, когда горутина завершится
			if err := cmd.Wait(); err != nil {
				log.Printf("[FFmpeg Manager] FFmpeg process exited with error: %v", err)
			} else {
				log.Println("[FFmpeg Manager] FFmpeg process exited normally.")
			}
			// Важно: Когда процесс FFmpeg завершился (сам по себе или был убит),
			// мы должны сообщить горутинам чтения, что им тоже пора завершаться,
			// если они еще этого не сделали, чтобы избежать блокировок.
			select {
			case currentFFmpegQuitSignal <- struct{}{}:
			default: // Если канал уже пуст или закрыт
			}
		}()

		// Менеджер ждет либо внешнего сигнала на перезапуск, либо самопроизвольного завершения FFmpeg
		select {
		case <-ffmpegRestartSignal: // Получен внешний сигнал на перезапуск
			log.Println("[FFmpeg Manager] Received external restart signal. Terminating current FFmpeg process.")
		case <-ffmpegWaitDone: // FFmpeg процесс завершился сам по себе (например, была ошибка или источник закрылся)
			log.Println("[FFmpeg Manager] FFmpeg process finished its execution. Restarting cycle.")
			// В этом случае ничего дополнительно останавливать не нужно, он уже завершился
			// и горутина ffmpegWaitDone уже отработала сигнал currentFFmpegQuitSignal.
			// Просто ждем завершения дочерних горутин и начинаем новый цикл.
			wg.Wait()
			time.Sleep(1 * time.Second) // Даем системе немного времени
			continue                    // Начинаем новый цикл сразу
		}

		// Если мы здесь, значит, был получен ffmpegRestartSignal.
		log.Println("[FFmpeg Manager] Sending quit signal to reader goroutines for current FFmpeg.")
		select {
		case currentFFmpegQuitSignal <- struct{}{}:
			// Сигнал отправлен.
		default:
			log.Println("[FFmpeg Manager] currentFFmpegQuitSignal was blocked/closed, readers might be already finishing.")
		}

		// Теперь пытаемся корректно завершить сам процесс FFmpeg.
		// Используем currentFFmpegCmd, который был установлен для этого цикла.
		ffmpegMutex.Lock()
		if currentFFmpegCmd != nil && currentFFmpegCmd.Process != nil {
			log.Println("[FFmpeg Manager] Sending SIGTERM to FFmpeg process...")
			err := currentFFmpegCmd.Process.Signal(syscall.SIGTERM) // Отправляем SIGTERM для корректного завершения
			if err != nil {
				log.Printf("[FFmpeg Manager] Failed to send SIGTERM to FFmpeg: %v. Trying Kill.", err)
				_ = currentFFmpegCmd.Process.Kill() // Если SIGTERM не сработал, принудительно убиваем
			}
		}
		ffmpegMutex.Unlock()

		// Ждем, пока все дочерние горутины и сам процесс FFmpeg завершатся.
		// Это гарантирует, что ресурсы будут освобождены перед следующим запуском.
		wg.Wait()        // Ждем, пока горутины чтения завершатся
		<-ffmpegWaitDone // Ждем, пока горутина, ожидающая завершения FFmpeg, завершится
		log.Println("[FFmpeg Manager] All components of previous FFmpeg cycle stopped. Preparing for next run.")
		time.Sleep(1 * time.Second) // Небольшая пауза перед следующим запуском
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
		log.Printf("Closing old PeerConnection for 'viewer'.")
		_ = old.Close()
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

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("CreateAnswer error: %v", err)
		return true
	}
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
		dc.OnClose(func() {
			log.Println("DataChannel closed")
			// При закрытии DataChannel, может потребоваться остановить screen watcher,
			// чтобы не отправлять данные в недействующий канал.
			// Пока оставим рабочим, чтобы он мог отправить сигналы на перезапуск FFmpeg
			// даже без активного DC.
		})
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			if payload, err := json.Marshal(c.ToJSON()); err == nil {
				out <- payload
			}
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("Peer Connection State has changed to %s\n", s.String())
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			log.Printf("PeerConnection %s, detaching activeDataChannel.", s.String())
			activeDataChannel = nil // Сбрасываем ссылку на активный канал при разрыве соединения
		}
	})

	return pc, nil
}

func handleICE(msg []byte, pcs map[string]*webrtc.PeerConnection, lock *sync.Mutex) {
	var ice webrtc.ICECandidateInit
	if err := json.Unmarshal(msg, &ice); err != nil || ice.Candidate == "" {
		// Некоторые сообщения могут быть не ICE-кандидатами, игнорируем их
		return
	}
	lock.Lock()
	defer lock.Unlock()
	for _, pc := range pcs {
		if pc.RemoteDescription() != nil { // Кандидаты можно добавлять только после установки RemoteDescription
			err := pc.AddICECandidate(ice)
			if err != nil {
				log.Printf("AddICECandidate error: %v", err)
			}
		}
	}
}

// startScreenWatcher периодически проверяет изменение размеров экрана и сигнализирует о перезапуске FFmpeg.
func startScreenWatcher(dc *webrtc.DataChannel) {
	go func() {
		prevW, prevH := actualScreenWidth, actualScreenHeight
		for {
			time.Sleep(3 * time.Second)

			// Если activeDataChannel обнулен (соединение разорвано), то, возможно,
			// нет смысла продолжать наблюдение, пока не будет нового соединения.
			// Хотя, если хотим, чтобы FFmpeg всегда транслировал правильное разрешение,
			// то можно и продолжить.
			if activeDataChannel == nil {
				// Если DataChannel закрыт, мы можем прекратить наблюдение за экраном
				// или продолжить, но не отправлять info.
				// Я решил продолжить, чтобы FFmpeg перезапускался с правильным разрешением,
				// независимо от наличия активного соединения.
				// Однако, отправлять screen_info бессмысленно.
			}

			w, h := getPhysicalScreenSize()
			// Добавим fallback на detectResolution если getPhysicalScreenSize возвращает 0,
			// т.к. getPhysicalScreenSize может не всегда работать.
			if w == 0 || h == 0 {
				w, h = detectResolution()
			}

			if w != prevW || h != prevH {
				log.Printf("[SCREEN] Detected screen size change: %dx%d -> %dx%d.", prevW, prevH, w, h)
				prevW, prevH = w, h
				actualScreenWidth, actualScreenHeight = w, h // Обновляем глобальные переменные

				// Отправляем сигнал на перезапуск FFmpeg
				select {
				case ffmpegRestartSignal <- struct{}{}:
					log.Println("[SCREEN] Signaling FFmpeg restart due to resolution change.")
				default:
					// Канал заблокирован, значит, сигнал уже в очереди
					log.Println("[SCREEN] Restart signal already pending from screen watcher, skipping.")
				}

				// Отправляем информацию об изменении разрешения через DataChannel, если он активен.
				if activeDataChannel != nil {
					info := map[string]interface{}{
						"type":   "screen_info",
						"width":  w,
						"height": h,
					}
					b, _ := json.Marshal(info)
					_ = dc.SendText(string(b))
					log.Printf("[SCREEN] Sent updated screen_info: %dx%d", w, h)
				}
			}
		}
	}()
}

// parseFFmpegResolution читает stderr FFmpeg и парсит разрешение.
// Оно завершается, если получает сигнал из канала `quit`.
func parseFFmpegResolution(r io.Reader, quit <-chan struct{}) {
	scanner := bufio.NewScanner(r)
	for {
		select {
		case <-quit:
			log.Println("[FFmpeg Stderr] Quit signal received, stopping scanner.")
			return
		default:
			// Неблокирующее чтение, но scanner.Scan() блокируется.
			// Это не проблема, так как quit канал позволяет выйти из блокировки.
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					log.Printf("[FFmpeg Stderr] Scanner error: %v", err)
				}
				log.Println("[FFmpeg Stderr] Scanner finished or pipe closed.")
				return
			}
			line := scanner.Text()
			// log.Printf("[FFmpeg Stderr] %s", line) // Для отладки, если нужно видеть весь вывод
			if strings.Contains(line, "Video:") {
				if m := reResolution.FindStringSubmatch(line); len(m) == 3 {
					w, _ := strconv.Atoi(m[1])
					h, _ := strconv.Atoi(m[2])
					if w > 0 && h > 0 {
						select {
						case resolutionUpdates <- [2]int{w, h}:
							// Отправлено в канал
						default:
							// Канал заблокирован, возможно, предыдущее разрешение еще не обработано,
							// или уже есть сигнал на перезапуск.
						}
					}
				}
			}
		}
	}
}

// streamVideo считывает видеоданные из stdout FFmpeg и отправляет их в videoTrack.
// Оно завершается, если получает сигнал из канала `quit`.
func streamVideo(r io.Reader, videoTrack *webrtc.TrackLocalStaticSample, quit <-chan struct{}) {
	reader := bufio.NewReader(r)
	buf := make([]byte, 0, 1<<16) // Увеличен буфер
	tmp := make([]byte, 4096)
	for {
		select {
		case <-quit:
			log.Println("[FFmpeg Video Stream] Quit signal received, stopping streaming.")
			return
		default:
			n, err := reader.Read(tmp)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					log.Printf("[FFmpeg Video Stream] FFmpeg read error: %v", err)
				}
				log.Println("[FFmpeg Video Stream] EOF or pipe closed.")
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

				// Перед отправкой NALU, проверяем, не пришел ли сигнал на завершение
				select {
				case <-quit:
					log.Println("[FFmpeg Video Stream] Quit signal received during NALU processing, stopping.")
					return
				default:
					_ = videoTrack.WriteSample(media.Sample{Data: nalu, Duration: time.Second / 30})
				}
				buf = buf[next:]
			}
		}
	}
}

// findStartCode находит стартовый код H.264 NALU (00 00 00 01).
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
	// log.Printf("[CONTROL] %+v", ctl) // Закомментировано для уменьшения логов, если часто срабатывает

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
		// Используем actualScreenWidth/Height для масштабирования,
		// чтобы корректно преобразовать координаты от клиента
		// к текущему физическому разрешению.
		currentDisplayW, currentDisplayH := robotgo.GetScreenSize()
		// Можно добавить использование getPhysicalScreenSize, если оно надежнее
		// для получения ОС-актуального размера для mouse_move.
		// w, h := getPhysicalScreenSize()
		// if w == 0 || h == 0 { w, h = currentDisplayW, currentDisplayH }

		tw := actualScreenWidth // Разрешение, которое мы сообщаем клиенту
		th := actualScreenHeight
		if tw == 0 || th == 0 { // Fallback, если вдруг глобальные размеры еще не установлены
			tw, th = currentDisplayW, currentDisplayH
		}

		// Вычисляем коэффициент масштабирования
		scaleX := float64(currentDisplayW) / float64(tw)
		scaleY := float64(currentDisplayH) / float64(th)

		safeX := clampInt(int(x*scaleX), 0, currentDisplayW-1)
		safeY := clampInt(int(y*scaleY), 0, currentDisplayH-1)
		robotgo.MoveMouse(safeX, safeY)

	case "mouse_down", "mouse_up":
		// robotgo ожидает "left", "middle", "right"
		// Предполагаем, что "button" от 0 (left), 1 (middle), 2 (right)
		btn := int(ctl["button"].(float64))
		names := []string{"left", "middle", "right"}
		if btn < 0 || btn >= len(names) {
			log.Printf("[CONTROL] Unknown mouse button: %d", btn)
			return
		}
		if t == "mouse_down" {
			robotgo.MouseDown(names[btn])
		} else {
			robotgo.MouseUp(names[btn])
		}

	case "mouse_toggle": // Некоторые реализации могут посылать mouse_toggle
		btn := int(ctl["button"].(float64))
		state, ok := ctl["state"].(string)
		if !ok {
			return
		}
		names := []string{"left", "middle", "right"}
		if btn < 0 || btn >= len(names) {
			log.Printf("[CONTROL] Unknown mouse button: %d", btn)
			return
		}
		if state == "down" {
			robotgo.MouseDown(names[btn])
		} else if state == "up" {
			robotgo.MouseUp(names[btn])
		}

	case "mouse_click": // Некоторые реализации могут посылать mouse_click
		btn := int(ctl["button"].(float64))
		names := []string{"left", "middle", "right"}
		if btn >= 0 && btn < len(names) {
			robotgo.Click(names[btn])
		}

	case "key_down":
		key_str, ok := ctl["key"].(string)
		if !ok {
			log.Println("[CONTROL] Key event missing 'key' field.")
			return
		}
		// robotgo.KeyTap(key_str) // KeyTap - это down + up
		robotgo.KeyDown(key_str)

	case "key_up":
		key_str, ok := ctl["key"].(string)
		if !ok {
			log.Println("[CONTROL] Key event missing 'key' field.")
			return
		}
		robotgo.KeyUp(key_str)

	case "key_press": // Для имитации однократного нажатия
		key_str, ok := ctl["key"].(string)
		if !ok {
			log.Println("[CONTROL] Key event missing 'key' field.")
			return
		}
		robotgo.KeyTap(key_str)

	default:
		log.Printf("[CONTROL] Unhandled event type: %s", t)
	}
}

// clampInt ограничивает значение `v` между `min` и `max`.
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
