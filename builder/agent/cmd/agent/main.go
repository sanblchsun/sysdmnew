package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

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

	Main

=====================================
*/
func main() {
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

	var regResp struct {
		AgentUUID string `json:"agent_uuid"`
		Token     string `json:"token"`
	}
	if err := json.Unmarshal(body, &regResp); err != nil {
		log.Fatalln("invalid register response")
	}

	log.Println("Registered as", regResp.AgentUUID)

	/*
		-----------------------------
		2. Telemetry (once)
		-----------------------------
	*/
	telemetry := collectTelemetry()
	telemetryURL := fmt.Sprintf(
		"%s/api/agent/telemetry?uuid=%s&token=%s",
		ServerURL, regResp.AgentUUID, regResp.Token,
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
		sendHeartbeat(ServerURL, regResp.AgentUUID, regResp.Token)
	}
}
