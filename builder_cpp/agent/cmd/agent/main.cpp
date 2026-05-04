// builder_cpp/agent/cmd/agent/main.cpp
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <winhttp.h>
#include <shlobj.h>
#include <processthreadsapi.h>
#include <winsock2.h>
#include <ws2tcpip.h>
#include <iostream>
#include <fstream>
#include <string>
#include <vector>
#include <chrono>
#include <thread>
#include <sstream>
#include <iomanip>
#include <algorithm>
#include <cctype>
#include <ctime>
#include <random>
#include <mutex>
#include <cstdarg>
#include <atomic>
#include <wincrypt.h>
#include <cstring>

#pragma comment(lib, "winhttp.lib")
#pragma comment(lib, "advapi32.lib")
#pragma comment(lib, "ws2_32.lib")
#pragma comment(lib, "crypt32.lib")

// Default values (overridden via compiler macros)
#ifndef SERVER_URL
#define SERVER_URL "http://localhost:8000"
#endif

#ifndef BUILD_SLUG
#define BUILD_SLUG "1.0.0"
#endif

std::string serverURL = SERVER_URL;
std::string buildSlug = BUILD_SLUG;

// Manual URL parsing to avoid WinHttpCrackUrl issues with query params
bool parseUrl(const std::string& url, std::string& host, int& port, std::string& path, std::string& query) {
    // Simple parser for http://host:port/path?query
    host.clear(); port = 80; path = "/"; query.clear();
    
    std::string u = url;
    if (u.find("http://") == 0) u = u.substr(7);
    else if (u.find("https://") == 0) { u = u.substr(8); port = 443; }
    
    size_t pathPos = u.find('/');
    size_t queryPos = u.find('?');
    
    std::string hostPort;
    if (pathPos != std::string::npos) {
        hostPort = u.substr(0, pathPos);
        if (queryPos != std::string::npos && queryPos > pathPos) {
            path = u.substr(pathPos, queryPos - pathPos);
            query = u.substr(queryPos);
        } else {
            path = u.substr(pathPos);
        }
    } else {
        hostPort = u;
    }
    
    size_t colonPos = hostPort.find(':');
    if (colonPos != std::string::npos) {
        host = hostPort.substr(0, colonPos);
        port = std::stoi(hostPort.substr(colonPos + 1));
    } else {
        host = hostPort;
    }
    
    return !host.empty();
}

std::mutex logMutex;
std::ofstream logFile;
std::atomic<bool> g_stopRequested(false);
std::string g_telemetryMode = "none";  // "none", "basic", "full"
SERVICE_STATUS serviceStatus = {0};
SERVICE_STATUS_HANDLE serviceHandle = NULL;
HANDLE stopEvent = NULL;

// ==================== UTILITIES ====================

std::string getExePath() {
    char path[MAX_PATH] = {0};
    GetModuleFileNameA(NULL, path, MAX_PATH);
    return std::string(path);
}

std::string getExeDir() {
    std::string exePath = getExePath();
    size_t pos = exePath.find_last_of("\\/");
    return (pos != std::string::npos) ? exePath.substr(0, pos) : exePath;
}

void setupFileLogger() {
    std::string exeDir = getExeDir();
    std::string logPath = exeDir + "\\agent.log";
    logFile.open(logPath, std::ios::app | std::ios::out);
}

void log(const char* msg) {
    std::lock_guard<std::mutex> lock(logMutex);
    auto now = std::chrono::system_clock::now();
    auto time = std::chrono::system_clock::to_time_t(now);
    struct tm tmTemp;
    localtime_s(&tmTemp, &time);
    char timeStr[32];
    strftime(timeStr, sizeof(timeStr), "%Y-%m-%d %H:%M:%S", &tmTemp);
    std::string line = std::string(timeStr) + " " + msg + "\n";
    std::cout << line;
    if (logFile.is_open()) {
        logFile << line;
        logFile.flush();
    }
}

void logf(const char* fmt, ...) {
    char buf[1024];
    va_list args;
    va_start(args, fmt);
    vsnprintf(buf, sizeof(buf), fmt, args);
    va_end(args);
    log(buf);
}

// ==================== MACHINE UID ====================

