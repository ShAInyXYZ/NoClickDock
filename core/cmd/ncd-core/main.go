// ncd-core measures everything NoClickDock shows. The Tauri shell starts it
// as a sidecar and talks to it over stdio; for development it can serve the
// same messages over HTTP instead.
//
//	ncd-core                   stdio JSON lines (production)
//	ncd-core --dev             real collectors, HTTP + SSE on 127.0.0.1:--port
//	ncd-core --mock            invented data, HTTP + SSE on 127.0.0.1:--port
//	ncd-core --mock --stdio    invented data over stdio (the shell's `ncd --mock`)
//	ncd-core --mock --mock-live  invented data that moves: progress, failures that come and go
//	ncd-core --once disk       print one snapshot and exit (add --mock for invented data)
//	ncd-core --migrate         bring the Python widgets' settings over, once, and exit
//
// Only the first form is production: Power writes its hourly log to
// ~/Documents/PM-Log then, and to a scratch directory in every other.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/config"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/hub"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/sys"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/transport"
	"github.com/ShAInyXYZ/NoClickDock/core/internal/widgets"
)

func main() {
	var (
		dev       = flag.Bool("dev", false, "real collectors over HTTP + SSE, for the interface in a browser")
		mock      = flag.Bool("mock", false, "invented data over HTTP + SSE; never reads this machine")
		once      = flag.String("once", "", "print one `widget`'s snapshot as JSON and exit")
		port      = flag.Int("port", 47810, "HTTP port for --dev and --mock (always on 127.0.0.1)")
		stdio     = flag.Bool("stdio", false, "with --dev or --mock: speak stdio to the shell instead of serving HTTP")
		live      = flag.Bool("mock-live", false, "with --mock: let the invented data move with the clock (progress, failures that come and go)")
		configDir = flag.String("config", "", "config directory (default: the user's, or a scratch one for --dev and --mock)")
		migrate   = flag.Bool("migrate", false, "bring the Python widgets' settings over now (once: only while config.json does not exist), say what came, and exit")
	)
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("ncd-core: ")

	if *dev && *mock {
		log.Fatal("--dev and --mock are exclusive: one measures this machine, the other invents one")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scratch := *dev || *mock
	dir, err := chooseConfigDir(*configDir, scratch, *port)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Open(dir)
	if err != nil {
		log.Print(err) // defaults are in use; keep running
	}
	// The installer asks for the migration before it disables the legacy
	// autostart entries (which say which widgets were installed).
	if *migrate {
		legacy, err := config.LegacyDirs()
		if err != nil {
			log.Fatal(err)
		}
		brought, err := cfg.Migrate(legacy)
		if err != nil {
			log.Fatal(err)
		}
		if len(brought) == 0 {
			fmt.Println("nothing to bring over (config.json exists, or no Python widget settings)")
		}
		for _, b := range brought {
			fmt.Println("brought over:", b)
		}
		return
	}
	// The Python generation's settings come over once, on the real
	// config's first run only (read, never written): development never
	// reads them.
	if *configDir == "" && !scratch && *once == "" && runtime.GOOS == "linux" {
		if legacy, err := config.LegacyDirs(); err == nil {
			if brought, err := cfg.Migrate(legacy); err != nil {
				log.Printf("legacy settings: %v", err)
			} else if len(brought) > 0 {
				log.Printf("brought over from the Python widgets: %s", strings.Join(brought, "; "))
			}
		}
	}

	// Power's hourly log goes to ~/Documents/PM-Log only in production (no
	// --dev, --mock, --once or --config): every other run — the reviewers',
	// the tests', a named shell instance's — logs to a scratch directory.
	production := !scratch && *once == "" && *configDir == ""
	powerLog, err := chooseLogDir(production, dir, *once != "")
	if err != nil {
		log.Fatal(err)
	}
	collectors := widgets.Real(cfg, powerLog)
	if *mock {
		collectors = widgets.Mocks(*live)
	}

	// No collector command the core started outlives it.
	defer sys.StopAll()

	if *once != "" {
		if err := printOnce(ctx, collectors, *once); err != nil {
			sys.StopAll()
			log.Fatal(err)
		}
		return
	}

	h := hub.New(runtime.GOOS, cfg, collectors)
	go h.Run(ctx)

	if (*dev || *mock) && !*stdio {
		log.Printf("config: %s", cfg.Path())
		err = transport.HTTP(ctx, h, *port)
	} else {
		err = transport.Stdio(ctx, h, os.Stdin, os.Stdout)
	}
	if err != nil {
		sys.StopAll()
		log.Fatal(err)
	}
}

// chooseConfigDir keeps development away from the real config: --dev and
// --mock get a scratch directory under the user's own cache (not the
// world-writable temp dir, where another user could plant one), one per
// port so two reviewers' cores do not share a dock, unless one is named.
func chooseConfigDir(flagDir string, scratch bool, port int) (string, error) {
	switch {
	case flagDir != "":
		return flagDir, nil
	case scratch:
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "noclickdock-dev", fmt.Sprintf("core-%d", port)), nil
	}
	return config.Dir()
}

// chooseLogDir: ~/Documents/PM-Log (the Python widget's, whose history the
// report reads) in production; beside a scratch config otherwise; under the
// user's cache for --once, whose config is the real one.
func chooseLogDir(production bool, configDir string, once bool) (string, error) {
	switch {
	case production:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(documentsDir(home), "PM-Log"), nil
	case once:
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "noclickdock-dev", "once", "PM-Log"), nil
	}
	return filepath.Join(configDir, "PM-Log"), nil
}

func printOnce(ctx context.Context, collectors []hub.Collector, id string) error {
	var known []string
	for _, c := range collectors {
		meta := c.Meta()
		if meta.ID != id {
			known = append(known, meta.ID)
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		// A collector with slow background work (disk's docker df) asks for
		// a second collect when it lands; one snapshot should include it.
		landed := make(chan struct{}, 1)
		r, background := c.(hub.Refresher)
		if background {
			r.SetRefresh(func() {
				select {
				case landed <- struct{}{}:
				default:
				}
			})
		}
		snap, err := c.Collect(ctx)
		if err == nil && background {
			select {
			case <-landed:
				snap, err = c.Collect(ctx)
			case <-ctx.Done():
			}
		}
		if err != nil {
			return err
		}
		snap.ID, snap.Title, snap.Order, snap.PollSecs = meta.ID, meta.Title, meta.Order, meta.PollSecs
		snap.UpdatedAt = time.Now()
		out, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	return fmt.Errorf("no widget %q here; this mode has %s", id, strings.Join(known, ", "))
}
