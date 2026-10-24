//go:build linux

package localservers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// The helper is a disposable TCP server, never an existing machine process.
func TestFixtureProcess(t *testing.T) {
	if os.Getenv("NCD_FIXTURE") != "1" {
		return
	}
	l, err := net.Listen("tcp4", "127.0.0.1:"+os.Getenv("NCD_FIXTURE_PORT"))
	if err != nil {
		os.Exit(3)
	}
	fmt.Println(l.Addr().(*net.TCPAddr).Port)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	l.Close()
	os.Exit(0)
}
func fixture(t *testing.T, dir, port string) (*exec.Cmd, int) {
	t.Helper()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=^TestFixtureProcess$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NCD_FIXTURE=1", "NCD_FIXTURE_PORT="+port)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	var got int
	line, _ := bufio.NewReader(out).ReadString('\n')
	if _, err = fmt.Sscan(line, &got); err != nil {
		t.Fatalf("fixture start: %v %q", err, line)
	}
	return cmd, got
}
func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func findPID(t *testing.T, all []server, pid int) server {
	t.Helper()
	for _, s := range all {
		if s.Proc.PID == pid {
			return s
		}
	}
	t.Fatalf("PID %d not found", pid)
	return server{}
}

func TestDiscoveryGroupingStopAndPortReuse(t *testing.T) {
	ctx := context.Background()
	dir := project(t)
	first, port := fixture(t, dir, "0")
	second, _ := fixture(t, dir, "0")
	all, err := discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a, b := findPID(t, all, first.Process.Pid), findPID(t, all, second.Process.Pid)
	if !a.Dev || a.Protected != "" || a.Dir != b.Dir || a.Name != b.Name {
		t.Fatalf("bad classification: %+v / %+v", a, b)
	}
	snap := render([]server{a, b}, false, nil, nil, nil)
	groups := 0
	for _, block := range snap.Pages[0].Blocks {
		if _, ok := block.(model.Section); ok {
			groups++
		}
	}
	if groups != 1 {
		t.Fatalf("same project split into %d groups", groups)
	}
	if err = stopProcess(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = verify(b, true); err != nil {
		t.Fatalf("stopping one server affected sibling: %v", err)
	}
	replacement, _ := fixture(t, dir, fmt.Sprint(port))
	if err = stopProcess(ctx, a); err == nil {
		t.Fatal("stale server could be stopped")
	}
	now, _ := discover(ctx)
	r := findPID(t, now, replacement.Process.Pid)
	if r.Key == a.Key || r.Endpoints[0].Port != port {
		t.Fatal("port reuse did not get fresh identity")
	}
	if err = verify(r, true); err != nil {
		t.Fatalf("replacement harmed: %v", err)
	}
	forged := r
	forged.Proc.Start = "0"
	if err = stopProcess(ctx, forged); err == nil {
		t.Fatal("mismatched start identity accepted")
	}
	forged = r
	forged.Endpoints = append([]endpoint{}, r.Endpoints...)
	forged.Endpoints[0].Inode = "obsolete"
	if err = stopProcess(ctx, forged); err == nil {
		t.Fatal("changed socket accepted")
	}
	if err = verify(r, true); err != nil {
		t.Fatalf("forged controls harmed replacement: %v", err)
	}
}

func TestExitBetweenDiscoveryAndAction(t *testing.T) {
	dir := project(t)
	cmd, _ := fixture(t, dir, "0")
	all, _ := discover(context.Background())
	s := findPID(t, all, cmd.Process.Pid)
	cmd.Process.Signal(syscall.SIGTERM)
	cmd.Wait()
	if err := stopProcess(context.Background(), s); err == nil {
		t.Fatal("exited process accepted")
	}
}
func TestSocketParsingAndProtection(t *testing.T) {
	text := " 0: 0100007F:1455 00000000:0000 0A 0 0 0 1000 0 81\n 1: 00000000000000000000000001000000:1F40 0:0 0A 0 0 0 1000 0 82\n 2: 0101A8C0:0050 0:0 0A 0 0 0 1000 0 83\n 3: 0100007F:0050 0:0 01 0 0 0 1000 0 84"
	ss := parseSockets(text)
	if len(ss) != 2 || ss[0].port != 5205 || ss[1].address != "::1" {
		t.Fatalf("parse: %+v", ss)
	}
	for _, name := range []string{"docker-proxy", "tailscaled", "postgres", "redis-server", "bookmarkeep", "ncd-core", "bash"} {
		s := describe(process{Exe: "/usr/bin/" + name, Cwd: "/tmp", Args: []string{name}})
		if s.Protected == "" {
			t.Errorf("%s is not protected", name)
		}
	}
	for _, args := range [][]string{{"node", "vite"}, {"node", "next", "dev"}, {"python3", "-m", "http.server"}, {"python3", "-m", "uvicorn"}, {"ruby", "rails", "server"}} {
		s := describe(process{Exe: "/usr/bin/" + args[0], Cwd: "/tmp", Args: args})
		if !s.Dev {
			t.Errorf("runtime not detected: %v", args)
		}
	}
}
func TestNoPrivateArgumentsInSnapshot(t *testing.T) {
	dir := project(t)
	s := describe(process{PID: 123, Exe: "/usr/bin/node", Cwd: dir, Args: []string{"node", "vite", "--token=CANARY_SECRET"}})
	s.Key = "123:1"
	s.Running = true
	s.Endpoints = []endpoint{{5173, "127.0.0.1", "1"}}
	raw, _ := json.Marshal(render([]server{s}, true, nil, nil, nil))
	if strings.Contains(string(raw), "CANARY_SECRET") || strings.Contains(string(raw), "--token") {
		t.Fatal("arguments leaked")
	}
}
func TestRegistrationValidation(t *testing.T) {
	dir := project(t)
	path := filepath.Join(dir, "launch.json")
	write := func(l launch, mode os.FileMode) {
		b, _ := json.Marshal(l)
		os.WriteFile(path, b, mode)
		os.Chmod(path, mode)
	}
	write(launch{dir, []string{"/bin/sh", "-c", "echo bad"}}, 0o600)
	if _, err := readLaunch(path); err == nil {
		t.Fatal("shell accepted")
	}
	write(launch{dir, []string{"/bin/sleep", "1"}}, 0o644)
	if _, err := readLaunch(path); err == nil {
		t.Fatal("public launch accepted")
	}
	write(launch{dir, []string{"/bin/sleep", "1"}}, 0o600)
	if _, err := readLaunch(path); err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	os.Symlink("/bin/sleep", path)
	if _, err := readLaunch(path); err == nil {
		t.Fatal("symlink accepted")
	}
}

// Use a fresh, isolated user unit and a temporary directory. No existing unit
// is ever controlled. Skip only when the host has no user service manager.
func TestRegisteredSystemdLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	b := linuxBackend{}
	unit := fmt.Sprintf("ncd-fixture-%d-%d.service", os.Getpid(), time.Now().UnixNano())
	if _, err := b.Service(ctx, unit); err != nil {
		t.Skip("user systemd unavailable")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	dir := project(t)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	t.Cleanup(func() { exec.Command("systemctl", "--user", "stop", unit).Run() })
	if err = b.Launch(ctx, launch{dir, []string{python, "-m", "http.server", fmt.Sprint(port), "--bind", "127.0.0.1"}}, unit); err != nil {
		t.Fatal(err)
	}
	wait := func(old string) server {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			all, _ := b.Scan(ctx)
			for _, s := range all {
				if b.Unit(s.Proc) == unit && s.Key != old {
					info, e := b.Service(ctx, unit)
					if e == nil && b.Exclusive(s, info) {
						s.Unit = unit
						s.Invocation = info.Invocation
						return s
					}
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("fixture unit never listened")
		return server{}
	}
	first := wait("")
	if err = b.Control(ctx, first, "restart"); err != nil {
		t.Fatal(err)
	}
	second := wait(first.Key)
	if err = b.Control(ctx, first, "stop"); err == nil {
		t.Fatal("old invocation accepted")
	}
	if _, err = b.Logs(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err = b.Control(ctx, second, "stop"); err != nil {
		t.Fatal(err)
	}
	if err = b.Verify(second); err == nil {
		t.Fatal("fixture still running")
	}
}

func TestWidgetRegisteredRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := serviceInfo(ctx, "ncd-fixture-check.service"); err != nil {
		t.Skip("user systemd unavailable")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python unavailable")
	}
	dir := project(t)
	cmd, port := fixture(t, dir, "0")
	w := New(t.TempDir())
	if _, err = w.Collect(ctx); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	var original server
	for _, s := range w.last {
		if s.Proc.PID == cmd.Process.Pid {
			original = s
		}
	}
	w.mu.Unlock()
	if original.Key == "" {
		t.Fatal("fixture missing")
	}
	if _, err = w.Act(ctx, "restart", map[string]string{"server": original.Key}); err == nil {
		t.Fatal("unregistered restart allowed")
	}
	if err = verify(original, true); err != nil {
		t.Fatalf("unsupported restart stopped server: %v", err)
	}
	os.MkdirAll(w.dir, 0o700)
	spec := launch{dir, []string{python, "-m", "http.server", fmt.Sprint(port), "--bind", "127.0.0.1"}}
	raw, _ := json.Marshal(spec)
	os.WriteFile(filepath.Join(w.dir, original.Registration+".json"), raw, 0o600)
	unit := "ncd-dev-" + original.Registration + ".service"
	t.Cleanup(func() { exec.Command("systemctl", "--user", "stop", unit).Run() })
	w.Collect(ctx)
	if _, err = w.Act(ctx, "restart", map[string]string{"server": original.Key}); err != nil {
		t.Fatal(err)
	}
	w.Collect(ctx)
	w.mu.Lock()
	var current server
	for _, s := range w.last {
		if s.Running && s.Unit == unit {
			current = s
		}
	}
	old := w.last[original.Key]
	w.mu.Unlock()
	if current.Key == "" || current.Key == original.Key {
		t.Fatal("new server identity missing")
	}
	if old.Running || !strings.Contains(old.RestartReason, "Port is now in use") {
		t.Fatal("old process retained stale running/start state")
	}
	if _, err = w.Act(ctx, "stop", map[string]string{"server": current.Key}); err != nil {
		t.Fatal(err)
	}
	w.Collect(ctx)
	w.mu.Lock()
	stopped := w.last[current.Key]
	w.mu.Unlock()
	if stopped.Running {
		t.Fatal("stopped server still running in snapshot")
	}
	if _, err = w.Act(ctx, "logs", map[string]string{"server": current.Key}); err != nil {
		t.Fatalf("stopped logs: %v", err)
	}
	if _, err = w.Act(ctx, "restart", map[string]string{"server": current.Key}); err != nil {
		t.Fatalf("registered start after stop: %v", err)
	}
}

func TestLogsRedactArgumentAndEnvironmentValues(t *testing.T) {
	private := []string{"--token=ARG_CANARY", "ENV_CANARY", "https://alice:PASS_CANARY@example.test/?key=QUERY_CANARY", "line-one\nLINE_CANARY"}
	result := redactLogs("ready\nARG_CANARY\nENV_CANARY\nPASS_CANARY\nQUERY_CANARY\nLINE_CANARY\nAuthorization: something\n", private)
	if strings.Contains(result, "CANARY") || strings.Contains(result, "something") {
		t.Fatalf("private material escaped: %q", result)
	}
	if !strings.Contains(result, "ready") {
		t.Fatal("ordinary diagnostic was discarded")
	}
}
