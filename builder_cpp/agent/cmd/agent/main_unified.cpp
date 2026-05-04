// Unified Windows Agent: sysdmnew (registration, update) + rmm_cpp (streaming, control)
// Compile-time parameters: SERVER_URL, BUILD_SLUG, AGENT_ID
// TLS via Schannel (no external crypto), ffmpeg for video, WebSocket for control

#define SECURITY_WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <winsvc.h>
#include <security.h>
#include <schannel.h>
#include <wincrypt.h>
#include <winhttp.h>
#include <iostream>
#include <fstream>
#include <string>
#include <vector>
#include <sstream>
#include <atomic>
#include <mutex>
#include <thread>
#include <chrono>
#include <cctype>
#include <random>
#include <algorithm>
#include <unordered_map>
#include <ctime>
#include <iomanip>

#pragma comment(lib, "ws2_32.lib")
#pragma comment(lib, "Secur32.lib")
#pragma comment(lib, "user32.lib")
#pragma comment(lib, "winhttp.lib")
#pragma comment(lib, "advapi32.lib")
#pragma comment(lib, "crypt32.lib")

// ===== COMPILE-TIME CONFIGURATION =====
#ifndef SERVER_URL
#define SERVER_URL "https://dev.local"
#endif
#ifndef BUILD_SLUG
#define BUILD_SLUG "1.0.0"
#endif
#ifndef AGENT_ID
#define AGENT_ID "agent_universal"
#endif

// ===== LOGGING =====
static std::ofstream g_logFile;
static std::mutex g_logMutex;

static void setupFileLogger(const std::string &logPath)
{
    std::lock_guard<std::mutex> lk(g_logMutex);
    if (g_logFile.is_open())
        g_logFile.close();
    g_logFile.open(logPath, std::ios::app);
}

static void log(const std::string &msg)
{
    auto now = std::time(nullptr);
    auto tm = *std::localtime(&now);
    std::ostringstream oss;
    oss << std::put_time(&tm, "%Y-%m-%d %H:%M:%S") << " [agent] " << msg;
    std::string line = oss.str();
    {
        std::lock_guard<std::mutex> lk(g_logMutex);
        if (g_logFile.is_open())
            g_logFile << line << std::endl;
        g_logFile.flush();
    }
    std::cerr << line << std::endl;
}

static void logf(const char *fmt, ...)
{
    char buf[2048];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(buf, sizeof buf, fmt, ap);
    va_end(ap);
    log(buf);
}

// ===== GLOBAL STATE (sysdmnew) =====
static std::atomic<bool> g_stopRequested(false);
static std::string g_telemetryMode = "none";
static std::string g_agentUUID;
static std::string g_agentToken;

// ===== GLOBAL STATE (rmm_cpp) =====
static std::atomic<int> g_screen_w{1920};
static std::atomic<int> g_screen_h{1080};
static std::atomic<int> g_screen_origin_x{0};
static std::atomic<int> g_screen_origin_y{0};

static std::mutex g_clip_m;
static std::string g_last_clip;

// ===== RAW TCP HELPERS =====
static bool send_all_raw(SOCKET s, const char *p, int n)
{
    while (n > 0)
    {
        int k = send(s, p, n, 0);
        if (k <= 0)
            return false;
        p += k;
        n -= k;
    }
    return true;
}

static int recv_n_raw(SOCKET s, char *p, int n)
{
    int got = 0;
    while (got < n)
    {
        int k = recv(s, p + got, n - got, 0);
        if (k <= 0)
            return got;
        got += k;
    }
    return got;
}

static SOCKET tcp_connect(const std::string &host, int port)
{
    addrinfo hints{}, *res = NULL;
    hints.ai_family = AF_INET;
    hints.ai_socktype = SOCK_STREAM;
    std::string p = std::to_string(port);
    if (getaddrinfo(host.c_str(), p.c_str(), &hints, &res) != 0)
        return INVALID_SOCKET;
    SOCKET s = INVALID_SOCKET;
    for (auto *a = res; a; a = a->ai_next)
    {
        s = socket(a->ai_family, a->ai_socktype, a->ai_protocol);
        if (s == INVALID_SOCKET)
            continue;
        if (connect(s, a->ai_addr, (int)a->ai_addrlen) == 0)
            break;
        closesocket(s);
        s = INVALID_SOCKET;
    }
    freeaddrinfo(res);
    if (s != INVALID_SOCKET)
    {
        int one = 1;
        setsockopt(s, IPPROTO_TCP, TCP_NODELAY, (char *)&one, sizeof one);
    }
    return s;
}

// ===== TLS / SCHANNEL =====
struct TlsConn
{
    SOCKET sock = INVALID_SOCKET;
    CredHandle cred = {};
    CtxtHandle ctx = {};
    bool cred_ok = false;
    bool ctx_ok = false;
    SecPkgContext_StreamSizes sizes = {};
    std::vector<uint8_t> raw;
    std::vector<uint8_t> plain;
};

static void tls_close(TlsConn *c)
{
    if (!c)
        return;
    if (c->ctx_ok)
    {
        DeleteSecurityContext(&c->ctx);
        c->ctx_ok = false;
    }
    if (c->cred_ok)
    {
        FreeCredentialHandle(&c->cred);
        c->cred_ok = false;
    }
    if (c->sock != INVALID_SOCKET)
    {
        closesocket(c->sock);
        c->sock = INVALID_SOCKET;
    }
}

static bool tls_handshake(TlsConn *c, const std::string &host, bool verify_cert)
{
    SCHANNEL_CRED sc{};
    sc.dwVersion = SCHANNEL_CRED_VERSION;
    sc.dwFlags = SCH_CRED_NO_DEFAULT_CREDS;
    if (verify_cert)
        sc.dwFlags |= SCH_CRED_AUTO_CRED_VALIDATION;
    else
        sc.dwFlags |= SCH_CRED_MANUAL_CRED_VALIDATION;

    SECURITY_STATUS ss = AcquireCredentialsHandleA(
        NULL, const_cast<char *>(UNISP_NAME_A),
        SECPKG_CRED_OUTBOUND, NULL, &sc, NULL, NULL, &c->cred, NULL);
    if (ss != SEC_E_OK)
    {
        logf("AcquireCredentialsHandle failed: 0x%lx", (DWORD)ss);
        return false;
    }
    c->cred_ok = true;

    const DWORD req_flags =
        ISC_REQ_SEQUENCE_DETECT | ISC_REQ_REPLAY_DETECT |
        ISC_REQ_CONFIDENTIALITY | ISC_RET_EXTENDED_ERROR |
        ISC_REQ_ALLOCATE_MEMORY | ISC_REQ_STREAM;

    std::wstring whost(host.begin(), host.end());

    SecBuffer out_b = {0, SECBUFFER_TOKEN, NULL};
    SecBufferDesc out_d = {SECBUFFER_VERSION, 1, &out_b};
    DWORD ret_flags = 0;

    ss = InitializeSecurityContextW(
        &c->cred, NULL, const_cast<wchar_t *>(whost.c_str()),
        req_flags, 0, SECURITY_NATIVE_DREP,
        NULL, 0, &c->ctx, &out_d, &ret_flags, NULL);
    c->ctx_ok = true;

    if (out_b.pvBuffer && out_b.cbBuffer > 0)
    {
        bool ok = send_all_raw(c->sock, (const char *)out_b.pvBuffer, (int)out_b.cbBuffer);
        FreeContextBuffer(out_b.pvBuffer);
        out_b.pvBuffer = NULL;
        if (!ok)
            return false;
    }
    if (ss != SEC_I_CONTINUE_NEEDED)
    {
        logf("ISC initial failed: 0x%lx", (DWORD)ss);
        return false;
    }

    std::vector<uint8_t> in_buf;
    char tmp[16384];

    while (true)
    {
        int n = recv(c->sock, tmp, sizeof tmp, 0);
        if (n <= 0)
        {
            log("tls_handshake: socket closed");
            return false;
        }
        in_buf.insert(in_buf.end(), tmp, tmp + n);

    retry:
        SecBuffer in_bufs[2] = {
            {(ULONG)in_buf.size(), SECBUFFER_TOKEN, in_buf.data()},
            {0, SECBUFFER_EMPTY, NULL}};
        SecBufferDesc in_d = {SECBUFFER_VERSION, 2, in_bufs};
        out_b = {0, SECBUFFER_TOKEN, NULL};
        out_d = {SECBUFFER_VERSION, 1, &out_b};
        ret_flags = 0;

        ss = InitializeSecurityContextW(
            &c->cred, &c->ctx, NULL,
            req_flags, 0, SECURITY_NATIVE_DREP,
            &in_d, 0, NULL, &out_d, &ret_flags, NULL);

        if (out_b.pvBuffer && out_b.cbBuffer > 0)
        {
            bool ok = send_all_raw(c->sock, (const char *)out_b.pvBuffer, (int)out_b.cbBuffer);
            FreeContextBuffer(out_b.pvBuffer);
            out_b.pvBuffer = NULL;
            if (!ok)
                return false;
        }

        if (in_bufs[1].BufferType == SECBUFFER_EXTRA && in_bufs[1].cbBuffer > 0)
        {
            size_t off = in_buf.size() - in_bufs[1].cbBuffer;
            std::vector<uint8_t> extra(in_buf.begin() + (ptrdiff_t)off, in_buf.end());
            in_buf = std::move(extra);
        }
        else if (ss != SEC_E_INCOMPLETE_MESSAGE)
        {
            in_buf.clear();
        }

        if (ss == SEC_E_OK)
            break;
        if (ss == SEC_I_CONTINUE_NEEDED)
            continue;
        if (ss == SEC_E_INCOMPLETE_MESSAGE)
        {
            int n = recv(c->sock, tmp, sizeof tmp, 0);
            if (n <= 0)
            {
                log("tls_handshake: socket closed (incomplete)");
                return false;
            }
            in_buf.insert(in_buf.end(), tmp, tmp + n);
            goto retry;
        }
        logf("TLS handshake error: 0x%lx", (DWORD)ss);
        return false;
    }

    if (!in_buf.empty())
        c->raw = std::move(in_buf);

    QueryContextAttributes(&c->ctx, SECPKG_ATTR_STREAM_SIZES, &c->sizes);
    return true;
}