std::string loadOrCreateMachineUID() {
    std::string exeDir = getExeDir();
    std::string uidPath = exeDir + "\\machine_uid";
    std::ifstream ifs(uidPath);
    if (ifs.good()) {
        std::string uid;
        std::getline(ifs, uid);
        if (!uid.empty()) return uid;
    }
    std::random_device rd;
    std::mt19937 gen(rd());
    std::uniform_int_distribution<> dist(0, 999999);
    std::ostringstream oss;
    oss << time(nullptr) << "-" << GetCurrentProcessId() << "-" << dist(gen);
    std::string uid = oss.str();
    std::ofstream of(uidPath);
    of << uid;
    of.close();
    return uid;
}

// ==================== NETWORK ====================

std::string getLocalIP() {
    WSADATA wsaData;
    if (WSAStartup(MAKEWORD(2, 2), &wsaData) != 0) return "";
    char hostname[256];
    if (gethostname(hostname, sizeof(hostname))) {
        WSACleanup();
        return "";
    }
    struct hostent* he = gethostbyname(hostname);
    if (!he) {
        WSACleanup();
        return "";
    }
    for (int i = 0; he->h_addr_list[i]; i++) {
        struct in_addr** addrList = (struct in_addr**)he->h_addr_list;
        if (addrList[i]) {
            char* ip = inet_ntoa(*addrList[i]);
            if (ip && strncmp(ip, "127.", 4) != 0) {
                WSACleanup();
                return std::string(ip);
            }
        }
    }
    WSACleanup();
    return "";
}

std::string getExternalIP() {
    log("Getting external IP...");
    HINTERNET hSession = WinHttpOpen(L"Agent/1.0", WINHTTP_ACCESS_TYPE_NO_PROXY, NULL, NULL, 0);
    if (!hSession) {
        logf("WinHttpOpen failed: %lu", GetLastError());
        return "";
    }
    HINTERNET hConnect = WinHttpConnect(hSession, L"api.ipify.org", INTERNET_DEFAULT_HTTP_PORT, 0);
    if (!hConnect) {
        logf("WinHttpConnect (ipify) failed: %lu", GetLastError());
        WinHttpCloseHandle(hSession);
        return "";
    }
    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"GET", NULL, NULL, NULL, NULL, 0);
    if (!hRequest) {
        logf("WinHttpOpenRequest (ipify) failed: %lu", GetLastError());
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return "";
    }
    if (!WinHttpSendRequest(hRequest, NULL, 0, NULL, 0, 0, 0)) {
        logf("WinHttpSendRequest (ipify) failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return "";
    }
    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        logf("WinHttpReceiveResponse (ipify) failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return "";
    }
    char buffer[64] = {0};
    DWORD bytesRead = 0;
    std::string result;
    while (WinHttpReadData(hRequest, buffer, sizeof(buffer)-1, &bytesRead) && bytesRead > 0) {
        buffer[bytesRead] = 0;
        result += buffer;
    }
    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);
    logf("External IP: %s", result.c_str());
    return result;
}

std::string getUsersAsString() {
    std::string psCommand = "$OutputEncoding = [console]::InputEncoding = [console]::OutputEncoding = New-Object System.Text.UTF8Encoding; "
                          "Get-LocalUser | Where-Object { $_.Enabled -eq $true } | ForEach-Object { $_.Name }";
    STARTUPINFOA si = {0};
    si.cb = sizeof(si);
    si.dwFlags = STARTF_USESHOWWINDOW;
    si.wShowWindow = SW_HIDE;
    SECURITY_ATTRIBUTES sa = {0};
    sa.nLength = sizeof(sa);
    sa.bInheritHandle = TRUE;
    HANDLE hRead, hWrite;
    CreatePipe(&hRead, &hWrite, &sa, 0);
    SetHandleInformation(hRead, HANDLE_FLAG_INHERIT, 0);
    std::string cmdLine = "powershell.exe -Command \"" + psCommand + "\"";
    PROCESS_INFORMATION pi = {0};
    char* cmd = _strdup(cmdLine.c_str());
    if (!CreateProcessA(NULL, cmd, NULL, NULL, TRUE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi)) {
        logf("CreateProcess failed: %lu", GetLastError());
        free(cmd);
        CloseHandle(hRead);
        CloseHandle(hWrite);
        return "";
    }
    free(cmd);
    CloseHandle(hWrite);
    char buffer[4096];
    DWORD bytesRead;
    std::string output;
    while (ReadFile(hRead, buffer, sizeof(buffer)-1, &bytesRead, NULL) && bytesRead > 0) {
        buffer[bytesRead] = 0;
        output += buffer;
    }
    CloseHandle(hRead);
    WaitForSingleObject(pi.hProcess, INFINITE);
    CloseHandle(pi.hProcess);
    CloseHandle(pi.hThread);
    std::string users;
    std::istringstream iss(output);
    std::string line;
    while (std::getline(iss, line)) {
        line = line.substr(0, line.find_last_not_of(" \n\r\t") + 1);
        if (!line.empty() && line != "Administrator" && line != "Guest") {
            if (!users.empty()) users += ", ";
            users += line;
        }
    }
    return users;
}

