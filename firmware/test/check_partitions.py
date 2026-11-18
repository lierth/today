#!/usr/bin/env python3
"""Check partitions.csv against the Arduino/PlatformIO flash layout.

`pio run -t upload` writes boot_app0.bin at the offset of the OTA *data*
partition; if that is not where otadata lives, every USB flash overwrites the
start of another partition (this project shipped with that bug once). Run:

    python3 firmware/test/check_partitions.py
"""
import csv
import re
import sys
from pathlib import Path

APP_OFFSET = 0x10000  # PlatformIO's $ESP32_APP_OFFSET for Arduino builds


def parse(path):
    rows = []
    with open(path, newline="") as f:
        for raw in csv.reader(f):
            if not raw or raw[0].strip().startswith("#"):
                continue
            name, kind, subtype, offset, size = (c.strip() for c in raw[:5])
            rows.append(
                {
                    "name": name,
                    "type": kind,
                    "subtype": subtype,
                    "offset": int(offset, 16),
                    "size": int(size, 16),
                }
            )
    return rows


def main():
    firmware = Path(__file__).resolve().parent.parent
    table = firmware / "partitions.csv"
    rows = parse(table)
    failures = []

    partitions = {row["name"]: row for row in rows}
    for name in ("nvs", "otadata", "app0", "app1"):
        if name not in partitions:
            failures.append(f"missing partition {name}")

    otadata = partitions.get("otadata")
    if otadata is None:
        report(failures)
        return 1
    if otadata["type"] != "data" or otadata["subtype"] != "ota":
        failures.append(
            f"otadata must be data/ota, got {otadata['type']}/{otadata['subtype']}"
        )

    # 1. The offset PlatformIO writes boot_app0.bin to must be otadata's.
    boot_app0_offset = otadata["offset"]
    if boot_app0_offset != 0xE000:
        failures.append(
            f"otadata is at 0x{boot_app0_offset:x}; PlatformIO writes boot_app0.bin "
            f"to 0xe000, and any other offset means the NVS start is not covered"
        )

    # 2. Arduino's NVS starts right after the 0x9000 partition table.
    if "nvs" in partitions and partitions["nvs"]["offset"] != 0x9000:
        failures.append(
            f"nvs starts at 0x{partitions['nvs']['offset']:x}, expected 0x9000 "
            f"(the bootloader+partition table reserve 0x0-0x9000)"
        )

    # 3. app0 must match $ESP32_APP_OFFSET so `pio upload` and merge agree.
    if "app0" in partitions and partitions["app0"]["offset"] != APP_OFFSET:
        failures.append(
            f"app0 starts at 0x{partitions['app0']['offset']:x}, expected "
            f"0x{APP_OFFSET:x} (PlatformIO uploads firmware.bin there)"
        )

    # 4. No overlaps, in table order (partition tables are sorted by offset).
    ordered = sorted(rows, key=lambda r: r["offset"])
    for prev, cur in zip(ordered, ordered[1:]):
        if prev["offset"] + prev["size"] > cur["offset"]:
            failures.append(
                f"{prev['name']} (0x{prev['offset']:x}+0x{prev['size']:x}) overlaps "
                f"{cur['name']} at 0x{cur['offset']:x}"
            )

    # 5. The filesystem label in storage.cpp must name a partition that
    #    exists. LittleFS.begin defaults to "spiffs"; with a partition named
    #    "littlefs" the mount fails silently and the frame cache is disabled.
    storage = firmware / "src" / "storage.cpp"
    if storage.exists():
        source = storage.read_text()
        match = re.search(r"LittleFS\.begin\s*\(([^)]*)\)", source)
        if not match:
            failures.append("storage.cpp: no LittleFS.begin(...) call found")
        else:
            args = [a.strip() for a in match.group(1).split(",")]
            label = args[3].strip().strip('"') if len(args) >= 4 else "spiffs"
            if label == "spiffs" and "spiffs" not in partitions:
                failures.append(
                    "storage.cpp mounts the filesystem without a label; "
                    'LittleFS.begin defaults to "spiffs" but partitions.csv has '
                    f'{sorted(partitions)} — pass the real name, e.g. '
                    'LittleFS.begin(true, "/littlefs", 10, "littlefs")'
                )
            elif label not in partitions:
                failures.append(
                    f"storage.cpp mount label {label!r} is not a partition in "
                    f"partitions.csv {sorted(partitions)}"
                )
            else:
                part = partitions[label]
                if part["type"] != "data":
                    failures.append(
                        f"storage.cpp mount label {label!r} is {part['type']}/{part['subtype']}, "
                        "not a data partition"
                    )

    # 6. The 15 KB frame cache must not be expected in NVS: 20 KB is inline
    #    with the row table, so keep a floor and document the LittleFS move.
    if "nvs" in partitions and partitions["nvs"]["size"] < 0x4000:
        failures.append(
            f"nvs is only 0x{partitions['nvs']['size']:x} bytes; credentials plus "
            f"outbox need ~16 KB"
        )
    if "nvs" in partitions and partitions["nvs"]["size"] > 0x8000:
        failures.append(
            f"nvs is 0x{partitions['nvs']['size']:x} bytes; the 15 KB frame cache "
            f"belongs in LittleFS, not NVS"
        )

    report(failures)
    return 1 if failures else 0


def report(failures):
    if failures:
        print("partitions.csv: FAIL")
        for f in failures:
            print("  -", f)
        return
    print("partitions.csv: ok (otadata at 0xe000, no overlaps, app0 at 0x10000)")


if __name__ == "__main__":
    sys.exit(main())
