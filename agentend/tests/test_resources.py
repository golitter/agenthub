from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.api.v1.resources import _linux_used_memory_kb, _parse_free_bytes


def test_parse_free_bytes_uses_mem_used_column():
    output = """\
               total        used        free      shared  buff/cache   available
Mem:    1081842589696 175667183616 7461994496 25844908032 898713411584 870148243456
Swap:      2147479552 2146516992     962560
"""

    used_bytes, total_bytes = _parse_free_bytes(output)

    assert total_bytes == 1081842589696
    assert used_bytes == 175667183616


def test_linux_used_memory_matches_free_used_formula():
    info = {
        "MemTotal": "1000000 kB",
        "MemFree": "100000 kB",
        "Buffers": "20000 kB",
        "Cached": "300000 kB",
        "SReclaimable": "50000 kB",
        "Shmem": "10000 kB",
        "MemAvailable": "700000 kB",
    }

    used_kb, total_kb = _linux_used_memory_kb(info)

    assert total_kb == 1000000
    assert used_kb == 540000
