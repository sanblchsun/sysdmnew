#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <winsvc.h>
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

#pragma comment(lib, "winhttp.lib")
#pragma comment(lib, "advapi32.lib")
#pragma comment(lib, "ws2_32.lib")

std::string serverURL;
std::string buildSlug;

std::mutex logMutex;
std::ofstream logFile;

std::string getExePath() {
    char path[MAX_PATH] = {};
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
    if (!logFile.is_open()) {
        return;
    }
}

void log(const char* msg) {
    std::lock_guard<std::mutex> lock(logMutex);

    auto now = std::chrono::system_clock::now();
    auto time = std::chrono::system_clock::to_time_t(now);
    struct tm tmtemp;
    localtime_s(&tmtemp, &time);

    char timeStr[32];
    strftime(timeStr, sizeof(timeStr), "%Y-%m-%d %H:%M:%S", &tmtemp);

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
    vsnprintf_s(buf, sizeof(buf), fmt, args);
    va_end(args);
    log(buf);
}

std::string loadOrCreateMachineUID() {
    std::string exeDir = getExeDir();
    std::string uidPath = exeDir + "\\machine_uid";

    std::ifstream f(uidPath);
    if (f.is_open()) {
        std::string uid;
        std::getline(f, uid);
        f.close();
        return uid;
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

std::string getLocalIP() {
    WSADATA wsaData;
    if (WSAStartup(MAKEWORD(2, 2), &wsaData) != 0) {
        return "";
    }

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
        struct in_addr** addrList = (struct in_addr**)he->h_addr_list[i];
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
    HINTERNET hSession = WinHttpOpen(L"Agent/1.0", WINHTTP_ACCESS_TYPE_NO_PROXY, NULL, NULL, 0);
    if (!hSession) return "";

    HINTERNET hConnect = WinHttpConnect(hSession, L"api.ipify.org", INTERNET_DEFAULT_HTTP_PORT, 0);
    if (!hConnect) {
        WinHttpCloseHandle(hSession);
        return "";
    }

    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"GET", NULL, NULL, NULL, NULL, 0);
    if (!hRequest) {
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return "";
    }

    BOOL success = WinHttpSendRequest(hRequest, NULL, 0, NULL, 0, 0, 0);
    if (!success) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return "";
    }

    WinHttpReceiveResponse(hRequest, NULL);

    char buffer[64] = {};
    DWORD bufferSize = sizeof(buffer);
    WinHttpReadData(hRequest, buffer, bufferSize, &bufferSize);

    std::string result = buffer;
    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    return result;
}

std::string getUsersAsString() {
    std::string psCommand = "$OutputEncoding = [console]::InputEncoding = [console]::OutputEncoding = New-Object System.Text.UTF8Encoding; "
                          "Get-LocalUser | Where-Object { $_.Enabled -eq $true } | ForEach-Object { $_.Name }";

    STARTUPINFOA si = {sizeof(si)};
    si.dwFlags = STARTF_USESHOWWINDOW;
    si.wShowWindow = SW_HIDE;

    SECURITY_ATTRIBUTES sa = {sizeof(sa)};
    sa.bInheritHandle = TRUE;

    HANDLE hRead, hWrite;
    CreatePipe(&hRead, &hWrite, &sa, 0);
    SetHandleInformation(hRead, HANDLE_FLAG_INHERIT, 0);

    si.hStdOutput = hWrite;
    si.hStdError = hWrite;

    PROCESS_INFORMATION pi = {};
    char cmd[512];
    sprintf_s(cmd, "powershell.exe -Command \"%s\"", psCommand.c_str());

    if (!CreateProcessA(NULL, cmd, NULL, NULL, TRUE, CREATE_NO_WINDOW, NULL, NULL, &si, &pi)) {
        CloseHandle(hWrite);
        CloseHandle(hRead);
        return "";
    }

    CloseHandle(hWrite);

    char buffer[4096] = {};
    DWORD bytesRead;
    std::string output;
    while (ReadFile(hRead, buffer, sizeof(buffer) - 1, &bytesRead, NULL) && bytesRead > 0) {
        buffer[bytesRead] = 0;
        output += buffer;
    }

    CloseHandle(hRead);
    WaitForSingleObject(pi.hProcess, INFINITE);
    CloseHandle(pi.hProcess);
    CloseHandle(pi.hThread);

    std::istringstream iss(output);
    std::string line;
    std::string result;
    while (std::getline(iss, line)) {
        std::string trimmed = line;
        trimmed.erase(trimmed.find_last_not_of(" \t\r\n") + 1);
        trimmed.erase(0, trimmed.find_first_not_of(" \t\r\n"));

        if (trimmed.empty()) continue;

        std::string lower;
        for (char c : trimmed) lower += std::tolower(c);

        bool isSystem = false;
        const char* systems[] = {"administrator", "guest", "defaultaccount", "wdagutilityaccount", "system", "network service", "local service"};
        for (const char* sys : systems) {
            if (lower.find(sys) != std::string::npos) {
                isSystem = true;
                break;
            }
        }
        if (!isSystem) {
            if (!result.empty()) result += ", ";
            result += trimmed;
        }
    }

    return result;
}

