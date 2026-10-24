// Package localservers discovers local TCP listeners without probing them.
// Only identity-bound, same-user development processes are controllable.
package localservers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type process struct {
	PID, PPID, UID                 int
	Start, State, Exe, Cwd, Cgroup string
	Args                           []string
	Private                        []string // redaction values; never serialized
	EnvKnown                       bool
}
type endpoint struct {
	Port        int
	Host, Inode string
}

func (e endpoint) URL() string { return "http://" + net.JoinHostPort(e.Host, fmt.Sprint(e.Port)) }

type server struct {
	Key, Name, Dir, Runtime, Protected, Unit, Invocation, RestartReason, Registration string
	Proc                                                                              process
	Endpoints                                                                         []endpoint
	Dev, Running                                                                      bool
}

var validUnit = regexp.MustCompile(`^[a-zA-Z0-9_.@:-]+\.service$`)
var secretPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api.?key|authorization|credential|cookie|bearer|://[^/\s]+:[^/\s]+@)`)

func safeText(s string) string {
	if secretPattern.MatchString(s) {
		return "[private]"
	}
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	return string([]rune(s)[:min(len([]rune(s)), 180)])
}
func registrationKey(s server) string {
	h := sha256.Sum256([]byte(s.Dir + "\x00" + fmt.Sprint(s.Endpoints[0].Port)))
	return fmt.Sprintf("%x", h[:8])
}

func describe(p process) server {
	s := server{Name: filepath.Base(p.Cwd), Dir: p.Cwd, Runtime: filepath.Base(p.Exe), Proc: p}
	text := strings.ToLower(p.Exe + " " + p.Cwd + " " + strings.Join(p.Args, " ") + " " + p.Cgroup)
	for _, word := range []string{"noclickdock", "ncd-core", "bookmarkeep", "book-markeep", "tailscale", "docker", "containerd", "postgres", "mysqld", "mariadb", "redis-server", "mongod", "elasticsear", "rabbitmq", "memcached", "sshd", "code-server"} {
		if strings.Contains(text, word) {
			s.Protected = "Infrastructure service"
			break
		}
	}
	base := strings.ToLower(filepath.Base(p.Exe))
	for prefix, label := range map[string]string{"node": "Node.js", "python": "Python", "bun": "Bun", "deno": "Deno", "ruby": "Ruby", "php": "PHP", "dotnet": ".NET", "java": "Java"} {
		if strings.HasPrefix(base, prefix) {
			s.Runtime = label
			break
		}
	}
	if strings.HasPrefix(base, "systemd") {
		s.Protected = "Infrastructure service"
	}
	if base == "bash" || base == "sh" || base == "zsh" || base == "fish" || base == "dash" || base == "ksh" || strings.Contains(base, "terminal") {
		s.Protected = "Terminal or shell"
	}
	for _, candidate := range []struct{ needle, label string }{
		{"vite", "Vite"}, {"next", "Next.js"}, {"astro", "Astro"}, {"nuxt", "Nuxt"}, {"webpack", "Webpack"}, {"react-scripts", "React"}, {"uvicorn", "Uvicorn"}, {"gunicorn", "Gunicorn"}, {"manage.py", "Django"}, {"flask", "Flask"}, {"http.server", "Python HTTP"}, {"rails", "Rails"}, {"artisan", "Laravel"}, {"deno", "Deno"}, {"bun", "Bun"},
	} {
		if strings.Contains(strings.ToLower(strings.Join(p.Args, " ")), candidate.needle) {
			s.Runtime = candidate.label
			s.Dev = true
			break
		}
	}
	// Nearest manifest wins, so monorepo packages remain separate projects.
	for dir, depth := p.Cwd, 0; dir != "/" && dir != "." && depth < 8; dir, depth = filepath.Dir(dir), depth+1 {
		b, err := readSmall(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Name                          string `json:"name"`
				Dependencies, DevDependencies map[string]json.RawMessage
			}
			if json.Unmarshal(b, &pkg) == nil {
				s.Dir = dir
				s.Name = filepath.Base(dir)
				if pkg.Name != "" {
					s.Name = pkg.Name
				}
				if base == "node" || base == "nodejs" || base == "bun" || base == "deno" {
					s.Dev = true
				}
				break
			}
		}
		found := false
		for _, manifest := range []string{"pyproject.toml", "Cargo.toml", "go.mod", "Gemfile", "composer.json"} {
			if st, err := os.Stat(filepath.Join(dir, manifest)); err == nil && !st.IsDir() {
				found = true
				if manifest == "go.mod" && s.Runtime == filepath.Base(p.Exe) {
					s.Runtime = "Go"
				}
				if manifest == "Cargo.toml" && s.Runtime == filepath.Base(p.Exe) {
					s.Runtime = "Rust"
				}
				break
			}
		}
		if found {
			s.Dir = dir
			s.Name = filepath.Base(dir)
			s.Dev = true
			break
		}
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st != nil {
			break
		}
	}
	if s.Name == "" || s.Name == "/" {
		s.Name = s.Runtime
	}
	if s.Protected != "" {
		s.Dev = false
	}
	return s
}

func readSmall(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > 64*1024 {
		return nil, fmt.Errorf("File too large")
	}
	return os.ReadFile(path)
}

// Drop whole lines rather than risk exposing a credential fragment in a log.
func redactLogs(out string, private []string) string {
	var values []string
	for _, raw := range private {
		candidates := []string{raw}
		if _, value, ok := strings.Cut(raw, "="); ok {
			candidates = append(candidates, value)
		}
		for _, candidate := range candidates {
			for _, line := range strings.Split(candidate, "\n") {
				if line != "" {
					values = append(values, line)
				}
			}
			if u, err := url.Parse(candidate); err == nil && u.Scheme != "" {
				if u.User != nil {
					values = append(values, u.User.Username())
					if p, ok := u.User.Password(); ok {
						values = append(values, p)
					}
				}
				for _, list := range u.Query() {
					values = append(values, list...)
				}
			}
		}
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		redact := secretPattern.MatchString(line)
		for _, value := range values {
			if value != "" && strings.Contains(line, value) {
				redact = true
				break
			}
		}
		if redact {
			line = "[private log line hidden]"
		} else {
			line = safeText(line)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "No journal entries for this service instance."
	}
	return strings.Join(lines, "\n")
}
