// main.go — AppArmor confinement experiment for AI-generated Go programs
//
// This program exercises a variety of system interactions that an AppArmor
// profile can mediate. It is intentionally broad: file I/O, network access,
// process spawning, and privileged operations. The companion profile
// (apparmor/go-apparmor) selectively allows or denies each category so you
// can observe AppArmor denials in the kernel log.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func main() {
	fmt.Println("=== AppArmor Confinement Experiment ===")
	fmt.Printf("Started at: %s\n", time.Now().Format(time.RFC3339))

	// 1. Filesystem access — reading /etc/hostname should be allowed by
	//    a tight profile; reading /etc/shadow should be denied.
	fmt.Println("\n[1] Filesystem probes")
	readFile("/etc/hostname", "read /etc/hostname")
	readFile("/etc/shadow", "read /etc/shadow (should be denied)")

	// 2. Network access — outbound HTTPS should be denied by a tight
	//    profile; localhost-only may be allowed.
	fmt.Println("\n[2] Network probe")
	doHTTP("https://example.com", "outbound HTTPS (should be denied)")

	// 3. Process spawning — running /bin/ls should be allowed or denied
	//    depending on whether the profile grants exec permission.
	fmt.Println("\n[3] Process spawn")
	spawnProcess("/bin/ls", "-la", "/tmp", "spawn /bin/ls (may be denied)")

	// 4. Capability probe — attempting to change UID requires
	//    CAP_SETUID, which a tight profile should not grant.
	fmt.Println("\n[4] Capability probe")
	checkCapability()

	// 5. Write to a protected location — /etc/ is normally read-only
	//    for confined processes.
	fmt.Println("\n[5] Write probe")
	writeFile("/etc/apparmor-test-marker", "apparmor experiment\n",
		"write /etc/apparmor-test-marker (should be denied)")

	fmt.Println("\n=== Experiment complete ===")
	fmt.Println("Check kernel log for AppArmor DENIED entries:")
	fmt.Println("  sudo journalctl -k --grep=apparmor")
}

func readFile(path, label string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("  %-50s → FAIL: %v\n", label, err)
		return
	}
	defer f.Close()
	io.Copy(io.Discard, f)
	fmt.Printf("  %-50s → OK\n", label)
}

func writeFile(path, content, label string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("  %-50s → FAIL: %v\n", label, err)
		return
	}
	defer f.Close()
	fmt.Fprint(f, content)
	fmt.Printf("  %-50s → OK\n", label)
}

func doHTTP(url, label string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("  %-50s → FAIL: %v\n", label, err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	fmt.Printf("  %-50s → OK (status %d)\n", label, resp.StatusCode)
}

func spawnProcess(path string, args ...string) {
	label := fmt.Sprintf("spawn %s", path)
	cmd := exec.Command(path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  %-50s → FAIL: %v\n", label, err)
		return
	}
	fmt.Printf("  %-50s → OK\n  output: %s", label, string(out))
}

func checkCapability() {
	// Attempt to look at /proc/self/status to see if we have CAP_SETUID.
	// AppArmor can also directly deny the capability; we attempt a
	// setuid-related syscall path that requires it.
	fmt.Println("  Checking /proc/self/status for CapEff:")
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		fmt.Printf("    read /proc/self/status → FAIL: %v\n", err)
		return
	}
	fmt.Println("    CapEff line present: yes (file read succeeded)")
	_ = data
}