struct DiskInfo {
    std::string name;
    long long size;
    long long free;
};

struct TelemetryData {
    std::string system;
    std::string userName;
    std::string ipAddr;
    std::string externalIP;
    std::vector<DiskInfo> disks;
    long long totalMemory;
    long long availableMemory;
};

std::string toString(size_t n) {
    std::ostringstream oss;
    oss << n;
    return oss.str();
}

long long getTotalMemory() {
    MEMORYSTATUSEX statex;
    statex.dwLength = sizeof(statex);
    if (GlobalMemoryStatusEx(&statex)) {
        return (long long)(statex.ullTotalPhys / (1024 * 1024));
    }
    return 0;
}

long long getAvailableMemory() {
    MEMORYSTATUSEX statex;
    statex.dwLength = sizeof(statex);
    if (GlobalMemoryStatusEx(&statex)) {
        return (long long)(statex.ullAvailPhys / (1024 * 1024));
    }
    return 0;
}

std::vector<DiskInfo> getDisks() {
    std::vector<DiskInfo> result;
    char drive[] = "A:\\";
    for (char d = 'A'; d <= 'Z'; d++) {
        drive[0] = d;
        if (GetDriveTypeA(drive) == DRIVE_FIXED) {
            ULARGE_INTEGER freeBytes, totalBytes, totalFree;
            if (GetDiskFreeSpaceExA(drive, &freeBytes, &totalBytes, &totalFree)) {
                DiskInfo info;
                info.name = std::string(drive);
                info.size = (long long)(totalBytes.QuadPart / (1024 * 1024 * 1024));
                info.free = (long long)(freeBytes.QuadPart / (1024 * 1024 * 1024));
                result.push_back(info);
            }
        }
    }
    return result;
}

TelemetryData collectTelemetry() {
    TelemetryData data;
    data.system = "windows";
    data.userName = getUsersAsString();
    data.ipAddr = getLocalIP();
    data.externalIP = getExternalIP();
    data.disks = getDisks();
    data.totalMemory = getTotalMemory();
    data.availableMemory = getAvailableMemory();
    return data;
}

