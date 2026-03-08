//go:build windows

// builder/agent/dpi_windows.go
package main

import (
	"log"
	"syscall"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	setDPIAware         = user32.NewProc("SetProcessDPIAware")
	getDpiForWindow     = user32.NewProc("GetDpiForWindow")
	getDesktopWindow    = user32.NewProc("GetDesktopWindow")
	getSystemMetricsFor = user32.NewProc("GetSystemMetricsForDpi")
)

func initWindowsDPI() {
	setDPIAware.Call()
}

// Получаем физические (не масштабированные) размеры экрана
func getPhysicalScreenSize() (int, int) {
	hwnd, _, _ := getDesktopWindow.Call()
	dpi, _, _ := getDpiForWindow.Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	const (
		SM_CXSCREEN = 0
		SM_CYSCREEN = 1
	)
	w, _, _ := getSystemMetricsFor.Call(SM_CXSCREEN, dpi)
	h, _, _ := getSystemMetricsFor.Call(SM_CYSCREEN, dpi)
	log.Printf("[DPI] physical screen %dx%d (dpi=%d)", w, h, dpi)
	return int(w), int(h)
}
