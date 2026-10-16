package disk

import (
	"time"

	"github.com/ShAInyXYZ/NoClickDock/core/internal/model"
)

// Mock is an invented machine: a pool past the warning line, a share that
// stopped answering, and Docker sitting on enough to be worth pruning.
func (*Widget) Mock(time.Time) model.Snapshot {
	fs := func(mount, fstype string, kind Kind, totalGB, usedFrac float64) Filesystem {
		total := uint64(totalGB * 1e9)
		used := uint64(float64(total) * usedFrac)
		// ext4 keeps 5 % for root; free is what is left after it.
		free := uint64(float64(total)*0.95) - min(used, uint64(float64(total)*0.95))
		return Filesystem{Mount: mount, Label: mount, FSType: fstype, Kind: kind, Total: total, Used: used, Free: free}
	}
	pool := func(mount, dataset string, totalGB, usedFrac float64) Filesystem {
		f := fs(mount, "zfs", Pool, totalGB, usedFrac)
		f.Label, f.Device = dataset, dataset
		return f
	}
	share := func(mount, device string, totalGB, usedFrac float64) Filesystem {
		f := fs(mount, "cifs", Network, totalGB, usedFrac)
		f.Device = device
		return f
	}
	fss := []Filesystem{
		fs("/", "ext4", Local, 500, 0.71),
		fs("/home", "ext4", Local, 2000, 0.64),
		fs("/mnt/scratch", "btrfs", Local, 1000, 0.52),
		pool("/srv/media", "tank/media", 8000, 0.88),
		share("/mnt/nas", "//nas.example/archive", 12000, 0.41),
		{Mount: "/mnt/offsite", Label: "/mnt/offsite", FSType: "nfs4", Kind: Network, Unresponsive: true},
	}
	docker := &dockerSpace{
		root: "/var/lib/docker",
		rows: []dockerRow{
			{"Images", 41e9},
			{"Containers", 1.5e9},
			{"Local Volumes", 7.3e9},
			{"Build Cache", 23.3e9},
		},
	}
	for _, r := range docker.rows {
		docker.total += r.size
	}
	return build(fss, docker, []model.MenuItem{{ID: "open-analyzer", Label: "Open Disk Usage Analyzer"}})
}