bool postJSON(const std::string& url, const std::string& bodyStr, std::string& responseBody, int& statusCode) {
    URL_COMPONENTS urlComp = {};
    urlComp.dwStructSize = sizeof(urlComp);

    wchar_t host[256] = {};
    wchar_t path[512] = {};
    urlComp.lpszHostName = host;
    urlComp.lpszUrlPath = path;
    urlComp.dwHostNameLength = 256;
    urlComp.dwUrlPathLength = 512;

    size_t protoEnd = url.find("://");
    if (protoEnd == std::string::npos) {
        protoEnd = url.find("//");
    }
    std::string urlNoProto = (protoEnd != std::string::npos) ? url.substr(protoEnd + 2) : url;
    std::wstring wurl(urlNoProto.begin(), urlNoProto.end());
    if (!WinHttpCrackUrl(wurl.c_str(), wurl.size(), 0, &urlComp)) {
        return false;
    }

    HINTERNET hSession = WinHttpOpen(L"Agent/1.0", WINHTTP_ACCESS_TYPE_NO_PROXY, NULL, NULL, 0);
    if (!hSession) return false;

    HINTERNET hConnect = WinHttpConnect(hSession, host, urlComp.nPort, 0);
    if (!hConnect) {
        WinHttpCloseHandle(hSession);
        return false;
    }

    HINTERNET hRequest = WinHttpOpenRequest(hConnect, L"POST", path, NULL, NULL, NULL, 0);
    if (!hRequest) {
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }

    std::wstring wBody(bodyStr.begin(), bodyStr.end());
    if (!WinHttpSendRequest(hRequest, L"Content-Type: application/json\r\n", -1,
                      (LPVOID)wBody.c_str(), wBody.size(), wBody.size(), 0)) {
        WinHttpCloseHandle(hRequest);
        WinHttpCloseHandle(hConnect);
        WinHttpCloseHandle(hSession);
        return false;
    }

    WinHttpReceiveResponse(hRequest, NULL);

    char buffer[4096] = {};
    DWORD bytesRead = 0;
    std::string response;
    while (WinHttpReadData(hRequest, buffer, sizeof(buffer) - 1, &bytesRead) && bytesRead > 0) {
        buffer[bytesRead] = 0;
        response += buffer;
    }

    statusCode = 200;
    responseBody = response;

    WinHttpCloseHandle(hRequest);
    WinHttpCloseHandle(hConnect);
    WinHttpCloseHandle(hSession);

    return true;
}

struct UpdateResponse {
    bool update;
    std::string build;
    std::string url;
    std::string sha256;
    bool force;
};

void checkForUpdate(const std::string& uuid, const std::string& token) {
    std::string url = serverURL + "/api/agent/check-update?uuid=" + uuid + "&token=" + token;

    std::string body = "{\"build\":\"" + buildSlug + "\"}";
    std::string responseBody;
    int statusCode;

    if (!postJSON(url, body, responseBody, statusCode) || statusCode != 200) {
        logf("Update check failed: %s", responseBody.c_str());
        return;
    }

    bool hasUpdate = responseBody.find("\"update\":true") != std::string::npos;

    if (!hasUpdate) {
        return;
    }

    logf("New version available");
}

void mainLogic() {
    logf("Agent started %s", buildSlug.c_str());

    std::string machineUID = loadOrCreateMachineUID();
    char hostname[256];
    DWORD size = sizeof(hostname);
    GetComputerNameA(hostname, &size);

    std::string uuid, token;

    for (;;) {
        std::string body = "{"
            "\"name_pc\":\"" + std::string(hostname) + "\","
            "\"machine_uid\":\"" + machineUID + "\","
            "\"exe_version\":\"" + buildSlug + "\","
            "\"external_ip\":\"" + getExternalIP() + "\""
            "}";

        std::string responseBody;
        int statusCode;

        if (postJSON(serverURL + "/api/agent/register", body, responseBody, statusCode) && statusCode == 200) {
            size_t uuidPos = responseBody.find("\"agent_uuid\":\"");
            size_t tokenPos = responseBody.find("\"token\":\"");
            if (uuidPos != std::string::npos && tokenPos != std::string::npos) {
                uuidPos += 13;
                tokenPos += 8;
                size_t uuidEnd = responseBody.find("\"", uuidPos);
                size_t tokenEnd = responseBody.find("\"", tokenPos);
                uuid = responseBody.substr(uuidPos, uuidEnd - uuidPos);
                token = responseBody.substr(tokenPos, tokenEnd - tokenPos);
                break;
            }
        }

        std::this_thread::sleep_for(std::chrono::seconds(10));
    }

    TelemetryData telemetry = collectTelemetry();
    telemetry.system = buildSlug;

    std::string telemetryBody = "{"
        "\"system\":\"" + telemetry.system + "\","
        "\"user_name\":\"" + telemetry.userName + "\","
        "\"ip_addr\":\"" + telemetry.ipAddr + "\","
        "\"external_ip\":\"" + telemetry.externalIP + "\","
        "\"total_memory\":" + toString(telemetry.totalMemory) + ","
        "\"available_memory\":" + toString(telemetry.availableMemory) + ","
        "\"exe_version\":\"" + buildSlug + "\""
        "}";

    std::string responseBody;
    int statusCode;
    postJSON(serverURL + "/api/agent/telemetry?uuid=" + uuid + "&token=" + token, telemetryBody, responseBody, statusCode);

    std::thread heartbeatThread([&uuid, &token]() {
        for (;;) {
            std::this_thread::sleep_for(std::chrono::seconds(10));
            std::string hUrl = serverURL + "/api/agent/heartbeat?uuid=" + uuid + "&token=" + token;
            std::string dummy;
            int code;
            postJSON(hUrl, "{}", dummy, code);
        }
    });

    std::thread updateThread([&uuid, &token]() {
        for (;;) {
            std::this_thread::sleep_for(std::chrono::seconds(60));
            checkForUpdate(uuid, token);
        }
    });

    heartbeatThread.join();
    updateThread.join();
}

