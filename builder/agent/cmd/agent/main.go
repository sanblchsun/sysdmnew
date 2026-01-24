package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// ========================
// Compile-time variables
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

	// Пытаемся прочитать существующий
	data, err := os.ReadFile(filename)
	if err == nil {
		return string(bytes.TrimSpace(data))
	}

	// Если нет — создаём
	uid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	_ = os.WriteFile(filename, []byte(uid), 0644)

	return uid
}

// ========================
// Main
// ========================
func main() {
	fmt.Println("Agent starting...")
	fmt.Printf(
		"CompanyID: %s\nServerURL: %s\nBuildSlug: %s\n",
		CompanyIDStr,
		ServerURL,
		BuildSlug,
	)

	// company_id сейчас сервер НЕ ждёт,
	// но оставляем преобразование, чтобы не ломать билд
	if CompanyIDStr != "" {
		_, _ = strconv.Atoi(CompanyIDStr)
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-pc"
	}

	machineUID := loadOrCreateMachineUID()

	payload := map[string]interface{}{
		"name_pc":     hostname,
		"machine_uid": machineUID,
	}

	payloadBytes, _ := json.Marshal(payload)

	resp, err := http.Post(
		ServerURL+"/api/agent/register",
		"application/json",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		fmt.Println("Registration failed:", err)
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	fmt.Println("Registered:", result)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fmt.Println("Agent heartbeat:", time.Now())
	}
}
