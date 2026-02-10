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
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	CompanySlug  string
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

func getExecutableInfo() (string, string, string) {
	var exePath, exeDir, exeName string

	// Способ 1: os.Executable() (предпочтительный)
	if path, err := os.Executable(); err == nil {
		exePath = path
	} else {
		// Способ 2: os.Args[0] как fallback
		if len(os.Args) > 0 {
			exePath = os.Args[0]

			// Если путь относительный, добавляем текущую директорию
			if !filepath.IsAbs(exePath) {
				if wd, err := os.Getwd(); err == nil {
					exePath = filepath.Join(wd, exePath)
				}
			}
		}
	}

	// Делаем путь абсолютным и нормализуем
	if exePath != "" {
		if absPath, err := filepath.Abs(exePath); err == nil {
			exePath = absPath
		}

		// Получаем директорию и имя файла
		exeDir = filepath.Dir(exePath)
		exeName = filepath.Base(exePath)
	}

	return exePath, exeDir, exeName
}

func loadOrCreateMachineUID() string {
	// Получаем директорию исполняемого файла
	_, exeDir, _ := getExecutableInfo()
	if exeDir == "" {
		exeDir = "." // fallback
	}

	filename := filepath.Join(exeDir, "machine_uid")

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
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Error getting interfaces: %v", err)
		return ""
	}

	for _, iface := range interfaces {
		// Пропускаем выключенные интерфейсы
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Пропускаем loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ip := ipnet.IP.String()

				// Пропускаем APIPA адреса (169.254.0.0/16)
				if ipnet.IP.IsLinkLocalUnicast() {
					log.Printf("Interface %s has APIPA address: %s", iface.Name, ip)
					continue
				}

				// Предпочитаем private адреса
				if ipnet.IP.IsPrivate() {
					log.Printf("Found valid IP on %s: %s", iface.Name, ip)
					return ip
				}
			}
		}
	}

	// Если не нашли нормальных адресов, ищем любой не-APIPA
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ip := ipnet.IP.String()

				// Берем любой IPv4, кроме loopback
				if !ipnet.IP.IsLoopback() && !ipnet.IP.IsLinkLocalUnicast() {
					log.Printf("Using fallback IP on %s: %s", iface.Name, ip)
					return ip
				}
			}
		}
	}

	log.Println("No suitable IP address found")
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

func formatUsersString(usersString string) string {
	if len(usersString) <= 255 {
		return usersString
	}

	// Обрезаем до 252 символов
	truncated := usersString[:252]

	// Ищем последнюю запятую для красивого обреза
	for i := len(truncated) - 1; i >= 0; i-- {
		if truncated[i] == ',' {
			return strings.TrimSpace(truncated[:i]) + "..."
		}
	}

	// Если запятых нет, просто обрезаем
	return truncated + "..."
}

func getUsersAsString() string {
	// PowerShell команда с явным указанием UTF-8
	psCommand := `$OutputEncoding = [console]::InputEncoding = [console]::OutputEncoding = New-Object System.Text.UTF8Encoding; `
	psCommand += `Get-LocalUser | Where-Object { $_.Enabled -eq $true } | `
	psCommand += `ForEach-Object { $_.Name }`

	cmd := exec.Command("powershell", "-Command", psCommand)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("PowerShell error: %v", err)
		return ""
	}

	// Разбираем вывод
	var users []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "$") {
			continue
		}

		// Проверяем, что это не системная учетка
		if !isSystemAccount(line) {
			users = append(users, line)
		}
	}

	if len(users) == 0 {
		return ""
	}

	usersString := strings.Join(users, ", ")
	return formatUsersString(usersString)
}

func isSystemAccount(username string) bool {
	lower := strings.ToLower(username)
	systemAccounts := []string{
		"administrator",
		"guest",
		"defaultaccount",
		"wdagutilityaccount",
		"system",
		"network service",
		"local service",
	}

	for _, acc := range systemAccounts {
		if strings.Contains(lower, acc) {
			return true
		}
	}

	return false
}