SERVICE_STATUS serviceStatus;
SERVICE_STATUS_HANDLE serviceHandle;
HANDLE stopEvent = NULL;

VOID WINAPI serviceCtrlHandler(DWORD ctrlCode) {
    if (ctrlCode == SERVICE_CONTROL_STOP) {
        serviceStatus.dwCurrentState = SERVICE_STOP_PENDING;
        SetServiceStatus(serviceHandle, &serviceStatus);
        SetEvent(stopEvent);
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
        return;
    }

    SetServiceStatus(serviceHandle, &serviceStatus);

    stopEvent = CreateEvent(NULL, TRUE, FALSE, NULL);
    if (!stopEvent) {
        serviceStatus.dwCurrentState = SERVICE_STOPPED;
        SetServiceStatus(serviceHandle, &serviceStatus);
        return;
    }

    serviceStatus.dwCurrentState = SERVICE_RUNNING;
    SetServiceStatus(serviceHandle, &serviceStatus);

    mainLogic();

    CloseHandle(stopEvent);
    serviceStatus.dwCurrentState = SERVICE_STOPPED;
    SetServiceStatus(serviceHandle, &serviceStatus);
}

bool installService() {
    std::string exePath = getExePath();

    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CREATE_SERVICE);
    if (!scm) {
        return false;
    }

    SC_HANDLE svc = CreateServiceA(
        scm,
        "SystemMonitoringAgent",
        "System Monitoring Agent",
        SERVICE_ALL_ACCESS,
        SERVICE_WIN32_OWN_PROCESS,
        SERVICE_DEMAND_START,
        SERVICE_ERROR_NORMAL,
        exePath.c_str(),
        NULL, NULL, NULL, NULL, NULL
    );

    if (!svc) {
        CloseServiceHandle(scm);
        return false;
    }

    CloseServiceHandle(svc);
    CloseServiceHandle(scm);
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
    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "--server-url") == 0 && i + 1 < argc) {
            serverURL = argv[++i];
        } else if (strcmp(argv[i], "--build-slug") == 0 && i + 1 < argc) {
            buildSlug = argv[++i];
        }
    }

    setupFileLogger();

    char logBuf[256];
    snprintf(logBuf, sizeof(logBuf), "Agent starting, build: %s", buildSlug.c_str());
    log(logBuf);

    if (!isServiceInstalled()) {
        if (!installService()) {
            log("Warning: Could not install service");
        }
        if (StartServiceA(NULL, 0, NULL)) {
            return 0;
        }
    }

    SERVICE_TABLE_ENTRYW table[] = {
        {(LPWSTR)L"SystemMonitoringAgent", serviceMain},
        {NULL, NULL}
    };

    StartServiceCtrlDispatcherW(table);

    return 0;
}