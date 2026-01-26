package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"time"
)

/*
Эти переменные ЗАПОЛНЯЮТСЯ через -ldflags в builder/build_agents.py
*/
var (
	CompanyIDStr string
	ServerURL    string
	BuildSlug    string
)

const (
	identityFile   = "agent_identity.json"
	machineUIDFile = "machine_uid"
)

/*
========================
UTILS
========================
*/

func fatal(msg string, err error) {
	if err != nil {
		fmt.Printf("[FATAL] %s: %v\n", msg, err)
	} else {
		fmt.Printf("[FATAL] %s\n", msg)
	}
	os.Exit(1)
}

func loadOrCreateMachineUID() string {
	if data, err := os.ReadFile(machineUIDFile); err == nil {
		return string(bytes.TrimSpace(data))
	}

	uid := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	if err := os.WriteFile(machineUIDFile, []byte(uid), 0644); err != nil {
		fatal("cannot write machine_uid", err)
	}
	return uid
}

func getLocalIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok &&
				!ipnet.IP.IsLoopback() &&
				ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

/*
========================
IDENTITY
========================
*/

type Identity struct {
	UUID  string `json:"agent_uuid"`
	Token string `json:"token"`
}

func loadIdentity() (*Identity, error) {
	data, err := os.ReadFile(identityFile)
	if err != nil {
		return nil, err
	}
	var id Identity
	err = json.Unmarshal(data, &id)
	if err != nil {
		return nil, err
	}
	if id.UUID == "" || id.Token == "" {
		return nil, fmt.Errorf("identity file invalid")
	}
	return &id, nil
}

func saveIdentity(id *Identity) {
	data, _ := json.MarshalIndent(id, "", "  ")
	_ = os.WriteFile(identityFile, data, 0600)
}

/*
========================
API
========================
*/

func registerAgent(payload map[string]interface{}) *Identity {
	body, _ := json.Marshal(payload)

	resp, err := http.Post(
		ServerURL+"/api/agent/register",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		fatal("register request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fatal(fmt.Sprintf("register failed, status %d", resp.StatusCode), nil)
	}

	var res Identity
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		fatal("cannot decode register response", err)
	}

	if res.UUID == "" || res.Token == "" {
		fatal("register response missing uuid/token", nil)
	}

	fmt.Println("[INFO] registered agent:", res.UUID)
	return &res
}

func sendHeartbeat(id *Identity) {
	url := fmt.Sprintf(
		"%s/api/agent/heartbeat?uuid=%s&token=%s",
		ServerURL,
		id.UUID,
		id.Token,
	)

	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Println("[WARN] heartbeat error:", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("[WARN] heartbeat status:", resp.StatusCode)
		return
	}

	fmt.Println("[OK] heartbeat", time.Now().Format(time.RFC3339))
}

/*
========================
MAIN
========================
*/

func main() {
	fmt.Println("=== RMM AGENT START ===")

	if ServerURL == "" {
		fatal("ServerURL is empty (ldflags not applied)", nil)
	}

	hostname, err := os.Hostname()
	if err != nil {
		fatal("cannot get hostname", err)
	}

	machineUID := loadOrCreateMachineUID()
	fmt.Println("[INFO] machine_uid:", machineUID)

	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	registerPayload := map[string]interface{}{
		"machine_uid":      machineUID,
		"name_pc":          hostname,
		"system":           runtime.GOOS,
		"user_name":        username,
		"ip_addr":          getLocalIP(),
		"disks":            []interface{}{},
		"total_memory":     nil,
		"available_memory": nil,
		"external_ip":      "",
	}

	var identity *Identity

	id, err := loadIdentity()
	if err != nil {
		fmt.Println("[INFO] no identity found, registering")
		identity = registerAgent(registerPayload)
		saveIdentity(identity)
	} else {
		identity = id
		fmt.Println("[INFO] loaded identity:", identity.UUID)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		sendHeartbeat(identity)
		<-ticker.C
	}
}
