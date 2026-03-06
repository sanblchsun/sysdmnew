//go:build !windows

package main

// Заглушки для других ОС
func initWindowsDPI()                   {}
func getPhysicalScreenSize() (int, int) { return 0, 0 }
