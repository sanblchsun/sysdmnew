// builder/agent/cmd/agent/main.go
package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
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

var (
	CompanyIDStr string
	CompanySlug  string
	ServerURL    string
	BuildSlug    string
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func getExePath() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	abs, _ := filepath.Abs(exe)
	return abs
}

func loadOrCreateMachineUID() string {
	exeDir := filepath.Dir(getExePath())
	path := filepath.Join(exeDir, "machine_uid")

	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b))
	}

	uid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	_ = os.WriteFile(path, []byte(uid), 0644)
	return uid
}

func getLocalIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				if ipnet.IP.IsPrivate() {
					return ipnet.IP.String()
				}
			}
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
	b, _ := io.ReadAll(resp.Body)
	return string(b)
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

func collectTelemetry() map[string]interface{} {
	data := map[string]interface{}{
		"system":           runtime.GOOS,
		"user_name":        getUsersAsString(),
		"ip_addr":          getLocalIP(),
		"external_ip":      getExternalIP(),
		"disks":            []map[string]interface{}{},
		"total_memory":     0,
		"available_memory": 0,
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		data["total_memory"] = int(vm.Total / (1024 * 1024))
		data["available_memory"] = int(vm.Available / (1024 * 1024))
	}

	if parts, err := disk.Partitions(true); err == nil {
		var disksData []map[string]interface{}
		for _, p := range parts {
			if u, err := disk.Usage(p.Mountpoint); err == nil {
				disksData = append(disksData, map[string]interface{}{
					"name": p.Mountpoint,
					"size": int(u.Total / (1024 * 1024 * 1024)),
					"free": int(u.Free / (1024 * 1024 * 1024)),
				})
			}
		}
		data["disks"] = disksData
	}

	return data
}

func postJSON(url string, payload interface{}) ([]byte, int, error) {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

type UpdateResponse struct {
	Update bool   `json:"update"`
	Build  string `json:"build"`
	URL    string `json:"url"`
	Sha256 string `json:"sha256"`
	Force  bool   `json:"force"`
}

func checkForUpdate(uuid, token string) {
	payload := map[string]interface{}{
		"uuid":  uuid,
		"token": token,
		"build": BuildSlug,
	}

	body, _, err := postJSON(ServerURL+"/api/agent/check-update", payload)
	if err != nil {
		log.Println("Update check failed:", err)
		return
	}

	var u UpdateResponse
	if err := json.Unmarshal(body, &u); err != nil {
		log.Println("Invalid update response:", err)
		return
	}

	if !u.Update {
		return
	}

	log.Println("New version available:", u.Build)

	exePath := getExePath()
	tmpPath := exePath + ".new"

	// ---- download file ----
	resp, err := httpClient.Get(u.URL)
	if err != nil {
		log.Println("Download failed:", err)
		return
	}
	defer resp.Body.Close()

	out, err := os.Create(tmpPath)
	if err != nil {
		log.Println("Cannot create tmp file:", err)
		return
	}

	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		log.Println("Download copy failed:", err)
		os.Remove(tmpPath)
		return
	}

	// ---- SHA256 verify ----
	hash, err := sha256File(tmpPath)
	if err != nil {
		log.Println("Hash calculation failed:", err)
		os.Remove(tmpPath)
		return
	}

	if hash != u.Sha256 {
		log.Println("SHA256 mismatch! Aborting update.")
		os.Remove(tmpPath)
		return
	}

	log.Println("SHA256 verified. Applying update...")

	// ---- replace binary ----
	os.Rename(exePath, exePath+".old")
	os.Rename(tmpPath, exePath)

	// ---- notify server ----
	postJSON(
		fmt.Sprintf("%s/api/agent/telemetry?uuid=%s&token=%s", ServerURL, uuid, token),
		map[string]interface{}{
			"exe_version": u.Build,
		},
	)

	exec.Command(exePath).Start()
	os.Exit(0)
}

func mainLogic() {
	log.Println("Agent started", BuildSlug)

	companyID, _ := strconv.Atoi(CompanyIDStr)
	hostname, _ := os.Hostname()
	machineUID := loadOrCreateMachineUID()

	var uuid, token string

	// =========================
	// Registration с exe_version
	// =========================
	for {
		resp, code, err := postJSON(ServerURL+"/api/agent/register", map[string]interface{}{
			"name_pc":     hostname,
			"machine_uid": machineUID,
			"company_id":  companyID,
			"exe_version": BuildSlug,
		})

		if err == nil && code == 200 {
			var r struct {
				AgentUUID string `json:"agent_uuid"`
				Token     string `json:"token"`
			}
			json.Unmarshal(resp, &r)
			uuid, token = r.AgentUUID, r.Token
			break
		}
		time.Sleep(10 * time.Second)
	}

	// =========================
	// Initial telemetry
	// =========================
	telemetry := collectTelemetry()
	telemetry["exe_version"] = BuildSlug
	postJSON(
		fmt.Sprintf("%s/api/agent/telemetry?uuid=%s&token=%s", ServerURL, uuid, token),
		telemetry,
	)

	ticker := time.NewTicker(10 * time.Second)
	updateTicker := time.NewTicker(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			httpClient.Post(
				fmt.Sprintf("%s/api/agent/heartbeat?uuid=%s&token=%s", ServerURL, uuid, token),
				"application/json", nil,
			)
		case <-updateTicker.C:
			checkForUpdate(uuid, token)
		}
	}
}

type Program struct{}

func (p *Program) Start(s service.Service) error {
	go mainLogic()
	return nil
}
func (p *Program) Stop(s service.Service) error { return nil }

func main() {
	exe := getExePath()

	cfg := &service.Config{
		Name:        "SystemMonitoringAgent",
		DisplayName: "System Monitoring Agent",
		Executable:  exe,
	}

	prg := &Program{}
	s, _ := service.New(prg, cfg)

	if service.Interactive() {
		s.Install()
		s.Start()
		return
	}

	s.Run()
}
