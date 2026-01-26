// builder/agent/cmd/agent/main.go
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

// ========================
// Compile-time variables через ldflags
// ========================
var (
	CompanyIDStr string
	ServerURL    string
	BuildSlug    string
)

// ========================
// Machine UID
// ========================
func loadOrCreateMachineUID() string {
	const filename = "machine_uid"

	data, err := os.ReadFile(filename)
	if err == nil {
		return string(bytes.TrimSpace(data))
	}

	uid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	_ = os.WriteFile(filename, []byte(uid), 0644)

	return uid
}

// ========================
// Локальный IP
// ========================
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// ========================
// Внешний IP через ipify
// ========================
func getExternalIP() string {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// ========================
// Вспомогательная функция для HTTP POST
// ========================
func postJSON(url string, payload interface{}) (*http.Response, error) {
	data, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // ⚠️ для dev / тест
		},
		Timeout: 15 * time.Second,
	}

	return client.Do(req)
}

// ========================
// Сбор системной информации
// ========================
func collectTelemetry() map[string]interface{} {
	telemetry := map[string]interface{}{
		"system":           runtime.GOOS,
		"user_name":        os.Getenv("USERNAME"),
		"ip_addr":          getLocalIP(),
		"disks":            []map[string]interface{}{},
		"total_memory":     0,
		"available_memory": 0,
		"external_ip":      getExternalIP(),
	}

	// Память
	if vm, err := mem.VirtualMemory(); err == nil {
		telemetry["total_memory"] = int(vm.Total / (1024 * 1024))         // МБ
		telemetry["available_memory"] = int(vm.Available / (1024 * 1024)) // МБ
	}

	// Диски
	if parts, err := disk.Partitions(true); err == nil {
		disks := []map[string]interface{}{}
		for _, p := range parts {
			if usage, err := disk.Usage(p.Mountpoint); err == nil {
				disks = append(disks, map[string]interface{}{
					"name": p.Mountpoint,
					"size": int(usage.Total / (1024 * 1024 * 1024)), // ГБ
					"free": int(usage.Free / (1024 * 1024 * 1024)),  // ГБ
				})
			}
		}
		telemetry["disks"] = disks
	}

	return telemetry
}

// ========================
// Main
// ========================
func main() {
	fmt.Println("Agent starting...")
	fmt.Printf("CompanyID: %s\nServerURL: %s\nBuildSlug: %s\n", CompanyIDStr, ServerURL, BuildSlug)

	if ServerURL == "" {
		log.Fatalln("ServerURL не задан! Проверь сборку через Python ldflags")
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-pc"
	}

	machineUID := loadOrCreateMachineUID()

	// -----------------------
	// 1. Регистрация
	// -----------------------
	registerPayload := map[string]interface{}{
		"name_pc":     hostname,
		"machine_uid": machineUID,
	}

	resp, err := postJSON(ServerURL+"/api/agent/register", registerPayload)
	if err != nil {
		log.Println("Ошибка при регистрации агента:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("Регистрация не удалась, статус:", resp.Status)
		return
	}

	var registerResult map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &registerResult); err != nil {
		log.Println("Ошибка чтения ответа регистрации:", err)
		return
	}
	fmt.Println("Registered:", registerResult)

	uuid, _ := registerResult["agent_uuid"].(string)
	token, _ := registerResult["token"].(string)

	// -----------------------
	// 2. Отправка системных данных (telemetry) с uuid/token в query
	// -----------------------
	telemetry := collectTelemetry()

	// 🔹 Логируем JSON перед отправкой
	telemetryJSON, _ := json.MarshalIndent(telemetry, "", "  ")
	fmt.Println("Отправляем telemetry JSON:")
	fmt.Println(string(telemetryJSON))

	telemetryURL := fmt.Sprintf("%s/api/agent/telemetry?uuid=%s&token=%s", ServerURL, uuid, token)

	resp, err = postJSON(telemetryURL, telemetry)
	if err != nil {
		log.Println("Ошибка при отправке telemetry:", err)
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("Ответ сервера:", resp.Status)
		fmt.Println("Тело ответа:", string(body))

		if resp.StatusCode == 200 {
			fmt.Println("Telemetry успешно отправлена")
		} else {
			fmt.Println("Ошибка telemetry, статус:", resp.Status)
		}
	}

	// -----------------------
	// 3. Heartbeat
	// -----------------------
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		fmt.Println("Agent heartbeat:", time.Now())
	}
}
