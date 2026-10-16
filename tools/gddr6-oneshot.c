// oneshot.c — read each supported GPU's VRAM temp once and exit.
// Output: one line per card, "<bus>:<dev>.<func> <temp>" (bus in lowercase hex,
// matching the middle of nvidia-smi's pci.bus_id), e.g. "11:00.0 62".
#include "gddr6.h"
#include <stdio.h>
#include <stdint.h>
#include <sys/mman.h>

extern struct gddr6_ctx ctx;

int main(void)
{
    gddr6_init();
    if (gddr6_detect_compatible_gpus() == 0)
        return 1;
    gddr6_memory_map();

    for (int i = 0; i < ctx.num_devices; i++)
    {
        struct device *d = &ctx.devices[i];
        if (d->mapped_addr == NULL || d->mapped_addr == MAP_FAILED)
            continue;
        if (d->decode != DECODE_ADA) // Ampere/Ada only; GDDR7 needs module scan
            continue;
        void *virt = (uint8_t *) d->mapped_addr + (d->phys_addr - d->base_offset);
        uint32_t raw = *((uint32_t *) virt);
        printf("%02x:%02x.%x %d\n", d->bus, d->dev, d->func, (raw & 0xfff) / 32);
    }
    gddr6_cleanup(0);
    return 0;
}