static TlsConn *tls_connect(const std::string &host, int port, bool verify_cert)
{
    SOCKET s = tcp_connect(host, port);
    if (s == INVALID_SOCKET)
        return nullptr;

    TlsConn *c = new TlsConn();
    c->sock = s;
    if (!tls_handshake(c, host, verify_cert))
    {
        tls_close(c);
        delete c;
        return nullptr;
    }
    return c;
}

static bool tls_send_all(TlsConn *c, const char *p, int n)
{
    const int MAX_MSG = (int)c->sizes.cbMaximumMessage;
    while (n > 0)
    {
        int chunk = std::min(n, MAX_MSG);
        std::vector<uint8_t> msg(c->sizes.cbHeader + (size_t)chunk + c->sizes.cbTrailer);

        SecBuffer bufs[3] = {
            {c->sizes.cbHeader, SECBUFFER_STREAM_HEADER, msg.data()},
            {(ULONG)chunk, SECBUFFER_DATA, msg.data() + c->sizes.cbHeader},
            {c->sizes.cbTrailer, SECBUFFER_STREAM_TRAILER,
             msg.data() + c->sizes.cbHeader + chunk}};
        SecBufferDesc desc = {SECBUFFER_VERSION, 3, bufs};
        memcpy(bufs[1].pvBuffer, p, (size_t)chunk);

        SECURITY_STATUS ss = EncryptMessage(&c->ctx, 0, &desc, 0);
        if (ss != SEC_E_OK)
            return false;

        int total = (int)(bufs[0].cbBuffer + bufs[1].cbBuffer + bufs[2].cbBuffer);
        if (!send_all_raw(c->sock, (const char *)msg.data(), total))
            return false;

        p += chunk;
        n -= chunk;
    }
    return true;
}

static int tls_recv_some(TlsConn *c, char *buf, int want)
{
    if (!c->plain.empty())
    {
        int n = (int)std::min((size_t)want, c->plain.size());
        memcpy(buf, c->plain.data(), (size_t)n);
        c->plain.erase(c->plain.begin(), c->plain.begin() + n);
        return n;
    }

    char tmp[16384];
    for (;;)
    {
        while (!c->raw.empty())
        {
            SecBuffer in_bufs[4] = {
                {(ULONG)c->raw.size(), SECBUFFER_DATA, c->raw.data()},
                {0, SECBUFFER_EMPTY, NULL},
                {0, SECBUFFER_EMPTY, NULL},
                {0, SECBUFFER_EMPTY, NULL}};
            SecBufferDesc in_desc = {SECBUFFER_VERSION, 4, in_bufs};

            SECURITY_STATUS ss = DecryptMessage(&c->ctx, &in_desc, 0, NULL);

            if (ss == SEC_E_INCOMPLETE_MESSAGE)
                break;
            if (ss == SEC_I_CONTEXT_EXPIRED)
                return 0;
            if (ss != SEC_E_OK && ss != SEC_I_RENEGOTIATE)
                return -1;

            for (int i = 0; i < 4; ++i)
            {
                if (in_bufs[i].BufferType == SECBUFFER_DATA && in_bufs[i].cbBuffer > 0)
                {
                    auto *ptr = (uint8_t *)in_bufs[i].pvBuffer;
                    c->plain.insert(c->plain.end(), ptr, ptr + in_bufs[i].cbBuffer);
                }
            }
            bool has_extra = false;
            for (int i = 1; i < 4; ++i)
            {
                if (in_bufs[i].BufferType == SECBUFFER_EXTRA && in_bufs[i].cbBuffer > 0)
                {
                    size_t off = c->raw.size() - in_bufs[i].cbBuffer;
                    std::vector<uint8_t> extra(
                        c->raw.begin() + (ptrdiff_t)off, c->raw.end());
                    c->raw = std::move(extra);
                    has_extra = true;
                    break;
                }
            }
            if (!has_extra)
                c->raw.clear();

            if (!c->plain.empty())
            {
                int n = (int)std::min((size_t)want, c->plain.size());
                memcpy(buf, c->plain.data(), (size_t)n);
                c->plain.erase(c->plain.begin(), c->plain.begin() + n);
                return n;
            }
        }
        int n = recv(c->sock, tmp, sizeof tmp, 0);
        if (n <= 0)
            return -1;
        c->raw.insert(c->raw.end(), tmp, tmp + n);
    }
}

static int tls_recv_n(TlsConn *c, char *p, int n)
{
    int got = 0;
    while (got < n)
    {
        int k = tls_recv_some(c, p + got, n - got);
        if (k <= 0)
            return got;
        got += k;
    }
    return got;
}

// ===== I/O WRAPPERS =====
static bool send_all(TlsConn *c, const char *p, int n)
{
    return tls_send_all(c, p, n);
}

static bool send_chunk(TlsConn *c, const char *p, int n)
{
    char h[32];
    int hl = std::snprintf(h, sizeof h, "%X\r\n", n);
    if (!send_all(c, h, hl))
        return false;
    if (n > 0 && !send_all(c, p, n))
        return false;
    return send_all(c, "\r\n", 2);
}

static int recv_n(TlsConn *c, char *p, int n)
{
    return tls_recv_n(c, p, n);
}

// ===== HTTPS GET =====
static std::string http_get(const std::string &host, int port,
                            const std::string &path, bool verify_cert = false)
{
    TlsConn *c = tls_connect(host, port, verify_cert);
    if (!c)
        return {};
    std::ostringstream r;
    r << "GET " << path << " HTTP/1.1\r\nHost: " << host << ":" << port
      << "\r\nConnection: close\r\nAccept: text/plain\r\n\r\n";
    std::string req = r.str();
    if (!send_all(c, req.data(), (int)req.size()))
    {
        tls_close(c);
        delete c;
        return {};
    }

    std::string all;
    char buf[4096];
    for (;;)
    {
        int n = tls_recv_some(c, buf, sizeof buf);
        if (n <= 0)
            break;
        all.append(buf, (size_t)n);
    }
    tls_close(c);
    delete c;
    auto p2 = all.find("\r\n\r\n");
    return p2 == std::string::npos ? std::string{} : all.substr(p2 + 4);
}

// ===== JSON PARSING =====
static bool json_str(const std::string &j, const std::string &k, std::string &out)
{
    std::string key = "\"" + k + "\"";
    auto p = j.find(key);
    if (p == std::string::npos)
        return false;
    p = j.find(':', p);
    if (p == std::string::npos)
        return false;
    ++p;
    while (p < j.size() && std::isspace((unsigned char)j[p]))
        ++p;
    if (p >= j.size() || j[p] != '"')
        return false;
    ++p;
    auto e = j.find('"', p);
    if (e == std::string::npos)
        return false;
    out = j.substr(p, e - p);
    return true;
}

