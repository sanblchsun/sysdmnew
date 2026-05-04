#pragma once

#include <string>
#include <vector>
#include <thread>
#include <mutex>
#include <atomic>
#include <winsock2.h>

/**
 * RemoteDesktopStreamer - Инкапсулирует функциональность видеопотока и удаленного управления
 * Извлечено из rmm_cpp/agent/main.cpp
 * Используется builder_cpp агентом для запуска streaming по команде сервера
 */
class RemoteDesktopStreamer
{
public:
    struct Config
    {
        std::string server_host = "127.0.0.1";
        int server_port = 443;
        std::string agent_id = "agent1";
        bool verify_cert = false;

        std::string codec = "mjpeg";      // mjpeg или h264
        std::string encoder = "cpu";      // cpu, amf, qsv, nvenc
        std::string bitrate = "4M";
        int framerate = 30;
        int mjpeg_q = 4;

        std::string ffmpeg_path = "ffmpeg.exe";
    };

    RemoteDesktopStreamer(const Config &cfg);
    ~RemoteDesktopStreamer();

    // Запустить streaming (блокирующая)
    void run();

    // Остановить streaming
    void stop();

    // Проверить статус
    bool is_running() const;

private:
    Config config;
    std::atomic<bool> should_stop{false};
    std::atomic<bool> is_active{false};

    // Потоки
    std::thread control_thread;
    std::thread config_thread;
    std::thread resolution_thread;
    std::thread clipboard_thread;

    // Для синхронизации
    std::mutex threads_mutex;

    // Внутренние методы
    void spawn_control_loop();
    void spawn_config_poll_loop();
    void spawn_resolution_watch_loop();
    void spawn_clipboard_watch_loop();
    void run_streaming_session();

    void join_all_threads();
};
