package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

/*
=====================================

	Compile-time variables (ldflags)

=====================================
*/
var (
	CompanyIDStr string
	ServerURL    string
	BuildSlug    string
)

/*
=====================================

	HTTP client

=====================================
*/
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // dev only
	},
}

/*
=====================================

	Machine UID

=====================================
*/
func loadOrCreateMachineUID() string {
	const filename = "machine_uid"

	if data, err := os.ReadFile(filename); err == nil {
		return string(bytes.TrimSpace(data))
	}

	uid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	_ = os.WriteFile(filename, []byte(uid), 0644)
	return uid
}

/*
=====================================

	Network helpers

=====================================
*/
func getLocalIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok &&
			!ipnet.IP.IsLoopback() &&
			ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func getExternalIP() string {
	resp, err := httpClient.Get("https://api.ipify.org")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

/*
=====================================

	Telemetry

=====================================
*/
func collectTelemetry() map[string]interface{} {
	telemetry := map[string]interface{}{
		"system":           runtime.GOOS,
		"user_name":        os.Getenv("USERNAME"),
		"ip_addr":          getLocalIP(),
		"external_ip":      getExternalIP(),
		"disks":            []map[string]interface{}{},
		"total_memory":     0,
		"available_memory": 0,
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		telemetry["total_memory"] = int(vm.Total / (1024 * 1024))
		telemetry["available_memory"] = int(vm.Available / (1024 * 1024))
	}

	if parts, err := disk.Partitions(true); err == nil {
		var disks []map[string]interface{}
		for _, p := range parts {
			if usage, err := disk.Usage(p.Mountpoint); err == nil {
				disks = append(disks, map[string]interface{}{
					"name": p.Mountpoint,
					"size": int(usage.Total / (1024 * 1024 * 1024)),
					"free": int(usage.Free / (1024 * 1024 * 1024)),
				})
			}
		}
		telemetry["disks"] = disks
	}

	return telemetry
}

/*
=====================================

	HTTP helpers

=====================================
*/
func postJSON(url string, payload interface{}) ([]byte, int, error) {
	data, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

/*
=====================================

	Heartbeat

=====================================
*/
func sendHeartbeat(serverURL, uuid, token string) {
	url := fmt.Sprintf(
		"%s/api/agent/heartbeat?uuid=%s&token=%s",
		serverURL, uuid, token,
	)

	req, _ := http.NewRequest("POST", url, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Println("heartbeat error:", err)
		return
	}
	resp.Body.Close()
}

/*
=====================================

	Register Response Structure

=====================================
*/
type RegisterResponse struct {
	AgentUUID string `json:"agent_uuid"`
	Token     string `json:"token"`
}

/*
=====================================

	Main logic

=====================================
*/
func mainLogic() {
	log.Println("Agent starting…")
	log.Printf("ServerURL=%s Build=%s\n", ServerURL, BuildSlug)

	if ServerURL == "" {
		log.Fatalln("ServerURL is empty (ldflags broken)")
	}

	hostname, _ := os.Hostname()
	machineUID := loadOrCreateMachineUID()

	/*
	   -----------------------------
	   1. Register
	   -----------------------------
	*/
	registerPayload := map[string]interface{}{
		"name_pc":     hostname,
		"machine_uid": machineUID,
	}

	body, status, err := postJSON(
		ServerURL+"/api/agent/register",
		registerPayload,
	)
	if err != nil || status != 200 {
		log.Fatalln("register failed:", err, status)
	}

	var Resp RegisterResponse
	if err := json.Unmarshal(body, &Resp); err != nil {
		log.Fatalln("invalid register response")
	}

	log.Println("Registered as", Resp.AgentUUID)

	/*
	   -----------------------------
	   2. Telemetry (once)
	   -----------------------------
	*/
	telemetry := collectTelemetry()
	telemetryURL := fmt.Sprintf(
		"%s/api/agent/telemetry?uuid=%s&token=%s",
		ServerURL, Resp.AgentUUID, Resp.Token,
	)

	_, _, _ = postJSON(telemetryURL, telemetry)

	/*
	   -----------------------------
	   3. Heartbeat loop
	   -----------------------------
	*/
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		sendHeartbeat(ServerURL, Resp.AgentUUID, Resp.Token)
	}
}

/*
=====================================

	Service configuration

=====================================
*/
type Program struct{}

func (p *Program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *Program) Stop(s service.Service) error {
	fmt.Println("Stopping the service...")
	return nil
}

func (p *Program) run() {
	mainLogic()
}

/*
=====================================

	Entry point

=====================================
*/
func main() {
	svcFlag := flag.Bool("service", false, "Control service mode.")
	flag.Parse()

	programName := filepath.Base(os.Args[0])

	serviceConfig := &service.Config{
		Name:        programName,
		DisplayName: "My Monitoring Agent",
		Description: "Monitoring agent for system resources.",
	}

	prg := &Program{}
	s, err := service.New(prg, serviceConfig)
	if err != nil {
		log.Fatalf("Error creating service: %v", err)
	}

	errs := make(chan error, 5)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errs <- fmt.Errorf("%s", <-c)
	}()

	if *svcFlag && len(flag.Args()) > 0 {
		action := strings.Join(flag.Args(), "")
		switch action {
		case "install":
			err = s.Install()
			if err == nil {
				// Обновляем тип запуска службы на автоматический
				updateCmd := exec.Command("sc", "config", programName, "start=", "auto")
				updateCmd.Stdout = os.Stdout
				updateCmd.Stderr = os.Stderr
				err = updateCmd.Run()
				if err != nil {
					log.Fatalf("Error setting service start type to 'auto': %v", err)
				}
			}
		case "uninstall":
			err = s.Uninstall()
		case "start":
			err = s.Start()
		case "stop":
			err = s.Stop()
		default:
			err = fmt.Errorf("Unknown service command '%s'", action)
		}
		if err != nil {
			log.Fatalf("Failed to execute service command: %v", err)
		}
		return
	}

	if err = s.Run(); err != nil {
		log.Fatalf("Service failed to start: %v", err)
	}
}