static bool json_int(const std::string &j, const std::string &k, int &out)
{
    std::string key = "\"" + k + "\"";
    auto p = j.find(key);
    if (p == std::string::npos)
        return false;
    p = j.find(':', p);
    if (p == std::string::npos)
        return false;
    ++p;
    while (p < j.size() && std::isspace((unsigned char)j[p]))
        ++p;
    if (p >= j.size())
        return false;
    int sign = 1;
    if (j[p] == '-')
    {
        sign = -1;
        ++p;
    }
    if (p >= j.size() || !std::isdigit((unsigned char)j[p]))
        return false;
    int v = 0;
    while (p < j.size() && std::isdigit((unsigned char)j[p]))
    {
        v = v * 10 + (j[p] - '0');
        ++p;
    }
    out = sign * v;
    return true;
}

static bool json_str_ex(const std::string &j, const std::string &k, std::string &out)
{
    std::string key = "\"" + k + "\"";
    auto p = j.find(key);
    if (p == std::string::npos)
        return false;
    p = j.find(':', p);
    if (p == std::string::npos)
        return false;
    ++p;
    while (p < j.size() && std::isspace((unsigned char)j[p]))
        ++p;
    if (p >= j.size() || j[p] != '"')
        return false;
    ++p;
    out.clear();
    auto hex = [](char c, unsigned &v)
    {
        if (c >= '0' && c <= '9')
        {
            v = c - '0';
            return true;
        }
        if (c >= 'a' && c <= 'f')
        {
            v = c - 'a' + 10;
            return true;
        }
        if (c >= 'A' && c <= 'F')
        {
            v = c - 'A' + 10;
            return true;
        }
        return false;
    };
    auto emit_cp = [&](unsigned cp)
    {
        if (cp < 0x80)
            out += (char)cp;
        else if (cp < 0x800)
        {
            out += (char)(0xC0 | (cp >> 6));
            out += (char)(0x80 | (cp & 0x3F));
        }
        else if (cp < 0x10000)
        {
            out += (char)(0xE0 | (cp >> 12));
            out += (char)(0x80 | ((cp >> 6) & 0x3F));
            out += (char)(0x80 | (cp & 0x3F));
        }
        else
        {
            out += (char)(0xF0 | (cp >> 18));
            out += (char)(0x80 | ((cp >> 12) & 0x3F));
            out += (char)(0x80 | ((cp >> 6) & 0x3F));
            out += (char)(0x80 | (cp & 0x3F));
        }
    };
    while (p < j.size())
    {
        char c = j[p];
        if (c == '"')
            return true;
        if (c == '\\' && p + 1 < j.size())
        {
            char n = j[p + 1];
            if (n == '"' || n == '\\' || n == '/')
            {
                out += n;
                p += 2;
                continue;
            }
            if (n == 'n')
            {
                out += '\n';
                p += 2;
                continue;
            }
            if (n == 't')
            {
                out += '\t';
                p += 2;
                continue;
            }
            if (n == 'r')
            {
                out += '\r';
                p += 2;
                continue;
            }
            if (n == 'b')
            {
                out += '\b';
                p += 2;
                continue;
            }
            if (n == 'f')
            {
                out += '\f';
                p += 2;
                continue;
            }
            if (n == 'u' && p + 5 < j.size())
            {
                unsigned cp = 0;
                for (int i = 0; i < 4; ++i)
                {
                    unsigned v;
                    if (!hex(j[p + 2 + i], v))
                        return false;
                    cp = (cp << 4) | v;
                }
                p += 6;
                if (cp >= 0xD800 && cp <= 0xDBFF && p + 5 < j.size() && j[p] == '\\' && j[p + 1] == 'u')
                {
                    unsigned low = 0;
                    bool ok = true;
                    for (int i = 0; i < 4; ++i)
                    {
                        unsigned v;
                        if (!hex(j[p + 2 + i], v))
                        {
                            ok = false;
                            break;
                        }
                        low = (low << 4) | v;
                    }
                    if (ok && low >= 0xDC00 && low <= 0xDFFF)
                    {
                        cp = 0x10000 + ((cp - 0xD800) << 10) + (low - 0xDC00);
                        p += 6;
                    }
                }
                emit_cp(cp);
                continue;
            }
            return false;
        }
        out += c;
        ++p;
    }
    return false;
}

static std::string json_escape(const std::string &s)
{
    std::string out;
    out.reserve(s.size() + 2);
    out += '"';
    for (size_t i = 0; i < s.size(); ++i)
    {
        unsigned char c = (unsigned char)s[i];
        switch (c)
        {
        case '"':
            out += "\\\"";
            break;
        case '\\':
            out += "\\\\";
            break;
        case '\n':
            out += "\\n";
            break;
        case '\r':
            out += "\\r";
            break;
        case '\t':
            out += "\\t";
            break;
        case '\b':
            out += "\\b";
            break;
        case '\f':
            out += "\\f";
            break;
        default:
            if (c < 0x20)
            {
                char buf[8];
                std::snprintf(buf, sizeof buf, "\\u%04x", c);
                out += buf;
            }
            else
                out += (char)c;
        }
    }
    out += '"';
    return out;
}

// ===== SCREEN METRICS =====
static bool read_screen_metrics(int &w, int &h, int &ox, int &oy)
{
    w = GetSystemMetrics(SM_CXVIRTUALSCREEN);
    h = GetSystemMetrics(SM_CYVIRTUALSCREEN);
    ox = GetSystemMetrics(SM_XVIRTUALSCREEN);
    oy = GetSystemMetrics(SM_YVIRTUALSCREEN);
    if (w <= 0 || h <= 0)
    {
        w = GetSystemMetrics(SM_CXSCREEN);
        h = GetSystemMetrics(SM_CYSCREEN);
        ox = 0;
        oy = 0;
    }
    return w > 0 && h > 0;
}

static void init_screen_metrics()
{
    int w, h, ox, oy;
    if (read_screen_metrics(w, h, ox, oy))
    {
        g_screen_w = w;
        g_screen_h = h;
        g_screen_origin_x = ox;
        g_screen_origin_y = oy;
    }
    logf("screen %dx%d origin=(%d,%d)",
         g_screen_w.load(), g_screen_h.load(),
         g_screen_origin_x.load(), g_screen_origin_y.load());
}

// ===== MOUSE INPUT =====
static void do_mouse_move(int x, int y)
{
    int sw = g_screen_w.load(), sh = g_screen_h.load();
    if (sw <= 1 || sh <= 1)
        return;
    if (x < 0)
        x = 0;
    if (y < 0)
        y = 0;
    if (x >= sw)
        x = sw - 1;
    if (y >= sh)
        y = sh - 1;
    INPUT in{};
    in.type = INPUT_MOUSE;
    in.mi.dx = (LONG)((int64_t)x * 65535 / (sw - 1));
    in.mi.dy = (LONG)((int64_t)y * 65535 / (sh - 1));
    in.mi.dwFlags = MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_VIRTUALDESK;
    SendInput(1, &in, sizeof(INPUT));
}

static void do_mouse_button(int button, bool down)
{
    INPUT in{};
    in.type = INPUT_MOUSE;
    DWORD f = 0;
    switch (button)
    {
    case 0:
        f = down ? MOUSEEVENTF_LEFTDOWN : MOUSEEVENTF_LEFTUP;
        break;
    case 1:
        f = down ? MOUSEEVENTF_MIDDLEDOWN : MOUSEEVENTF_MIDDLEUP;
        break;
    case 2:
        f = down ? MOUSEEVENTF_RIGHTDOWN : MOUSEEVENTF_RIGHTUP;
        break;
    default:
        return;
    }
    in.mi.dwFlags = f;
    SendInput(1, &in, sizeof(INPUT));
}

static void do_mouse_wheel(int delta)
{
    INPUT in{};
    in.type = INPUT_MOUSE;
    in.mi.mouseData = (DWORD)delta;
    in.mi.dwFlags = MOUSEEVENTF_WHEEL;
    SendInput(1, &in, sizeof(INPUT));
}