std::string jsonEscape(const std::string& s) {
    std::string out;
    for (char c : s) {
        switch (c) {
            case '\"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\b': out += "\\b"; break;
            case '\f': out += "\\f"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default: out += c;
        }
    }
    return out;
}

// ==================== HTTP CLIENT ====================

bool postJSON(const std::string& url, const std::string& bodyStr, std::string& responseBody, int& statusCode) {
    logf("HTTP POST to: %s", url.c_str());
    logf("Body: %s", bodyStr.c_str());
    
    // Parse URL manually to handle query parameters
    std::string host, path, query;
    int port;
    if (!parseUrl(url, host, port, path, query)) {
        logf("URL parsing failed: %s", url.c_str());
        return false;
    }
    
    std::string fullPath = path + query;
    logf("Host: %s, Path: %s, Port: %d", host.c_str(), fullPath.c_str(), port);

    HINTERNET hSession = WinHttpOpen(L"Agent/1.0", WINHTTP_ACCESS_TYPE_NO_PROXY, NULL, NULL, 0);
    if (!hSession) {
        logf("WinHttpOpen failed: %lu", GetLastError());
        return false;
    }

    std::wstring whost(host.begin(), host.end());
    HINTERNET hConnect = WinHttpConnect(hSession, whost.c_str(), port, 0);
    if (!hConnect) {
        logf("WinHttpConnect failed: %lu", GetLastError());
        WinHttpCloseHandle(hSession);
        return false;
    }

    // Use full path with query params for OpenRequest
    std::wstring wpath(fullPath.begin(), fullPath.end());
    
    // Determine if HTTPS is needed
    DWORD dwFlags = 0;
    if (url.find("https://") == 0) {
        dwFlags |= WINHTTP_FLAG_SECURE;
    }
    
    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"POST", wpath.c_str(), NULL, NULL, NULL, dwFlags);
    if (!hRequest) {
        logf("WinHttpOpenRequest failed: %lu", GetLastError());
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }
    
    // Ignore SSL certificate errors for self-signed certs in dev
    if (url.find("https://") == 0) {
        DWORD dwFlags = SECURITY_FLAG_IGNORE_UNKNOWN_CA |
                      SECURITY_FLAG_IGNORE_CERT_DATE_INVALID |
                      SECURITY_FLAG_IGNORE_CERT_CN_INVALID |
                      SECURITY_FLAG_IGNORE_CERT_WRONG_USAGE;
        WinHttpSetOption(hRequest, WINHTTP_OPTION_SECURITY_FLAGS, &dwFlags, sizeof(dwFlags));
    }

    std::wstring header = L"Content-Type: application/json\r\n";
    if (!WinHttpAddRequestHeaders(hRequest, header.c_str(), (DWORD)header.size(), WINHTTP_ADDREQ_FLAG_ADD)) {
        logf("WinHttpAddRequestHeaders failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }

    if (!WinHttpSendRequest(hRequest, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                           (LPVOID)bodyStr.c_str(), (DWORD)bodyStr.size(), (DWORD)bodyStr.size(), 0)) {
        logf("WinHttpSendRequest failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }

    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        logf("WinHttpReceiveResponse failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }

    DWORD dwStatusCode = 0;
    DWORD dwSize = sizeof(dwStatusCode);
    if (WinHttpQueryHeaders(hRequest, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER, NULL, &dwStatusCode, &dwSize, NULL)) {
        statusCode = (int)dwStatusCode;
    } else {
        statusCode = 0;
    }
    logf("Response status: %d", statusCode);

    char buffer[4096] = {0};
    DWORD bytesRead = 0;
    std::string response;
    while (WinHttpReadData(hRequest, buffer, sizeof(buffer) - 1, &bytesRead) && bytesRead > 0) {
        buffer[bytesRead] = 0;
        response += buffer;
    }
    responseBody = response;
    logf("Response body: %s", response.c_str());

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    return true;
}

// ==================== TELEMETRY ====================

struct TelemetryData {
    std::string system;
    std::string userName;
    std::string ipAddr;
    std::string externalIP;
    std::vector<std::string> disks;
    uint64_t totalMemory;
    uint64_t availableMemory;
};

std::string getTotalMemory() {
    MEMORYSTATUSEX memStatus = {0};
    memStatus.dwLength = sizeof(memStatus);
    if (!GlobalMemoryStatusEx(&memStatus)) return "0";
    return std::to_string(memStatus.ullTotalPhys / (1024 * 1024));
}

std::string getAvailableMemory() {
    MEMORYSTATUSEX memStatus = {0};
    memStatus.dwLength = sizeof(memStatus);
    if (!GlobalMemoryStatusEx(&memStatus)) return "0";
    return std::to_string(memStatus.ullAvailPhys / (1024 * 1024));
}

TelemetryData collectTelemetry() {
    TelemetryData data;
    data.system = "windows";
    data.userName = getUsersAsString();
    data.ipAddr = getLocalIP();
    data.externalIP = getExternalIP();
    data.disks = {};
    data.totalMemory = std::stoull(getTotalMemory());
    data.availableMemory = std::stoull(getAvailableMemory());
    return data;
}

// ==================== SHA256 ====================

std::string sha256File(const std::string& path) {
    logf("Calculating SHA256 for: %s", path.c_str());
    HCRYPTPROV hProv = 0;
    HCRYPTHASH hHash = 0;
    
    if (!CryptAcquireContext(&hProv, 0, 0, PROV_RSA_AES, CRYPT_VERIFYCONTEXT)) {
        logf("CryptAcquireContext failed: %lu", GetLastError());
        return "";
    }

    if (!CryptCreateHash(hProv, CALG_SHA_256, 0, 0, &hHash)) {
        logf("CryptCreateHash failed: %lu", GetLastError());
        CryptReleaseContext(hProv, 0);
        return "";
    }

    HANDLE hFile = CreateFileA(path.c_str(), GENERIC_READ, FILE_SHARE_READ, 0, OPEN_EXISTING, FILE_FLAG_SEQUENTIAL_SCAN, 0);
    if (hFile == INVALID_HANDLE_VALUE) {
        logf("CreateFile (sha256) failed: %lu", GetLastError());
        CryptDestroyHash(hHash);
        CryptReleaseContext(hProv, 0);
        return "";
    }

    BYTE rgbFile[4096];
    DWORD cbRead = 0;
    while (ReadFile(hFile, rgbFile, sizeof(rgbFile), &cbRead, NULL) && cbRead > 0) {
        if (!CryptHashData(hHash, rgbFile, cbRead, 0)) {
            logf("CryptHashData failed: %lu", GetLastError());
            CloseHandle(hFile);
            CryptDestroyHash(hHash);
            CryptReleaseContext(hProv, 0);
            return "";
        }
    }
    CloseHandle(hFile);

    BYTE rgbHash[32];
    DWORD cbHash = 32;
    if (!CryptGetHashParam(hHash, HP_HASHVAL, rgbHash, &cbHash, 0)) {
        logf("CryptGetHashParam failed: %lu", GetLastError());
        CryptDestroyHash(hHash);
        CryptReleaseContext(hProv, 0);
        return "";
    }

    CryptDestroyHash(hHash);
    CryptReleaseContext(hProv, 0);

    std::string result;
    CHAR rgbDigits[] = "0123456789abcdef";
    for (DWORD i = 0; i < cbHash; i++) {
        CHAR rgb[3];
        rgb[0] = rgbDigits[rgbHash[i] >> 4];
        rgb[1] = rgbDigits[rgbHash[i] & 0xf];
        rgb[2] = 0;
        result += rgb;
    }
    logf("SHA256: %s", result.c_str());
    return result;
}

// ==================== UPDATE ====================

void checkForUpdate(const std::string& uuid, const std::string& token) {
    std::string url = serverURL + "/api/agent/check-update?uuid=" + uuid + "&token=" + token;
    std::string body = "{\"build\":\"" + buildSlug + "\"}";
    std::string responseBody;
    int statusCode;
    
    if (!postJSON(url, body, responseBody, statusCode) || statusCode != 200) {
        logf("Update check failed: %s", responseBody.c_str());
        return;
    }

    if (responseBody.find("\"update\":true") == std::string::npos) return;

    logf("New version available: %s", responseBody.c_str());

    std::string newBuild, downloadUrl, sha256;
    size_t buildPos = responseBody.find("\"build\":\"");
    size_t urlPos = responseBody.find("\"url\":\"");
    size_t shaPos = responseBody.find("\"sha256\":\"");
    
    if (buildPos != std::string::npos && urlPos != std::string::npos && shaPos != std::string::npos) {
        buildPos += 9;  // length of "\"build\":\""
        urlPos += 7;    // length of "\"url\":\""
        shaPos += 10;   // length of "\"sha256\":\""
        size_t buildEnd = responseBody.find("\"", buildPos);
        size_t urlEnd = responseBody.find("\"", urlPos);
        size_t shaEnd = responseBody.find("\"", shaPos);
        if (buildEnd != std::string::npos && urlEnd != std::string::npos && shaEnd != std::string::npos) {
            newBuild = responseBody.substr(buildPos, buildEnd - buildPos);
            downloadUrl = responseBody.substr(urlPos, urlEnd - urlPos);
            sha256 = responseBody.substr(shaPos, shaEnd - shaPos);
        }
    }

    if (newBuild.empty() || downloadUrl.empty()) {
        log("Invalid update response");
        return;
    }

    logf("New build: %s, URL: %s, SHA256: %s", newBuild.c_str(), downloadUrl.c_str(), sha256.c_str());

    if (g_stopRequested) {
        log("Update cancelled: service stopping");
        return;
    }

    std::string exePath = getExePath();
    std::string tmpPath = exePath + ".new";

    // Download new version
    logf("Downloading from: %s", downloadUrl.c_str());

    // Parse download URL manually
    std::string host, path, query;
    int port;
    if (!parseUrl(downloadUrl, host, port, path, query)) {
        logf("URL parsing failed: %s", downloadUrl.c_str());
        return;
    }
    
    std::string fullPath = path + query;
    logf("Download - Host: %s, Path: %s, Port: %d", host.c_str(), fullPath.c_str(), port);

    HINTERNET hSession = WinHttpOpen(L"Agent/1.0", WINHTTP_ACCESS_TYPE_NO_PROXY, NULL, NULL, 0);
    if (!hSession) {
        logf("WinHttpOpen (download) failed: %lu", GetLastError());
        return;
    }

    std::wstring whost(host.begin(), host.end());
    HINTERNET hConnect = WinHttpConnect(hSession, whost.c_str(), port, 0);
    if (!hConnect) {
        logf("WinHttpConnect (download) failed: %lu", GetLastError());
        WinHttpCloseHandle(hSession);
        return;
    }

    logf("Download request path: %s", fullPath.c_str());
    std::wstring wpath(fullPath.begin(), fullPath.end());
    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"GET", wpath.c_str(), NULL, NULL, NULL, 0);
    if (!hRequest) {
        logf("WinHttpOpenRequest (download) failed: %lu", GetLastError());
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return;
    }

    if (!WinHttpSendRequest(hRequest, NULL, 0, NULL, 0, 0, 0)) {
        logf("WinHttpSendRequest (download) failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return;
    }

    if (!WinHttpReceiveResponse(hRequest, NULL)) {
        logf("WinHttpReceiveResponse (download) failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return;
    }

    DWORD dwStatusCode = 0;
    DWORD dwSize = sizeof(dwStatusCode);
    if (WinHttpQueryHeaders(hRequest, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER, NULL, &dwStatusCode, &dwSize, NULL)) {
        if (dwStatusCode != 200) {
            logf("Download failed: HTTP %d", dwStatusCode);
            WinHttpCloseHandle(hRequest);
            WinHttpCloseHandle(hConnect);
            WinHttpCloseHandle(hSession);
            return;
        }
    }

    HANDLE hFile = CreateFileA(tmpPath.c_str(), GENERIC_WRITE, 0, 0, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, 0);
    if (hFile == INVALID_HANDLE_VALUE) {
        logf("CreateFile (tmp) failed: %lu", GetLastError());
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return;
    }

    char buffer[4096];
    DWORD bytesRead = 0;
    while (WinHttpReadData(hRequest, buffer, sizeof(buffer), &bytesRead) && bytesRead > 0) {
        DWORD written = 0;
        WriteFile(hFile, buffer, bytesRead, &written, NULL);
    }
    CloseHandle(hFile);

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    // Verify SHA256
    std::string hash = sha256File(tmpPath);
    if (hash.empty() || hash != sha256) {
        logf("SHA256 mismatch! Expected: %s, Got: %s", sha256.c_str(), hash.c_str());
        DeleteFileA(tmpPath.c_str());
        return;
    }

    // Replace executable
    std::string oldPath = exePath + ".old";
    // Delete existing .old file if present
    DeleteFileA(oldPath.c_str());
    if (MoveFileA(exePath.c_str(), oldPath.c_str())) {
        if (MoveFileA(tmpPath.c_str(), exePath.c_str())) {
            log("Update successful, starting new version...");
            
            // Send telemetry with new version
            std::string telemetryBody = "{\"exe_version\":\"" + newBuild + "\"}";
            std::string dummy;
            int code;
            postJSON(serverURL + "/api/agent/telemetry?uuid=" + uuid + "&token=" + token, telemetryBody, dummy, code);

            // Schedule a service restart via a detached process
            // This avoids SCM conflicts when restarting from within the service
            STARTUPINFOA si = {0};
            si.cb = sizeof(si);
            si.dwFlags = STARTF_USESHOWWINDOW;
            si.wShowWindow = SW_HIDE;
            PROCESS_INFORMATION pi = {0};
            
            // Command: wait 2 seconds then restart service
            char cmd[MAX_PATH * 2];
            sprintf_s(cmd, sizeof(cmd), "cmd.exe /c \"timeout /t 2 /nobreak >nul && sc start SystemMonitoringAgent\"");
            
            if (CreateProcessA(NULL, cmd, NULL, NULL, FALSE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi)) {
                CloseHandle(pi.hProcess);
                CloseHandle(pi.hThread);
                log("Scheduled service restart in 2 seconds");
            } else {
                logf("Failed to schedule restart: %lu", GetLastError());
            }
            
            // Give time for the scheduled restart to execute
            Sleep(3000);
            ExitProcess(0);  // Clean exit
        } else {
            // Restore old file
            MoveFileA(oldPath.c_str(), exePath.c_str());
            log("Update failed: cannot move new exe");
        }
    } else {
        DeleteFileA(tmpPath.c_str());
        log("Update failed: cannot move current exe");
    }
}

// ==================== MAIN LOGIC ====================

void mainLogic() {
    logf("Agent started %s", buildSlug.c_str());
    logf("Server URL: %s", serverURL.c_str());

    if (serverURL.empty()) {
        log("ERROR: serverURL is empty!");
        return;
    }

    std::string machineUID = loadOrCreateMachineUID();
    logf("Machine UID: %s", machineUID.c_str());

    char hostname[256];
    DWORD size = sizeof(hostname);
    GetComputerNameA(hostname, &size);
    logf("Hostname: %s", hostname);

    std::string uuid, token;

    // Registration
    for (;;) {
        if (g_stopRequested) return;

        std::string url = serverURL + "/api/agent/register";
        logf("Registering at: %s", url.c_str());

        // Собираем все данные для регистрации
        TelemetryData telemetry = collectTelemetry();
        std::string osVersion = "Windows";  // TODO: получить точно через GetVersion()
        
        // Формируем полное JSON с обязательными полями
        std::string body = "{"
            "\"machine_uid\":\"" + jsonEscape(machineUID) + "\","
            "\"name_pc\":\"" + jsonEscape(std::string(hostname)) + "\","
            "\"exe_version\":\"" + jsonEscape(buildSlug) + "\","
            "\"system\":\"" + jsonEscape(osVersion) + "\","
            "\"user_name\":\"" + jsonEscape(telemetry.userName) + "\","
            "\"ip_addr\":\"" + jsonEscape(telemetry.ipAddr) + "\","
            "\"external_ip\":\"" + jsonEscape(telemetry.externalIP) + "\","
            "\"total_memory\":" + std::to_string(telemetry.totalMemory) + ","
            "\"available_memory\":" + std::to_string(telemetry.availableMemory) + ","
            "\"disks\":"
            "}";

        logf("Register body: %s", body.c_str());

        std::string responseBody;
        int statusCode;

        if (postJSON(url, body, responseBody, statusCode)) {
            logf("Register response status: %d", statusCode);
            logf("Register response body: %s", responseBody.c_str());
        } else {
            log("Register HTTP request failed");
        }

        if (statusCode == 200) {
            size_t uuidPos = responseBody.find("\"agent_uuid\":\"");
            size_t tokenPos = responseBody.find("\"token\":\"");
            if (uuidPos != std::string::npos && tokenPos != std::string::npos) {
                uuidPos += 14; // length of "agent_uuid":"
                tokenPos += 9;  // length of "token":"
                size_t uuidEnd = responseBody.find("\"", uuidPos);
                size_t tokenEnd = responseBody.find("\"", tokenPos);
                if (uuidEnd != std::string::npos && tokenEnd != std::string::npos) {
                    uuid = responseBody.substr(uuidPos, uuidEnd - uuidPos);
                    token = responseBody.substr(tokenPos, tokenEnd - tokenPos);
                    logf("Registered! UUID: %s, Token: %s", uuid.c_str(), token.c_str());
                    break;
                }
            }
        }

        log("Registration failed, retrying in 10 seconds...");
        for (int i = 0; i < 10 && !g_stopRequested; i++) {
            std::this_thread::sleep_for(std::chrono::seconds(1));
        }
    }

    if (g_stopRequested) return;

    // Telemetry
    log("Sending telemetry...");
    TelemetryData telemetry = collectTelemetry();
    telemetry.system = buildSlug;

    std::string telemetryBody = "{\"system\":\"" + telemetry.system + "\","
                               "\"user_name\":\"" + jsonEscape(telemetry.userName) + "\","
                               "\"ip_addr\":\"" + telemetry.ipAddr + "\","
                               "\"external_ip\":\"" + telemetry.externalIP + "\","
                               "\"total_memory\":" + std::to_string(telemetry.totalMemory) + ","
                               "\"available_memory\":" + std::to_string(telemetry.availableMemory) + ","
                               "\"exe_version\":\"" + buildSlug + "\"}";

    std::string responseBody;
    int statusCode;
    postJSON(serverURL + "/api/agent/telemetry?uuid=" + uuid + "&token=" + token, telemetryBody, responseBody, statusCode);

    // Main loop
    log("Entering main loop...");
    while (!g_stopRequested) {
        // Heartbeat every 10 seconds
        for (int i = 0; i < 10 && !g_stopRequested; i++) {
            std::this_thread::sleep_for(std::chrono::seconds(1));
        }

        if (g_stopRequested) break;

        log("Sending heartbeat...");
        std::string dummy;
        int code;
        std::string hbBody = "{}";
        postJSON(serverURL + "/api/agent/heartbeat?uuid=" + uuid + "&token=" + token, hbBody, dummy, code);
        
        // Parse telemetry_mode from heartbeat response (dummy contains response)
        size_t modePos = dummy.find("\"telemetry_mode\":\"");
        if (modePos != std::string::npos) {
            modePos += 18; // length of "telemetry_mode":"
            size_t modeEnd = dummy.find("\"", modePos);
            if (modeEnd != std::string::npos) {
                g_telemetryMode = dummy.substr(modePos, modeEnd - modePos);
                logf("Telemetry mode: %s", g_telemetryMode.c_str());
            }
        }

        // Send telemetry if mode is "full"
        if (g_telemetryMode == "full" && !g_stopRequested) {
            log("Sending telemetry (full mode)...");
            TelemetryData telemetry = collectTelemetry();
            telemetry.system = buildSlug;
            
            std::string telemetryBody = "{\"system\":\"" + telemetry.system + "\","
                                       "\"user_name\":\"" + jsonEscape(telemetry.userName) + "\","
                                       "\"ip_addr\":\"" + telemetry.ipAddr + "\","
                                       "\"external_ip\":\"" + telemetry.externalIP + "\","
                                       "\"total_memory\":" + std::to_string(telemetry.totalMemory) + ","
                                       "\"available_memory\":" + std::to_string(telemetry.availableMemory) + ","
                                       "\"exe_version\":\"" + buildSlug + "\"}";
            postJSON(serverURL + "/api/agent/telemetry?uuid=" + uuid + "&token=" + token, telemetryBody, dummy, code);
        }

        // Update check every 60 seconds
        for (int i = 0; i < 50 && !g_stopRequested; i++) {
            std::this_thread::sleep_for(std::chrono::seconds(1));
        }

        if (!g_stopRequested) {
            checkForUpdate(uuid, token);
        }
    }
}

// ==================== SERVICE ====================

VOID WINAPI serviceCtrlHandler(DWORD ctrlCode) {
    if (ctrlCode == SERVICE_CONTROL_STOP) {
        log("Service stop requested");
        serviceStatus.dwCurrentState = SERVICE_STOP_PENDING;
        SetServiceStatus(serviceHandle, &serviceStatus);
        g_stopRequested = true;
        if (stopEvent) SetEvent(stopEvent);
    }
}

VOID WINAPI serviceMain(DWORD argc, LPWSTR* argv) {
    serviceStatus.dwServiceType = SERVICE_WIN32;
    serviceStatus.dwCurrentState = SERVICE_START_PENDING;
    serviceStatus.dwControlsAccepted = SERVICE_ACCEPT_STOP;
    serviceStatus.dwWin32ExitCode = 0;
    serviceStatus.dwServiceSpecificExitCode = 0;

    serviceHandle = RegisterServiceCtrlHandlerW(L"SystemMonitoringAgent", serviceCtrlHandler);
    if (!serviceHandle) {
        log("RegisterServiceCtrlHandler failed");
        return;
    }

    SetServiceStatus(serviceHandle, &serviceStatus);

    stopEvent = CreateEvent(NULL, TRUE, FALSE, NULL);
    if (!stopEvent) {
        log("CreateEvent failed");
        serviceStatus.dwCurrentState = SERVICE_STOPPED;
        SetServiceStatus(serviceHandle, &serviceStatus);
        return;
    }

    serviceStatus.dwCurrentState = SERVICE_RUNNING;
    SetServiceStatus(serviceHandle, &serviceStatus);

    log("Service main started");
    mainLogic();

    CloseHandle(stopEvent);
    serviceStatus.dwCurrentState = SERVICE_STOPPED;
    SetServiceStatus(serviceHandle, &serviceStatus);
    log("Service stopped");
}

bool installService() {
    std::string exePath = getExePath();
    logf("Installing service, path: %s", exePath.c_str());
    
    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CREATE_SERVICE);
    if (!scm) {
        logf("OpenSCManager failed: %lu", GetLastError());
        return false;
    }
    SC_HANDLE svc = CreateServiceA(
        scm, "SystemMonitoringAgent", "System Monitoring Agent",
        SERVICE_ALL_ACCESS, SERVICE_WIN32_OWN_PROCESS,
        SERVICE_AUTO_START, SERVICE_ERROR_NORMAL,
        exePath.c_str(), NULL, NULL, NULL, NULL, NULL
    );
    if (!svc) {
        logf("CreateService failed: %lu", GetLastError());
        CloseServiceHandle(scm);
        return false;
    }
    
    // Setup recovery options: restart on failure
    SERVICE_FAILURE_ACTIONS actions = {0};
    SC_ACTION action = { SC_ACTION_RESTART, 1000 }; // Restart after 1 second
    actions.cActions = 1;
    actions.lpsaActions = &action;
    actions.dwResetPeriod = 86400; // Reset failure count after 1 day
    ChangeServiceConfig2A(svc, SERVICE_CONFIG_FAILURE_ACTIONS, &actions);
    
    CloseServiceHandle(svc);
    CloseServiceHandle(scm);
    log("Service installed successfully with auto-restart");
    return true;
}

bool isServiceInstalled() {
    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_ENUMERATE_SERVICE);
    if (!scm) return false;
    SC_HANDLE svc = OpenServiceA(scm, "SystemMonitoringAgent", SERVICE_QUERY_CONFIG);
    bool exists = (svc != NULL);
    if (svc) CloseServiceHandle(svc);
    CloseServiceHandle(scm);
    return exists;
}

int main(int argc, char* argv[]) {
    setupFileLogger();
    log("Agent started as console app");

    if (!isServiceInstalled()) {
        if (!installService()) {
            log("Warning: Could not install service");
        } else {
            SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
            if (scm) {
                SC_HANDLE svc = OpenServiceA(scm, "SystemMonitoringAgent", SERVICE_START);
                if (svc) {
                    StartServiceA(svc, 0, NULL);
                    CloseServiceHandle(svc);
                }
                CloseServiceHandle(scm);
            }
        }
        return 0;
    }

    SERVICE_TABLE_ENTRYW table[] = {
        {(LPWSTR)L"SystemMonitoringAgent", serviceMain},
        {NULL, NULL}
    };

    log("Starting service dispatcher...");
    StartServiceCtrlDispatcherW(table);
    log("Service dispatcher exited");
    return 0;
}
