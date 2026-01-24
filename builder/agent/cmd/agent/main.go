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

// Все переменные string
var (
	CompanyIDStr string
	ServerURL    string
	BuildSlug    string
)

func main() {
	fmt.Println("Agent starting...")
	fmt.Printf("CompanyID: %s\nServerURL: %s\nBuildSlug: %s\n", CompanyIDStr, ServerURL, BuildSlug)

	// Конвертируем в int, если нужно
	companyID := 0
	if CompanyIDStr != "" {
		companyID, _ = strconv.Atoi(CompanyIDStr)
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-pc"
	}

	payload := map[string]interface{}{
		"name_pc":    hostname,
		"company_id": companyID, // если сервер ожидает
	}

	payloadBytes, _ := json.Marshal(payload)
	resp, err := http.Post(ServerURL+"/api/agent/register", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		fmt.Println("Registration failed:", err)
	} else {
		defer resp.Body.Close()
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Println("Registered:", result)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		fmt.Println("Agent heartbeat:", time.Now())
	}
}