// ===== KEYBOARD INPUT =====
static void do_text_input(const std::string &utf8)
{
    if (utf8.empty())
        return;
    int wlen = MultiByteToWideChar(CP_UTF8, 0, utf8.c_str(), (int)utf8.size(), NULL, 0);
    if (wlen <= 0)
        return;
    std::vector<wchar_t> w((size_t)wlen);
    MultiByteToWideChar(CP_UTF8, 0, utf8.c_str(), (int)utf8.size(), w.data(), wlen);
    std::vector<INPUT> inputs;
    inputs.reserve(w.size() * 2);
    auto push_vk = [&](WORD vk)
    {
        INPUT d{};
        d.type = INPUT_KEYBOARD;
        d.ki.wVk = vk;
        d.ki.wScan = (WORD)MapVirtualKeyW(vk, MAPVK_VK_TO_VSC);
        d.ki.dwFlags = 0;
        INPUT u = d;
        u.ki.dwFlags = KEYEVENTF_KEYUP;
        inputs.push_back(d);
        inputs.push_back(u);
    };
    for (wchar_t ch : w)
    {
        if (ch == L'\r')
            continue;
        if (ch == L'\n')
        {
            push_vk(VK_RETURN);
            continue;
        }
        if (ch == L'\t')
        {
            push_vk(VK_TAB);
            continue;
        }
        INPUT d{};
        d.type = INPUT_KEYBOARD;
        d.ki.wVk = 0;
        d.ki.wScan = (WORD)ch;
        d.ki.dwFlags = KEYEVENTF_UNICODE;
        if ((ch & 0xFF00) == 0xE000)
            d.ki.dwFlags |= KEYEVENTF_EXTENDEDKEY;
        INPUT u = d;
        u.ki.dwFlags |= KEYEVENTF_KEYUP;
        inputs.push_back(d);
        inputs.push_back(u);
    }
    if (inputs.empty())
        return;
    const size_t BATCH = 128;
    for (size_t i = 0; i < inputs.size(); i += BATCH)
    {
        UINT n = (UINT)std::min(BATCH, inputs.size() - i);
        SendInput(n, inputs.data() + i, sizeof(INPUT));
    }
}

static int code_to_vk(const std::string &code)
{
    if (code.size() == 4 && code.compare(0, 3, "Key") == 0)
    {
        char c = code[3];
        if (c >= 'A' && c <= 'Z')
            return c;
    }
    if (code.size() == 6 && code.compare(0, 5, "Digit") == 0)
    {
        char c = code[5];
        if (c >= '0' && c <= '9')
            return c;
    }
    if (code.compare(0, 6, "Numpad") == 0)
    {
        if (code.size() == 7)
        {
            char c = code[6];
            if (c >= '0' && c <= '9')
                return VK_NUMPAD0 + (c - '0');
        }
        if (code == "NumpadAdd")
            return VK_ADD;
        if (code == "NumpadSubtract")
            return VK_SUBTRACT;
        if (code == "NumpadMultiply")
            return VK_MULTIPLY;
        if (code == "NumpadDivide")
            return VK_DIVIDE;
        if (code == "NumpadDecimal")
            return VK_DECIMAL;
        if (code == "NumpadEnter")
            return VK_RETURN;
    }
    if (!code.empty() && code[0] == 'F' && code.size() >= 2 && code.size() <= 3)
    {
        bool digits = true;
        for (size_t i = 1; i < code.size(); ++i)
            if (!std::isdigit((unsigned char)code[i]))
            {
                digits = false;
                break;
            }
        if (digits)
        {
            int n = std::atoi(code.c_str() + 1);
            if (n >= 1 && n <= 24)
                return VK_F1 + (n - 1);
        }
    }
    static const std::unordered_map<std::string, int> m = {
        {"Enter", VK_RETURN},
        {"Backspace", VK_BACK},
        {"Tab", VK_TAB},
        {"Space", VK_SPACE},
        {"Escape", VK_ESCAPE},
        {"ArrowLeft", VK_LEFT},
        {"ArrowRight", VK_RIGHT},
        {"ArrowUp", VK_UP},
        {"ArrowDown", VK_DOWN},
        {"Home", VK_HOME},
        {"End", VK_END},
        {"PageUp", VK_PRIOR},
        {"PageDown", VK_NEXT},
        {"Insert", VK_INSERT},
        {"Delete", VK_DELETE},
        {"ShiftLeft", VK_LSHIFT},
        {"ShiftRight", VK_RSHIFT},
        {"ControlLeft", VK_LCONTROL},
        {"ControlRight", VK_RCONTROL},
        {"AltLeft", VK_LMENU},
        {"AltRight", VK_RMENU},
        {"MetaLeft", VK_LWIN},
        {"MetaRight", VK_RWIN},
        {"OSLeft", VK_LWIN},
        {"OSRight", VK_RWIN},
        {"CapsLock", VK_CAPITAL},
        {"NumLock", VK_NUMLOCK},
        {"ScrollLock", VK_SCROLL},
        {"PrintScreen", VK_SNAPSHOT},
        {"Pause", VK_PAUSE},
        {"ContextMenu", VK_APPS},
        {"Minus", VK_OEM_MINUS},
        {"Equal", VK_OEM_PLUS},
        {"BracketLeft", VK_OEM_4},
        {"BracketRight", VK_OEM_6},
        {"Backslash", VK_OEM_5},
        {"Semicolon", VK_OEM_1},
        {"Quote", VK_OEM_7},
        {"Comma", VK_OEM_COMMA},
        {"Period", VK_OEM_PERIOD},
        {"Slash", VK_OEM_2},
        {"Backquote", VK_OEM_3},
        {"IntlBackslash", VK_OEM_102},
    };
    auto it = m.find(code);
    return it == m.end() ? 0 : it->second;
}

static void do_key(const std::string &code, bool down)
{
    int vk = code_to_vk(code);
    if (vk == 0)
        return;
    INPUT in{};
    in.type = INPUT_KEYBOARD;
    in.ki.wVk = (WORD)vk;
    in.ki.wScan = (WORD)MapVirtualKeyW(vk, MAPVK_VK_TO_VSC);
    in.ki.dwFlags = down ? 0 : KEYEVENTF_KEYUP;
    switch (vk)
    {
    case VK_RMENU:
    case VK_RCONTROL:
    case VK_LEFT:
    case VK_RIGHT:
    case VK_UP:
    case VK_DOWN:
    case VK_PRIOR:
    case VK_NEXT:
    case VK_HOME:
    case VK_END:
    case VK_INSERT:
    case VK_DELETE:
    case VK_SNAPSHOT:
    case VK_APPS:
    case VK_LWIN:
    case VK_RWIN:
    case VK_NUMLOCK:
        in.ki.dwFlags |= KEYEVENTF_EXTENDEDKEY;
        break;
    }
    SendInput(1, &in, sizeof(INPUT));
}

// ===== CLIPBOARD =====
static std::string clipboard_read_utf8()
{
    bool opened = false;
    for (int i = 0; i < 10; ++i)
    {
        if (OpenClipboard(NULL))
        {
            opened = true;
            break;
        }
        Sleep(15);
    }
    if (!opened)
        return {};
    std::string result;
    HANDLE h = GetClipboardData(CF_UNICODETEXT);
    if (h)
    {
        const wchar_t *w = (const wchar_t *)GlobalLock(h);
        if (w)
        {
            int n = WideCharToMultiByte(CP_UTF8, 0, w, -1, NULL, 0, NULL, NULL);
            if (n > 1)
            {
                result.resize((size_t)n - 1);
                WideCharToMultiByte(CP_UTF8, 0, w, -1, &result[0], n, NULL, NULL);
            }
            GlobalUnlock(h);
        }
    }
    CloseClipboard();
    return result;
}

static void clipboard_write_utf8(const std::string &utf8)
{
    int wlen = MultiByteToWideChar(CP_UTF8, 0, utf8.c_str(), (int)utf8.size() + 1, NULL, 0);
    if (wlen <= 0)
        return;
    HGLOBAL mem = GlobalAlloc(GMEM_MOVEABLE, (size_t)wlen * sizeof(wchar_t));
    if (!mem)
        return;
    wchar_t *dst = (wchar_t *)GlobalLock(mem);
    if (!dst)
    {
        GlobalFree(mem);
        return;
    }
    MultiByteToWideChar(CP_UTF8, 0, utf8.c_str(), (int)utf8.size() + 1, dst, wlen);
    GlobalUnlock(mem);
    bool opened = false;
    for (int i = 0; i < 10; ++i)
    {
        if (OpenClipboard(NULL))
        {
            opened = true;
            break;
        }
        Sleep(15);
    }
    if (!opened)
    {
        GlobalFree(mem);
        return;
    }
    EmptyClipboard();
    if (!SetClipboardData(CF_UNICODETEXT, mem))
        GlobalFree(mem);
    CloseClipboard();
    std::lock_guard<std::mutex> lk(g_clip_m);
    g_last_clip = utf8;
}

// ===== WEBSOCKET =====
static std::string b64(const unsigned char *d, size_t n)
{
    static const char *T = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string o;
    size_t i = 0;
    while (i < n)
    {
        uint32_t v = 0;
        int k = (int)std::min<size_t>(3, n - i);
        for (int j = 0; j < k; ++j)
            v |= d[i + j] << ((2 - j) * 8);
        for (int j = 0; j < 4; ++j)
            o += (j <= k) ? T[(v >> ((3 - j) * 6)) & 63] : '=';
        i += 3;
    }
    return o;
}

