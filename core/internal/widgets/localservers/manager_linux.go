//go:build linux

package localservers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
	"golang.org/x/sys/unix"
)

type linuxBackend struct{}

func newBackend() backend                                       { return linuxBackend{} }
func (linuxBackend) Scan(ctx context.Context) ([]server, error) { return discover(ctx) }
func (linuxBackend) Verify(s server) error                      { return verify(s, true) }
func (linuxBackend) Stop(ctx context.Context, s server) error   { return stopProcess(ctx, s) }
func (linuxBackend) Unit(p process) string                      { return unitOf(p) }

func serviceInfo(ctx context.Context, unit string) (service, error) {
	if !validUnit.MatchString(unit) {
		return service{}, errors.New("Invalid service identity")
	}
	out, err := sys.Run(ctx, 3*time.Second, "systemctl", "--user", "show", "--no-pager", "--property=MainPID,InvocationID,ControlGroup,ActiveState,KillMode", "--", unit)
	if err != nil {
		return service{}, errors.New("User service manager is unavailable")
	}
	s := service{Unit: unit}
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "MainPID":
			s.PID, _ = strconv.Atoi(v)
		case "InvocationID":
			s.Invocation = v
		case "ControlGroup":
			s.Group = v
		case "ActiveState":
			s.Active = v
		}
	}
	return s, nil
}
func (linuxBackend) Service(ctx context.Context, unit string) (service, error) {
	return serviceInfo(ctx, unit)
}

// Do not operate a unit shared with another process. This deliberately excludes
// terminal scopes, PM2 daemons, worker pools and services with wrapper processes.
func exclusiveService(s server, info service) bool {
	if info.PID != s.Proc.PID || info.Invocation == "" || info.Group == "" || info.Active != "active" {
		return false
	}
	if unitOf(s.Proc) != info.Unit {
		return false
	}
	root := filepath.Join("/sys/fs/cgroup", filepath.Clean(info.Group))
	if !strings.HasPrefix(root, "/sys/fs/cgroup/") {
		return false
	}
	count := 0
	valid := true
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.Name() != "cgroup.procs" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pid := range strings.Fields(string(b)) {
			count++
			if pid != strconv.Itoa(s.Proc.PID) {
				valid = false
			}
		}
		return nil
	})
	return err == nil && valid && count == 1
}
func (linuxBackend) Exclusive(s server, info service) bool { return exclusiveService(s, info) }
func (linuxBackend) Control(ctx context.Context, s server, verb string) error {
	if verb != "stop" && verb != "restart" {
		return errors.New("Unsupported service action")
	}
	if err := verify(s, true); err != nil {
		return err
	}
	current, err := serviceInfo(ctx, s.Unit)
	if err != nil {
		return err
	}
	if current.Invocation != s.Invocation || !exclusiveService(s, current) {
		return errors.New("Service instance changed or has shared processes; refresh before acting")
	}
	// Recheck the process after the manager query, directly before dispatch.
	if err := verify(s, true); err != nil {
		return err
	}
	_, err = sys.Run(ctx, 15*time.Second, "systemctl", "--user", verb, "--", s.Unit)
	if err != nil {
		return errors.New("Service manager could not complete the action; check the service logs")
	}
	return nil
}

func (linuxBackend) Launch(ctx context.Context, spec launch, unit string) error {
	// Never re-use an active unit or replace an unrelated service of the same name.
	info, err := serviceInfo(ctx, unit)
	if err != nil {
		return err
	}
	if info.PID != 0 || (info.Active != "inactive" && info.Active != "failed" && info.Active != "") {
		return errors.New("Registered service is still active")
	}
	args := []string{"--user", "--quiet", "--collect", "--service-type=exec", "--unit=" + unit, "--property=WorkingDirectory=" + spec.Directory, "--property=KillMode=process"}
	// Explicit registrations do not inherit NCD's environment or shell setup.
	// systemd's user environment is cleared by an absolute env executable.
	envBin := sys.Find("env")
	if envBin == "" {
		return errors.New("env is required for a registered launch")
	}
	args = append(args, "--", envBin, "-i", "PATH=/usr/local/bin:/usr/bin:/bin", "HOME="+os.Getenv("HOME"))
	args = append(args, spec.Argv...)
	_, err = sys.Run(ctx, 10*time.Second, "systemd-run", args...)
	if err != nil {
		return errors.New("Could not start the registered command; verify its executable, directory and user systemd session")
	}
	return nil
}