/*
=====================================

	Telemetry

=====================================
*/
func collectTelemetry() map[string]interface{} {
	telemetry := map[string]interface{}{
		"system":           runtime.GOOS,
		"user_name":        getUsersAsString(),
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
	log.Printf("ServerURL=%s Build=%s\n", ServerURL, CompanySlug)

	if ServerURL == "" {
		log.Fatalln("ServerURL is empty (ldflags broken)")
	}

	if CompanyIDStr == "" {
		log.Fatalln("CompanyID is empty (ldflags broken)")
	}
	// Преобразуем CompanyIDStr в int
	companyIDInt, err := strconv.Atoi(CompanyIDStr)
	if err != nil {
		log.Fatalf("Invalid CompanyID format: %s. Must be a number.", CompanyIDStr)
	}

	hostname, _ := os.Hostname()
	machineUID := loadOrCreateMachineUID()

	/*
	   -----------------------------
	   1. Registration attempt
	   -----------------------------
	*/
	var Resp RegisterResponse

	for {
		registerPayload := map[string]interface{}{
			"name_pc":     hostname,
			"machine_uid": machineUID,
			"company_id":  companyIDInt,
		}

		body, status, err := postJSON(
			ServerURL+"/api/agent/register",
			registerPayload,
		)

		if err != nil || status != 200 {
			log.Printf("Registration failed due to server issues: %v\n", err)
			log.Println("Waiting before next try...")
			time.Sleep(10 * time.Second) // Ждем 10 секунд перед следующей попыткой
			continue
		}

		if err := json.Unmarshal(body, &Resp); err != nil {
			log.Fatalln("Invalid register response format:", err)
		}

		break // Успех, регистрация пройдена
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
	log.Println("Service is starting...")
	go p.run()
	return nil
}

func (p *Program) Stop(s service.Service) error {
	log.Println("Service is stopping...")
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
	// Определяем путь к исполняемому файлу
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	// Получаем абсолютный путь к исполняемому файлу
	absPath, err := filepath.Abs(exePath)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Конфигурация службы с явным указанием аргументов
	serviceConfig := &service.Config{
		Name:        "SystemMonitoringAgent",
		DisplayName: "System Monitoring Agent",
		Description: "Agent for monitoring system resources and sending telemetry.",
		Arguments:   []string{},
		Executable:  absPath,
	}

	// Создаем объект программы
	prg := &Program{}

	// Создаем службу
	s, err := service.New(prg, serviceConfig)
	if err != nil {
		log.Fatalf("Error creating service: %v", err)
	}

	// Проверяем, запущена ли программа как служба
	if service.Interactive() {
		// Запущено из командной строки - устанавливаем службу
		log.Println("Running in interactive mode, installing service...")

		// Проверяем, установлена ли уже служба
		status, err := s.Status()
		if err != nil && err != service.ErrNotInstalled {
			log.Printf("Warning: Could not check service status: %v", err)
		}

		if status == service.StatusUnknown || err == service.ErrNotInstalled {
			// Служба не установлена - устанавливаем
			log.Println("Installing service...")
			err = s.Install()
			if err != nil {
				log.Fatalf("Failed to install service: %v", err)
			}

			log.Println("Service installed successfully")

			// Запускаем службу
			log.Println("Starting service...")
			err = s.Start()
			if err != nil {
				log.Printf("Warning: Could not start service: %v", err)
				log.Println("Service installed but not started. You may need to start it manually.")
			} else {
				log.Println("Service started successfully")
			}
		} else {
			// Служба уже установлена - запускаем
			log.Println("Service already installed, starting...")
			err = s.Start()
			if err != nil {
				log.Fatalf("Failed to start service: %v", err)
			}
			log.Println("Service started successfully")
		}

		// Завершаем работу интерактивного режима
		log.Println("Exiting interactive mode. The service will continue running in the background.")
		return
	}

	// Запускаем как службу
	logger, err := s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}

	err = s.Run()
	if err != nil {
		logger.Error(err)
	}
}