static bool ws_handshake(TlsConn *c, const std::string &host, int port,
                         const std::string &path)
{
    unsigned char k[16];
    std::random_device rd;
    for (int i = 0; i < 16; ++i)
        k[i] = (unsigned char)(rd() & 0xFF);
    std::ostringstream r;
    r << "GET " << path << " HTTP/1.1\r\n"
      << "Host: " << host << ":" << port << "\r\n"
      << "Upgrade: websocket\r\nConnection: Upgrade\r\n"
      << "Sec-WebSocket-Key: " << b64(k, 16) << "\r\n"
      << "Sec-WebSocket-Version: 13\r\n\r\n";
    std::string rs = r.str();
    if (!send_all(c, rs.data(), (int)rs.size()))
        return false;
    std::string h;
    char ch;
    while (h.size() < 8192)
    {
        if (recv_n(c, &ch, 1) != 1)
            return false;
        h += ch;
        if (h.size() >= 4 && h.compare(h.size() - 4, 4, "\r\n\r\n") == 0)
            break;
    }
    return h.find(" 101") != std::string::npos;
}

static bool ws_send(TlsConn *c, int op, const void *data, size_t len)
{
    std::vector<uint8_t> f;
    f.reserve(len + 14);
    f.push_back((uint8_t)(0x80 | op));
    uint8_t mask[4];
    std::random_device rd;
    for (int i = 0; i < 4; ++i)
        mask[i] = (uint8_t)(rd() & 0xFF);
    if (len < 126)
        f.push_back((uint8_t)(0x80 | len));
    else if (len < 65536)
    {
        f.push_back((uint8_t)(0x80 | 126));
        f.push_back((uint8_t)((len >> 8) & 0xFF));
        f.push_back((uint8_t)(len & 0xFF));
    }
    else
    {
        f.push_back((uint8_t)(0x80 | 127));
        for (int i = 7; i >= 0; --i)
            f.push_back((uint8_t)((len >> (i * 8)) & 0xFF));
    }
    for (int i = 0; i < 4; ++i)
        f.push_back(mask[i]);
    const uint8_t *p = (const uint8_t *)data;
    for (size_t i = 0; i < len; ++i)
        f.push_back(p[i] ^ mask[i & 3]);
    return send_all(c, (const char *)f.data(), (int)f.size());
}

static int ws_recv(TlsConn *c, std::vector<uint8_t> &payload)
{
    uint8_t h[2];
    if (recv_n(c, (char *)h, 2) != 2)
        return -1;
    int op = h[0] & 0x0F;
    bool masked = (h[1] & 0x80) != 0;
    uint64_t len = h[1] & 0x7F;
    if (len == 126)
    {
        uint8_t b[2];
        if (recv_n(c, (char *)b, 2) != 2)
            return -1;
        len = ((uint64_t)b[0] << 8) | b[1];
    }
    else if (len == 127)
    {
        uint8_t b[8];
        if (recv_n(c, (char *)b, 8) != 8)
            return -1;
        len = 0;
        for (int i = 0; i < 8; ++i)
            len = (len << 8) | b[i];
    }
    uint8_t mk[4] = {0, 0, 0, 0};
    if (masked && recv_n(c, (char *)mk, 4) != 4)
        return -1;
    if (len > (8u << 20))
        return -1;
    payload.resize((size_t)len);
    if (len && recv_n(c, (char *)payload.data(), (int)len) != (int)len)
        return -1;
    if (masked)
        for (size_t i = 0; i < payload.size(); ++i)
            payload[i] ^= mk[i & 3];
    if (op == 0x8)
        return -1;
    if (op == 0x9)
    {
        ws_send(c, 0xA, payload.data(), payload.size());
        return 0;
    }
    if (op == 0xA)
        return 0;
    if (op == 0x1)
        return 1;
    if (op == 0x2)
        return 2;
    return 0;
}

// ===== CONTROL HANDLERS =====
static void handle_control(const std::string &j)
{
    std::string type;
    if (!json_str(j, "type", type))
        return;
    if (type == "mouse_move")
    {
        int x = 0, y = 0;
        if (json_int(j, "x", x) && json_int(j, "y", y))
            do_mouse_move(x, y);
    }
    else if (type == "mouse_down" || type == "mouse_up")
    {
        int btn = 0;
        json_int(j, "button", btn);
        do_mouse_button(btn, type == "mouse_down");
    }
    else if (type == "mouse_wheel")
    {
        int d = 0;
        if (json_int(j, "delta", d))
            do_mouse_wheel(d);
    }
    else if (type == "text")
    {
        std::string text;
        if (json_str_ex(j, "text", text))
            do_text_input(text);
    }
    else if (type == "key_down" || type == "key_up")
    {
        std::string code;
        if (json_str(j, "code", code))
            do_key(code, type == "key_down");
    }
    else if (type == "clipboard")
    {
        std::string text;
        if (json_str_ex(j, "text", text))
            clipboard_write_utf8(text);
    }
}

// ===== HELLO MESSAGE =====
static std::string make_hello_json()
{
    std::ostringstream hs;
    hs << "{\"type\":\"hello\""
       << ",\"screen_w\":" << g_screen_w.load()
       << ",\"screen_h\":" << g_screen_h.load() << "}";
    return hs.str();
}

// ===== FFMPEG =====
struct VideoConfig
{
    std::mutex m;
    std::string codec = "mjpeg";
    std::string encoder = "cpu";
    std::string bitrate = "4M";
    int framerate = 30;
    int mjpeg_q = 4;
    std::atomic<bool> restart{false};
    std::atomic<bool> stop{false};
};

static std::string build_ffmpeg_cmd(const VideoConfig &cfg, const std::string &ffmpeg_path = "ffmpeg.exe")
{
    std::ostringstream c;
    c << "\"" << ffmpeg_path << "\" -hide_banner -loglevel warning"
      << " -f gdigrab -framerate " << cfg.framerate << " -draw_mouse 1 -i desktop";
    if (cfg.codec == "mjpeg")
    {
        c << " -f mjpeg -q:v " << cfg.mjpeg_q << " -pix_fmt yuvj420p pipe:1";
    }
    else
    {
        const std::string &enc = cfg.encoder;
        if (enc == "amf")
        {
            int gop = cfg.framerate * 2;
            c << " -c:v h264_amf -usage lowlatency -quality balanced -rc vbr_latency"
              << " -b:v " << cfg.bitrate << " -maxrate " << cfg.bitrate
              << " -g " << gop << " -bf 0 -vbaq true -preanalysis true -enforce_hrd true";
        }
        else if (enc == "qsv")
        {
            c << " -c:v h264_qsv -preset veryfast -look_ahead 0"
              << " -b:v " << cfg.bitrate << " -maxrate " << cfg.bitrate
              << " -g " << cfg.framerate << " -bf 0";
        }
        else if (enc == "nvenc")
        {
            c << " -c:v h264_nvenc -preset p1 -tune ull -rc cbr"
              << " -b:v " << cfg.bitrate
              << " -g " << cfg.framerate << " -bf 0";
        }
        else
        {
            int gop = cfg.framerate * 2;
            c << " -c:v libx264 -preset veryfast -tune zerolatency -profile:v main -pix_fmt yuv420p"
              << " -bf 0 -refs 1 -b:v " << cfg.bitrate << " -maxrate " << cfg.bitrate
              << " -bufsize " << cfg.bitrate << " -g " << gop << " -keyint_min " << cfg.framerate
              << " -x264-params \"nal-hrd=cbr:force-cfr=1:aud=1:scenecut=0:rc-lookahead=0:sync-lookahead=0:aq-mode=1\"";
        }
        if (enc != "cpu")
            c << " -bsf:v h264_metadata=aud=insert";
        c << " -f h264 -flush_packets 1 pipe:1";
    }
    return c.str();
}