// Read output only from the verified invocation, never a reused unit's history.
func (linuxBackend) Logs(ctx context.Context, s server) (string, error) {
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(s.Invocation) {
		return "", errors.New("No verified service invocation is available")
	}
	if s.Running {
		if err := verify(s, true); err != nil {
			return "", err
		}
		info, err := serviceInfo(ctx, s.Unit)
		if err != nil {
			return "", err
		}
		if info.Invocation != s.Invocation || !exclusiveService(s, info) {
			return "", errors.New("Service instance changed")
		}
		capturePrivate(&s.Proc)
	}
	if !s.Proc.EnvKnown {
		return "", errors.New("Cannot safely redact this process's logs")
	}
	out, err := sys.Run(ctx, 3*time.Second, "journalctl", "--user", "--no-pager", "--output=cat", "--lines=20", "_SYSTEMD_INVOCATION_ID="+s.Invocation)
	if err != nil {
		return "", errors.New("Logs are not available from the user journal")
	}
	// Arguments and environment never leave the backend. Suppress every line
	// containing a value from either source, as well as credential-like lines.
	private := append(append([]string{}, s.Proc.Args...), s.Proc.Private...)
	return redactLogs(out, private), nil
}

func privateLaunchFile(path string) error {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o077 != 0 {
		return errors.New("Launch file must be owned by you, mode 0600, and not a symlink")
	}
	return nil
}
func readLaunch(path string) (launch, error) {
	var l launch
	if err := privateLaunchFile(path); err != nil {
		return l, errors.New("Launch file must be a private, user-owned regular file (0600)")
	}
	b, err := readSmall(path)
	if err != nil {
		return l, errors.New("Cannot read the launch file")
	}
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&l) != nil {
		return l, errors.New("Launch file is not valid JSON; expected directory and argv")
	}
	if !filepath.IsAbs(l.Directory) || len(l.Argv) == 0 || !filepath.IsAbs(l.Argv[0]) {
		return l, errors.New("Register an absolute executable in argv and an absolute directory")
	}
	if !sys.Has("systemd-run") || !sys.Has("systemctl") || !sys.Has("env") {
		return l, errors.New("Registered launches require systemd-run, systemctl and env")
	}
	// Older systemd-run versions expand $ and % without an opt-out. Refuse
	// those configurations instead of silently changing the launch arguments.
	for _, value := range append(append([]string{}, l.Argv...), l.Directory, os.Getenv("HOME")) {
		if strings.ContainsAny(value, "$%\r\n") {
			return l, errors.New("Launch arguments and directories containing $, %, or newlines need a separately configured user service")
		}
	}
	if len(l.Argv) > 128 {
		return l, errors.New("Too many launch arguments")
	}
	if st, err := os.Stat(l.Directory); err != nil || !st.IsDir() {
		return l, errors.New("Launch directory is unavailable")
	}
	if st, err := os.Stat(l.Argv[0]); err != nil || !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
		return l, errors.New("Registered executable is unavailable or not executable")
	}
	for _, a := range l.Argv {
		if strings.ContainsRune(a, 0) || len(a) > 8192 {
			return l, errors.New("Invalid launch argument")
		}
	}
	// A launch is direct argv, never a shell, terminal, daemon or process manager.
	resolved, err := filepath.EvalSymlinks(l.Argv[0])
	if err != nil {
		return l, errors.New("Cannot resolve the registered executable")
	}
	base := filepath.Base(resolved)
	for _, bad := range []string{"sh", "bash", "zsh", "fish", "dash", "sudo", "su", "env", "systemctl", "systemd-run", "docker", "pm2", "npm", "npx", "pnpm", "yarn", "nohup"} {
		if base == bad {
			return l, errors.New("Use the server executable directly, without a shell, package runner or process manager")
		}
	}
	check := describe(process{Exe: l.Argv[0], Cwd: l.Directory, Args: l.Argv})
	if check.Protected != "" {
		return l, errors.New("Infrastructure commands cannot be registered here")
	}
	return l, nil
}

// Capture only for a verified user service; retained in memory for its stopped logs.
func capturePrivate(p *process) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", p.PID))
	if err != nil {
		return
	}
	p.Private = nil
	for _, kv := range strings.Split(string(b), "\x00") {
		_, v, ok := strings.Cut(kv, "=")
		if ok && v != "" {
			p.Private = append(p.Private, v)
		}
	}
	p.EnvKnown = true
}
