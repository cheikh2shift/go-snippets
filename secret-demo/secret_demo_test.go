package secretdemo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

var canaryHex = "53555045522d5345435245542d4b4559"

var canaryBytes = []byte{
	0x53, 0x55, 0x50, 0x45, 0x52, 0x2d, 0x53, 0x45,
	0x43, 0x52, 0x45, 0x54, 0x2d, 0x4b, 0x45, 0x59,
}

func buildCanary(t *testing.T, variant string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), variant)
	cmd := exec.Command("go", "build", "-o", bin, "./canary/"+variant)
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=runtimesecret")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build canary/%s: %v\n%s", variant, err, stderr.String())
	}
	return bin
}

func runCanaryAndScan(t *testing.T, variant string) (found bool) {
	t.Helper()
	bin := buildCanary(t, variant)

	var rLimit syscall.Rlimit
	syscall.Getrlimit(syscall.RLIMIT_CORE, &rLimit)
	rLimit.Cur = rLimit.Max
	if rLimit.Cur == 0 {
		rLimit.Cur = 1 << 30
	}
	syscall.Setrlimit(syscall.RLIMIT_CORE, &rLimit)

	origPattern, _ := os.ReadFile("/proc/sys/kernel/core_pattern")
	coreDir := t.TempDir()
	corePath := filepath.Join(coreDir, "core")
	if err := os.WriteFile("/proc/sys/kernel/core_pattern", []byte(corePath), 0o644); err != nil {
		t.Fatalf("set core_pattern: %v", err)
	}
	defer os.WriteFile("/proc/sys/kernel/core_pattern", origPattern, 0o644)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"GOTRACEBACK=crash",
		"CANARY_HEX="+canaryHex,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start canary/%s: %v", variant, err)
	}

	time.Sleep(2 * time.Second)

	if err := cmd.Process.Signal(syscall.SIGSEGV); err != nil {
		t.Fatalf("signal canary/%s: %v", variant, err)
	}
	_, _ = cmd.Process.Wait()

	for i := 0; i < 50; i++ {
		if info, err := os.Stat(corePath); err == nil && info.Size() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	info, err := os.Stat(corePath)
	if err != nil {
		t.Fatalf("core dump not produced: %v", err)
	}
	t.Logf("core dump: %s (%d bytes)", corePath, info.Size())

	f, err := os.Open(corePath)
	if err != nil {
		t.Fatalf("open core dump: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 1<<20)
	for {
		n, readErr := f.Read(buf)
		if n > 0 && bytes.Contains(buf[:n], canaryBytes) {
			return true
		}
		if readErr != nil {
			break
		}
	}
	return false
}

func TestSecretNotInCoreDump(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"control", true},
		{"secret", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			found := runCanaryAndScan(t, tc.name)
			if found != tc.want {
				t.Fatalf("canary found=%v, expected %v", found, tc.want)
			}
			t.Logf("canary found=%v, expected %v OK", found, tc.want)
		})
	}
}