static HANDLE start_ffmpeg(const std::string &cmdline, PROCESS_INFORMATION &pi)
{
    SECURITY_ATTRIBUTES sa{sizeof(sa), NULL, TRUE};
    HANDLE rd = NULL, wr = NULL;
    if (!CreatePipe(&rd, &wr, &sa, 4 * 1024 * 1024))
    {
        log("CreatePipe failed");
        return NULL;
    }
    SetHandleInformation(rd, HANDLE_FLAG_INHERIT, 0);
    STARTUPINFOA si{};
    si.cb = sizeof(si);
    si.dwFlags = STARTF_USESTDHANDLES;
    si.hStdOutput = wr;
    si.hStdError = GetStdHandle(STD_ERROR_HANDLE);
    si.hStdInput = GetStdHandle(STD_INPUT_HANDLE);
    std::vector<char> buf(cmdline.begin(), cmdline.end());
    buf.push_back(0);
    logf("launching ffmpeg: %s", cmdline.c_str());
    if (!CreateProcessA(NULL, buf.data(), NULL, NULL, TRUE, 0, NULL, NULL, &si, &pi))
    {
        logf("CreateProcess failed, err=%lu", GetLastError());
        CloseHandle(rd);
        CloseHandle(wr);
        return NULL;
    }
    CloseHandle(wr);
    return rd;
}

// ===== STREAMING STATE =====
struct StreamingState
{
    std::mutex m;
    std::string agent_id;
    std::string server_host;
    int server_port = 443;
    VideoConfig video_cfg;
    TlsConn *ctrl_conn = nullptr;
    std::atomic<bool> stop{false};
};

// ===== THREAD LOOPS =====
static void control_loop(StreamingState &st)
{
    while (!st.stop)
    {
        TlsConn *c = tls_connect(st.server_host, st.server_port, false);
        if (!c)
        {
            std::this_thread::sleep_for(std::chrono::seconds(3));
            continue;
        }
        std::string path = "/ws/control/agent/" + st.agent_id;
        if (!ws_handshake(c, st.server_host, st.server_port, path))
        {
            logf("control handshake failed for %s", st.agent_id.c_str());
            tls_close(c);
            delete c;
            std::this_thread::sleep_for(std::chrono::seconds(3));
            continue;
        }
        logf("control connected for %s", st.agent_id.c_str());
        {
            std::lock_guard<std::mutex> lk(st.m);
            st.ctrl_conn = c;
        }
        std::string hello = make_hello_json();
        ws_send(c, 0x1, hello.data(), hello.size());
        std::vector<uint8_t> buf;
        while (!st.stop)
        {
            int r = ws_recv(c, buf);
            if (r < 0)
                break;
            if (r == 1)
            {
                std::string msg(buf.begin(), buf.end());
                handle_control(msg);
            }
        }
        {
            std::lock_guard<std::mutex> lk(st.m);
            st.ctrl_conn = nullptr;
        }
        tls_close(c);
        delete c;
        logf("control disconnected, retry in 2s");
        std::this_thread::sleep_for(std::chrono::seconds(2));
    }
}

static void resolution_watch_loop(StreamingState &st)
{
    using namespace std::chrono;
    while (!st.stop)
    {
        std::this_thread::sleep_for(seconds(2));
        if (st.stop)
            break;
        int w, h, ox, oy;
        if (!read_screen_metrics(w, h, ox, oy))
            continue;
        if (w == g_screen_w.load() && h == g_screen_h.load() &&
            ox == g_screen_origin_x.load() && oy == g_screen_origin_y.load())
            continue;
        logf("resolution changed: %dx%d -> %dx%d",
             g_screen_w.load(), g_screen_h.load(), w, h);
        g_screen_w = w;
        g_screen_h = h;
        g_screen_origin_x = ox;
        g_screen_origin_y = oy;
        {
            std::lock_guard<std::mutex> lk(st.video_cfg.m);
            st.video_cfg.restart = true;
        }
        if (st.ctrl_conn)
        {
            std::string hello = make_hello_json();
            ws_send(st.ctrl_conn, 0x1, hello.data(), hello.size());
        }
    }
}

static void clipboard_watch_loop(StreamingState &st)
{
    {
        std::string cur = clipboard_read_utf8();
        std::lock_guard<std::mutex> lk(g_clip_m);
        g_last_clip = cur;
    }
    while (!st.stop)
    {
        std::this_thread::sleep_for(std::chrono::milliseconds(500));
        if (st.stop)
            break;
        std::string cur = clipboard_read_utf8();
        if (cur.empty())
            continue;
        bool changed = false;
        {
            std::lock_guard<std::mutex> lk(g_clip_m);
            if (cur != g_last_clip)
            {
                g_last_clip = cur;
                changed = true;
            }
        }
        if (!changed)
            continue;
        if (cur.size() > 512 * 1024)
            continue;
        if (st.ctrl_conn)
        {
            std::string msg = std::string("{\"type\":\"clipboard\",\"text\":") + json_escape(cur) + "}";
            ws_send(st.ctrl_conn, 0x1, msg.data(), msg.size());
        }
    }
}

static void poll_config_loop(StreamingState &st)
{
    std::string last_sig;
    while (!st.stop)
    {
        std::this_thread::sleep_for(std::chrono::milliseconds(2000));
        std::string body = http_get(st.server_host, st.server_port,
                                    "/agents/" + st.agent_id + "/config");
        if (body.empty())
            continue;
        std::string codec, encoder, bitrate;
        int fps = 0, mq = 0;
        std::istringstream iss(body);
        std::string line;
        while (std::getline(iss, line))
        {
            if (!line.empty() && line.back() == '\r')
                line.pop_back();
            auto eq = line.find('=');
            if (eq == std::string::npos)
                continue;
            std::string k = line.substr(0, eq), v = line.substr(eq + 1);
            if (k == "codec")
                codec = v;
            else if (k == "encoder")
                encoder = v;
            else if (k == "bitrate")
                bitrate = v;
            else if (k == "fps")
                fps = std::atoi(v.c_str());
            else if (k == "mjpeg_q")
                mq = std::atoi(v.c_str());
        }
        if (codec.empty())
            continue;
        std::string sig = codec + "|" + encoder + "|" + bitrate + "|" +
                          std::to_string(fps) + "|" + std::to_string(mq);
        if (sig == last_sig)
            continue;
        last_sig = sig;
        std::lock_guard<std::mutex> lk(st.video_cfg.m);
        st.video_cfg.codec = codec;
        st.video_cfg.encoder = encoder.empty() ? "cpu" : encoder;
        st.video_cfg.bitrate = bitrate.empty() ? "4M" : bitrate;
        if (fps > 0)
            st.video_cfg.framerate = fps;
        if (mq > 0)
            st.video_cfg.mjpeg_q = mq;
        st.video_cfg.restart = true;
        logf("config updated: codec=%s encoder=%s bitrate=%s fps=%d",
             st.video_cfg.codec.c_str(), st.video_cfg.encoder.c_str(),
             st.video_cfg.bitrate.c_str(), st.video_cfg.framerate);
    }
}

static void run_session(StreamingState &st)
{
    TlsConn *c = tls_connect(st.server_host, st.server_port, false);
    if (!c)
    {
        log("tls_connect failed for streaming");
        return;
    }

    std::string codec, encoder, bitrate;
    int fps = 30, mq = 4;
    {
        std::lock_guard<std::mutex> lk(st.video_cfg.m);
        codec = st.video_cfg.codec;
        encoder = st.video_cfg.encoder;
        bitrate = st.video_cfg.bitrate;
        fps = st.video_cfg.framerate;
        mq = st.video_cfg.mjpeg_q;
    }
    st.video_cfg.restart = false;

    std::string ctype = (codec == "mjpeg") ? "video/x-motion-jpeg" : "video/h264";
    std::ostringstream req;
    req << "POST /ingest/" << st.agent_id << " HTTP/1.1\r\n"
        << "Host: " << st.server_host << ":" << st.server_port << "\r\n"
        << "Content-Type: " << ctype << "\r\n"
        << "X-Agent-Encoder: " << encoder << "\r\n"
        << "X-Agent-Bitrate: " << bitrate << "\r\n"
        << "X-Agent-FPS: " << fps << "\r\n"
        << "Transfer-Encoding: chunked\r\n"
        << "Connection: close\r\n\r\n";
    std::string s = req.str();
    if (!send_all(c, s.data(), (int)s.size()))
    {
        tls_close(c);
        delete c;
        return;
    }

    VideoConfig snap;
    {
        std::lock_guard<std::mutex> lk(st.video_cfg.m);
        snap.codec = st.video_cfg.codec;
        snap.encoder = st.video_cfg.encoder;
        snap.bitrate = st.video_cfg.bitrate;
        snap.framerate = st.video_cfg.framerate;
        snap.mjpeg_q = st.video_cfg.mjpeg_q;
    }
    std::string cmd = build_ffmpeg_cmd(snap);

    PROCESS_INFORMATION pi{};
    HANDLE pipe = start_ffmpeg(cmd, pi);
    if (!pipe)
    {
        tls_close(c);
        delete c;
        return;
    }

    std::vector<char> buf(64 * 1024);
    auto t0 = std::chrono::steady_clock::now();
    uint64_t bytes = 0;
    while (!st.stop)
    {
        if (st.video_cfg.restart)
        {
            logf("restart requested for %s", st.agent_id.c_str());
            break;
        }
        DWORD n = 0;
        if (!ReadFile(pipe, buf.data(), (DWORD)buf.size(), &n, NULL) || n == 0)
        {
            log("ffmpeg pipe closed");
            break;
        }
        if (!send_chunk(c, buf.data(), (int)n))
        {
            log("send failed");
            break;
        }
        bytes += n;
        auto now = std::chrono::steady_clock::now();
        auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - t0).count();
        if (ms >= 5000)
        {
            double kbps = (bytes * 8.0) / ms;
            logf("[%s/%s] %d kbit/s", codec.c_str(), encoder.c_str(), (int)kbps);
            t0 = now;
            bytes = 0;
        }
    }
    send_all(c, "0\r\n\r\n", 5);
    TerminateProcess(pi.hProcess, 0);
    CloseHandle(pi.hProcess);
    CloseHandle(pi.hThread);
    CloseHandle(pipe);
    tls_close(c);
    delete c;
}

