package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openBrowser best-effort opens url in the default browser (a normal tab).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// launchAppWindow opens url in a Chromium "app" window — a dedicated window with
// no tabs or address bar, so HookTrap looks and behaves like a native desktop
// app. A dedicated profile directory keeps the launched process alive for the
// window's lifetime, so the caller can wait on it and exit when it is closed.
// Returns nil if no Chromium-based browser is available.
func launchAppWindow(url, profileDir string) *exec.Cmd {
	bin := findChromium()
	if bin == "" {
		return nil
	}
	_ = os.MkdirAll(profileDir, 0o700)
	cmd := exec.Command(bin,
		"--app="+url,
		"--user-data-dir="+profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate,AutomationControlled",
		"--window-size=1280,860",
	)
	if err := cmd.Start(); err != nil {
		return nil
	}
	return cmd
}

// findChromium locates an installed Chromium-based browser (Edge is present on
// every Windows 10/11 install; Chrome is preferred elsewhere).
func findChromium() string {
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pfx := os.Getenv("ProgramFiles(x86)")
		la := os.Getenv("LocalAppData")
		for _, c := range []string{
			filepath.Join(pfx, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pfx, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(la, `Google\Chrome\Application\chrome.exe`),
		} {
			if c != "" {
				if _, err := os.Stat(c); err == nil {
					return c
				}
			}
		}
	case "darwin":
		for _, c := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		} {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	for _, n := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "chrome", "msedge"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}
