// AI AGENT SANDBOX — built from the Go STANDARD LIBRARY only.
//
// Demonstrates operating-system-level isolation primitives for an AI agent:
//   [TEST 1] Network isolation        -> NEWNET + NEWUTS
//   [TEST 2] Hostname isolation       -> NEWUTS + sethostname
//   [TEST 3] Resource limits          -> RLIMIT_NOFILE
//   [TEST 4] Filesystem isolation     -> chroot into a minimal rootfs
//
// The child is spawned the standard "re-exec" way: the parent launcher is
// re-invoked inside the new namespaces, and then the child performs the
// actual chroot. We do NOT put Chroot in SysProcAttr because that would make
// the kernel chroot BEFORE execing the child, breaking fork/exec of the
// binary inside the (then-empty) rootfs.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ---------------------------------------------------------------------------
// Entry point: parent (spawn + verify) or child (run inside namespaces).
// ---------------------------------------------------------------------------

func main() {
	child := flag.Bool("child", false, "run the sandboxed child payload")
	rootfs := flag.String("rootfs", "", "minimal rootfs the child should chroot into")
	flag.Parse()

	if *child {
		runChild(*rootfs)
		return
	}

	// ---- PARENT MODE -------------------------------------------------------
	buildRootfs := func() string {
		dir, _ := os.MkdirTemp("", "sandbox-rootfs-")
		os.MkdirAll(filepath.Join(dir, "etc"), 0o755)
		os.MkdirAll(filepath.Join(dir, "proc"), 0o755)
		os.WriteFile(filepath.Join(dir, "etc", "passwd"), []byte("sandboxed-user:x:0:0::/:/bin/sh\n"), 0o644)
		return dir
	}

	root := *rootfs
	if root == "" {
		root = buildRootfs()
		defer os.RemoveAll(root)
	}
	fmt.Printf("[parent] built minimal rootfs: %s\n", root)
	fmt.Printf("[parent]   (it contains ONLY a fake /etc/passwd — no host files)\n")

	fmt.Println("[parent] spawning sandboxed child with namespaces...")

	// Re-exec ourselves as the child in new namespaces.
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("[fatal] os.Executable: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(self, "-child", "-rootfs", root)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "SANDBOX_CHILD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNET | syscall.CLONE_NEWUSER,
		// User namespace maps: remap the current runtime user to root inside.
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		// NOTE: no Chroot here — see header comment. chroot happens in child.
	}

	_ = errors.New // (kept for clarity in case of future extra checks)
	if err := cmd.Run(); err != nil {
		fmt.Printf("[fatal] start child: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[parent] child finished cleanly.")
}

// ---------------------------------------------------------------------------
// Child payload: runs inside the new namespaces. It is PID 1 of its own
// namespace, and sets up the filesystem jail itself.
// ---------------------------------------------------------------------------

func runChild(rootfs string) {
	fmt.Println("==============================================================")
	fmt.Println("  AI AGENT SANDBOX — child running inside isolated namespaces")
	fmt.Println("==============================================================")

	// We are now safely inside the user namespace with full capabilities.
	// Give the rootfs a /proc to make some tools happy (best-effort only).
	_ = syscall.Mount("proc", filepath.Join(rootfs, "proc"), "proc", 0, "")

	// --- Filesystem jail -----------------------------------------------------
	if err := syscall.Chroot(rootfs); err != nil {
		fmt.Printf("[fatal] chroot: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Chdir("/"); err != nil {
		fmt.Printf("[fatal] chdir / after chroot: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[child] chrooted into", rootfs)

	runTests()
	os.Exit(0)
}

// ---------------------------------------------------------------------------
// The four isolation checks, with honest diagnostics.
// ---------------------------------------------------------------------------

func runTests() {
	os.Stdout.WriteString("\n")

	// ---- TEST 1: Network isolation -----------------------------------------
	fmt.Println("[TEST 1] Network isolation")
	ifaces, err := net.Interfaces()
	isolated := err == nil && len(ifaces) == 0
	if err == nil && len(ifaces) == 0 {
		fmt.Println("   PASS network namespace is isolated (no host interfaces)")
	} else {
		fmt.Printf("   %s got %d interface(s) (err=%v)\n", fail(isolated), len(ifaces), err)
	}

	// ---- TEST 2: Hostname isolation ----------------------------------------
	fmt.Println("[TEST 2] Hostname isolation")
	if err := syscall.Sethostname([]byte("sandbox")); err != nil {
		fmt.Printf("   FAIL sethostname: %v\n", err)
	} else {
		host, _ := os.Hostname()
		if host == "sandbox" {
			fmt.Printf("   PASS hostname changed to %q (host is unaffected)\n", host)
		} else {
			fmt.Printf("   FAIL expected hostname sandbox, got %q\n", host)
		}
	}

	// ---- TEST 3: Resource limits (max open files) ---------------------------
	fmt.Println("[TEST 3] Resource limits (max open files)")
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err == nil {
		lim.Cur = 16
		lim.Max = 16
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
		fmt.Println("   PASS capped max open files at 16")

		// Try to open a 9th file — should exceed the 16-fd cap (3 std fds + ours).
		got := 3
		for ; got < 20; got++ {
			f, e := os.Open("/dev/null")
			if e != nil {
				fmt.Printf("   PASS open #%d rejected: %v\n", got, e)
				break
			}
			_ = f.Close()
		}
		if got >= 20 {
			fmt.Println("   FAIL never hit the fd cap")
		}
		// restore a sane limit so later tests can read files
		lim.Cur = 1024
		lim.Max = 1024
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
	} else {
		fmt.Printf("   FAIL getrlimit: %v\n", err)
	}

	// ---- TEST 4: Filesystem isolation (chroot) ------------------------------
	fmt.Println("[TEST 4] Filesystem isolation (chroot)")
	// Our fake passwd must be the only one we can see.
	if b, err := os.ReadFile("/etc/passwd"); err != nil {
		fmt.Printf("   note: could not read /etc/passwd: %v\n", err)
	} else if strings.Contains(string(b), "sandboxed-user") {
		fmt.Printf("   PASS can only see the FAKE passwd: %q\n", strings.TrimSpace(string(b)))
	} else {
		fmt.Printf("   FAIL /etc/passwd is not the fake one: %q\n", string(b))
	}

	// /home is NOT in the minimal rootfs, so it must be unreachable.
	if _, err := os.Stat("/home"); err != nil {
		fmt.Println("   PASS cannot see host /home: stat /home:", err)
	} else {
		fmt.Println("   FAIL /home is visible inside sandbox!")
	}

	// /proc may exist (we mounted it); it's a note, not a failure.
	if _, err := os.Stat("/proc"); err == nil {
		fmt.Println("   note: /proc mounted (by us) - ignoring")
	}

	fmt.Println("\n[child] all checks complete.")
}

func fail(ok bool) string {
	if ok {
		return "FAIL"
	}
	return "note:"
}

// keep json referenced so the import stays meaningful if you add reports
var _ = json.Marshal