// ===== SYSTEM INFO GATHERING (sysdmnew) =====
static std::string get_machine_uid()
{
    HKEY hkey;
    if (RegOpenKeyExA(HKEY_LOCAL_MACHINE,
                      "SOFTWARE\\Microsoft\\Cryptography", 0, KEY_READ, &hkey) != ERROR_SUCCESS)
        return "unknown";

    char val[256] = {0};
    DWORD sz = sizeof val;
    if (RegQueryValueExA(hkey, "MachineGuid", NULL, NULL, (BYTE *)val, &sz) != ERROR_SUCCESS)
    {
        RegCloseKey(hkey);
        return "unknown";
    }
    RegCloseKey(hkey);
    return std::string(val);
}

static std::string get_computer_name()
{
    char name[MAX_COMPUTERNAME_LENGTH + 1];
    DWORD sz = sizeof name;
    if (GetComputerNameA(name, &sz))
        return std::string(name);
    return "UNKNOWN";
}

static std::string get_os_version()
{
    OSVERSIONINFOA vi{};
    vi.dwOSVersionInfoSize = sizeof(vi);
    if (GetVersionExA(&vi))
    {
        char buf[128];
        std::snprintf(buf, sizeof buf, "Windows %ld.%ld", vi.dwMajorVersion, vi.dwMinorVersion);
        return std::string(buf);
    }
    return "Windows";
}

static std::string get_username()
{
    char name[256];
    DWORD sz = sizeof name;
    if (GetUserNameA(name, &sz))
        return std::string(name);
    return "";
}

static std::string get_local_ip()
{
    SOCKET s = socket(AF_INET, SOCK_DGRAM, 0);
    if (s == INVALID_SOCKET)
        return "";

    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_port = htons(53);
    addr.sin_addr.s_addr = inet_addr("8.8.8.8");

    if (connect(s, (struct sockaddr *)&addr, sizeof addr) == SOCKET_ERROR)
    {
        closesocket(s);
        return "";
    }

    sockaddr_in local;
    int len = sizeof local;
    if (getsockname(s, (sockaddr *)&local, &len) == SOCKET_ERROR)
    {
        closesocket(s);
        return "";
    }

    closesocket(s);
    return std::string(inet_ntoa(local.sin_addr));
}

static void get_memory_info(uint64_t &total, uint64_t &available)
{
    MEMORYSTATUSEX ms;
    ms.dwLength = sizeof(ms);
    if (GlobalMemoryStatusEx(&ms))
    {
        total = ms.ullTotalPhys;
        available = ms.ullAvailPhys;
    }
    else
    {
        total = available = 0;
    }
}

// ===== REGISTRATION & UPDATE (sysdmnew) =====
static bool postJSON(const std::string &url_str, const std::string &json_body, std::string &response)
{
    // Parse URL
    std::string host, path;
    int port = 443;

    size_t proto_end = url_str.find("://");
    if (proto_end == std::string::npos)
        return false;

    size_t host_start = proto_end + 3;
    size_t path_start = url_str.find('/', host_start);
    if (path_start == std::string::npos)
        path_start = url_str.size();

    std::string hostport = url_str.substr(host_start, path_start - host_start);
    size_t colon = hostport.find(':');
    if (colon != std::string::npos)
    {
        host = hostport.substr(0, colon);
        port = std::atoi(hostport.c_str() + colon + 1);
    }
    else
    {
        host = hostport;
    }
    path = (path_start < url_str.size()) ? url_str.substr(path_start) : "/";

    TlsConn *c = tls_connect(host, port, false);
    if (!c)
        return false;

    std::ostringstream req;
    req << "POST " << path << " HTTP/1.1\r\n"
        << "Host: " << host << ":" << port << "\r\n"
        << "Content-Type: application/json\r\n"
        << "Content-Length: " << json_body.size() << "\r\n"
        << "Connection: close\r\n\r\n"
        << json_body;
    std::string req_str = req.str();
    if (!send_all(c, req_str.data(), (int)req_str.size()))
    {
        tls_close(c);
        delete c;
        return false;
    }

    std::string all;
    char buf[4096];
    for (;;)
    {
        int n = tls_recv_some(c, buf, sizeof buf);
        if (n <= 0)
            break;
        all.append(buf, (size_t)n);
    }
    tls_close(c);
    delete c;

    auto p2 = all.find("\r\n\r\n");
    if (p2 == std::string::npos)
        return false;
    response = all.substr(p2 + 4);
    return true;
}

