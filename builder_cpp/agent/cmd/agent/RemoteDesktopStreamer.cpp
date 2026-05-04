#include "RemoteDesktopStreamer.h"
#include <iostream>

RemoteDesktopStreamer::RemoteDesktopStreamer(const Config &cfg)
    : config(cfg)
{
}

RemoteDesktopStreamer::~RemoteDesktopStreamer()
{
    stop();
    join_all_threads();
}

void RemoteDesktopStreamer::run()
{
    if (is_active) return;
    is_active = true;
    should_stop = false;

    // Запускаем все потоки
    spawn_control_loop();
    spawn_config_poll_loop();
    spawn_resolution_watch_loop();
    spawn_clipboard_watch_loop();

    // Запускаем основной поток streaming-a
    run_streaming_session();

    is_active = false;
}

void RemoteDesktopStreamer::stop()
{
    should_stop = true;
}

bool RemoteDesktopStreamer::is_running() const
{
    return is_active;
}

void RemoteDesktopStreamer::spawn_control_loop()
{
    // TODO: реализовать control loop из rmm_cpp
    std::cerr << "[RemoteDesktopStreamer] control_loop() TODO\n";
}

void RemoteDesktopStreamer::spawn_config_poll_loop()
{
    // TODO: реализовать config poll loop из rmm_cpp
    std::cerr << "[RemoteDesktopStreamer] config_poll_loop() TODO\n";
}

void RemoteDesktopStreamer::spawn_resolution_watch_loop()
{
    // TODO: реализовать resolution watch loop из rmm_cpp
    std::cerr << "[RemoteDesktopStreamer] resolution_watch_loop() TODO\n";
}

void RemoteDesktopStreamer::spawn_clipboard_watch_loop()
{
    // TODO: реализовать clipboard watch loop из rmm_cpp
    std::cerr << "[RemoteDesktopStreamer] clipboard_watch_loop() TODO\n";
}

void RemoteDesktopStreamer::run_streaming_session()
{
    // TODO: реализовать main streaming из rmm_cpp
    std::cerr << "[RemoteDesktopStreamer] run_streaming_session() TODO\n";
}

void RemoteDesktopStreamer::join_all_threads()
{
    std::lock_guard<std::mutex> lk(threads_mutex);
    if (control_thread.joinable()) control_thread.join();
    if (config_thread.joinable()) config_thread.join();
    if (resolution_thread.joinable()) resolution_thread.join();
    if (clipboard_thread.joinable()) clipboard_thread.join();
}