static std::string sha256_file(const std::string &path)
{
    HANDLE hFile = CreateFileA(path.c_str(), GENERIC_READ, FILE_SHARE_READ,
                               NULL, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
    if (hFile == INVALID_HANDLE_VALUE)
        return "";

    HCRYPTPROV hProv = 0;
    if (!CryptAcquireContextA(&hProv, NULL, NULL, PROV_RSA_AES, CRYPT_VERIFYCONTEXT))
    {
        CloseHandle(hFile);
        return "";
    }

    HCRYPTHASH hHash = 0;
    if (!CryptCreateHash(hProv, CALG_SHA_256, 0, 0, &hHash))
    {
        CryptReleaseContext(hProv, 0);
        CloseHandle(hFile);
        return "";
    }

    char buf[65536];
    DWORD n;
    while (ReadFile(hFile, buf, sizeof buf, &n, NULL) && n > 0)
    {
        CryptHashData(hHash, (BYTE *)buf, n, 0);
    }

    DWORD cbHash = 32;
    BYTE hash[32];
    CryptGetHashParam(hHash, HP_HASHVAL, hash, &cbHash, 0);

    std::ostringstream oss;
    for (DWORD i = 0; i < cbHash; ++i)
    {
        char hex[3];
        snprintf(hex, sizeof hex, "%02x", hash[i]);
        oss << hex;
    }

    CryptDestroyHash(hHash);
    CryptReleaseContext(hProv, 0);
    CloseHandle(hFile);
    return oss.str();
}

static void checkForUpdate(const std::string &uuid, const std::string &token)
{
    std::string server_url = SERVER_URL;
    std::ostringstream check_url_ss;
    check_url_ss << server_url << "/api/agent/check-update?uuid=" << uuid << "&token=" << token;
    std::string check_url = check_url_ss.str();

    std::ostringstream req_body;
    req_body << "{\"build\":\"" << BUILD_SLUG << "\"}";
    std::string body = req_body.str();

    std::string resp;
    if (!postJSON(check_url, body, resp))
    {
        log("check-update request failed");
        return;
    }

    bool has_update = false;
    std::string new_build, new_url, new_sha;
    json_str(resp, "build", new_build);
    json_str(resp, "url", new_url);
    json_str(resp, "sha256", new_sha);

    // Check if "update" field is true
    auto up_pos = resp.find("\"update\":");
    if (up_pos != std::string::npos)
    {
        auto true_pos = resp.find("true", up_pos);
        has_update = (true_pos != std::string::npos && true_pos < up_pos + 20);
    }

    if (!has_update || new_url.empty() || new_sha.empty())
    {
        log("no update available");
        return;
    }

    logf("update available: build=%s, url: %s", new_build.c_str(), new_url.c_str());

    char tmp_path[MAX_PATH];
    GetTempPathA(sizeof tmp_path, tmp_path);
    std::string tmp_exe = std::string(tmp_path) + "agent_tmp.exe";

    // Download
    if (new_url.find("http") == 0)
    {
        log("TODO: implement download from URL");
        return;
    }

    std::string actual_sha = sha256_file(tmp_exe);
    if (actual_sha != new_sha)
    {
        logf("SHA256 mismatch: expected %s, got %s", new_sha.c_str(), actual_sha.c_str());
        return;
    }

    char exe_path[MAX_PATH];
    GetModuleFileNameA(NULL, exe_path, sizeof exe_path);
    std::string old_exe = std::string(exe_path) + ".old";

    if (MoveFileA(exe_path, old_exe.c_str()))
    {
        if (MoveFileA(tmp_exe.c_str(), exe_path))
        {
            log("agent updated, scheduling restart");
            std::ostringstream cmd;
            cmd << "timeout /t 1 /nobreak && sc start SystemMonitoringAgent";
            system(cmd.str().c_str());
        }
        else
        {
            MoveFileA(old_exe.c_str(), exe_path);
        }
    }
}

// ===== WINDOWS SERVICE =====
static SERVICE_STATUS g_serviceStatus = {0};
static SERVICE_STATUS_HANDLE g_serviceHandle = NULL;
static HANDLE g_stopEvent = NULL;

static void WINAPI serviceCtrlHandler(DWORD dwCtrl)
{
    switch (dwCtrl)
    {
    case SERVICE_CONTROL_STOP:
        g_stopRequested = true;
        if (g_stopEvent)
            SetEvent(g_stopEvent);
        break;
    }
}

static void WINAPI serviceMain(DWORD argc, LPSTR *argv)
{
    g_serviceHandle = RegisterServiceCtrlHandlerA("SystemMonitoringAgent", serviceCtrlHandler);
    if (!g_serviceHandle)
        return;

    g_serviceStatus.dwServiceType = SERVICE_WIN32_OWN_PROCESS;
    g_serviceStatus.dwCurrentState = SERVICE_RUNNING;
    g_serviceStatus.dwControlsAccepted = SERVICE_ACCEPT_STOP;
    SetServiceStatus(g_serviceHandle, &g_serviceStatus);

    log("service started");
    g_stopEvent = CreateEventA(NULL, TRUE, FALSE, NULL);

    // Run main agent logic
    WSADATA w;
    if (WSAStartup(MAKEWORD(2, 2), &w) == 0)
    {
        init_screen_metrics();

        // Gather system info for registration
        std::string machine_uid = get_machine_uid();
        std::string name_pc = get_computer_name();
        std::string system = get_os_version();
        std::string user_name = get_username();
        std::string ip_addr = get_local_ip();
        uint64_t total_mem, avail_mem;
        get_memory_info(total_mem, avail_mem);

        // Registration loop
        for (int i = 0; i < 20 && !g_stopRequested; ++i)
        {
            std::string reg_url = std::string(SERVER_URL) + "/api/agent/register";
            std::ostringstream reg_body;
            reg_body << "{"
                     << "\"machine_uid\":" << json_escape(machine_uid)
                     << ",\"name_pc\":" << json_escape(name_pc)
                     << ",\"exe_version\":" << json_escape(BUILD_SLUG)
                     << ",\"system\":" << json_escape(system)
                     << ",\"user_name\":" << json_escape(user_name)
                     << ",\"ip_addr\":" << json_escape(ip_addr)
                     << ",\"total_memory\":" << total_mem
                     << ",\"available_memory\":" << avail_mem
                     << ",\"disks\":[]"
                     << "}";
            std::string reg_resp;
            if (postJSON(reg_url, reg_body.str(), reg_resp))
            {
                json_str(reg_resp, "agent_uuid", g_agentUUID);
                json_str(reg_resp, "token", g_agentToken);
                if (!g_agentUUID.empty())
                {
                    logf("registered: uuid=%s", g_agentUUID.c_str());
                    break;
                }
            }
            if (!g_stopRequested)
                Sleep(10000);
        }

        if (!g_agentUUID.empty())
        {
            StreamingState st;
            st.agent_id = AGENT_ID;
            st.server_host = "dev.local"; // Parse from SERVER_URL
            st.server_port = 443;

            std::thread ctrl_thread([&]()
                                    { control_loop(st); });
            std::thread res_thread([&]()
                                   { resolution_watch_loop(st); });
            std::thread clip_thread([&]()
                                    { clipboard_watch_loop(st); });
            std::thread poll_thread([&]()
                                    { poll_config_loop(st); });
            std::thread stream_thread([&]()
                                      {
                while (!st.stop) {
                    run_session(st);
                    if (!st.stop) std::this_thread::sleep_for(std::chrono::seconds(2));
                } });

            // Heartbeat loop
            using namespace std::chrono;
            while (!g_stopRequested)
            {
                std::this_thread::sleep_for(seconds(10));
                std::ostringstream hb_url_ss;
                hb_url_ss << SERVER_URL << "/api/agent/heartbeat?uuid=" << g_agentUUID << "&token=" << g_agentToken;
                std::string hb_url = hb_url_ss.str();
                std::string hb_resp;
                if (postJSON(hb_url, "{}", hb_resp))
                {
                    json_str(hb_resp, "telemetry_mode", g_telemetryMode);
                }

                // Update check every 60s
                static auto last_check = steady_clock::now();
                if (steady_clock::now() - last_check >= minutes(1))
                {
                    checkForUpdate(g_agentUUID, g_agentToken);
                    last_check = steady_clock::now();
                }
            }

            st.stop = true;
            ctrl_thread.join();
            res_thread.join();
            clip_thread.join();
            poll_thread.join();
            stream_thread.join();
        }

        WSACleanup();
    }

    if (g_stopEvent)
        CloseHandle(g_stopEvent);
    g_serviceStatus.dwCurrentState = SERVICE_STOPPED;
    SetServiceStatus(g_serviceHandle, &g_serviceStatus);
    log("service stopped");
}

static bool isServiceInstalled()
{
    SC_HANDLE scm = OpenSCManagerA(NULL, NULL, SC_MANAGER_ENUMERATE_SERVICE);
    if (!scm)
        return false;
    SC_HANDLE svc = OpenServiceA(scm, "SystemMonitoringAgent", SERVICE_QUERY_STATUS);
    bool result = (svc != NULL);
    if (svc)
        CloseServiceHandle(svc);
    CloseServiceHandle(scm);
    return result;
}

static void installService()
{
    char exe_path[MAX_PATH];
    GetModuleFileNameA(NULL, exe_path, sizeof exe_path);

    SC_HANDLE scm = OpenSCManagerA(NULL, NULL, SC_MANAGER_CREATE_SERVICE);
    if (!scm)
    {
        log("OpenSCManager failed");
        return;
    }

    SC_HANDLE svc = CreateServiceA(
        scm,
        "SystemMonitoringAgent",
        "System Monitoring Agent",
        SERVICE_ALL_ACCESS,
        SERVICE_WIN32_OWN_PROCESS,
        SERVICE_AUTO_START,
        SERVICE_ERROR_NORMAL,
        exe_path,
        NULL, NULL, NULL, NULL, NULL);

    if (svc)
    {
        SERVICE_FAILURE_ACTIONSW sfa{};
        SC_ACTION actions[3] = {
            {SC_ACTION_RESTART, 1000},
            {SC_ACTION_RESTART, 1000},
            {SC_ACTION_NONE, 0}};
        sfa.cActions = 3;
        sfa.lpsaActions = actions;
        sfa.dwResetPeriod = 60;
        ChangeServiceConfig2A(svc, SERVICE_CONFIG_FAILURE_ACTIONS, &sfa);
        log("service installed");
        CloseServiceHandle(svc);
    }
    else
    {
        logf("CreateService failed: %lu", GetLastError());
    }

    CloseServiceHandle(scm);
}

int main(int argc, char *argv[])
{
    setupFileLogger("agent.log");
    logf("Agent version: %s", BUILD_SLUG);

    if (!isServiceInstalled())
    {
        installService();
        SC_HANDLE scm = OpenSCManagerA(NULL, NULL, SC_MANAGER_CONNECT);
        if (scm)
        {
            SC_HANDLE svc = OpenServiceA(scm, "SystemMonitoringAgent", SERVICE_START);
            if (svc)
            {
                StartServiceA(svc, 0, NULL);
                CloseServiceHandle(svc);
            }
            CloseServiceHandle(scm);
        }
        return 0;
    }

    char serviceName[] = "SystemMonitoringAgent";
    SERVICE_TABLE_ENTRYA table[] = {
        {serviceName, serviceMain},
        {NULL, NULL}};

    log("Starting service dispatcher...");
    StartServiceCtrlDispatcherA(table);
    return 0;
}
