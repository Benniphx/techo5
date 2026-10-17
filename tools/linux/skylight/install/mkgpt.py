#!/usr/bin/env python3
"""Make the Skylight's new GPT from its backed-up one: userdata (entry 14) becomes techo5-store
(2 GiB) and techo5-data takes the rest of the same range as a new entry 15. Nothing else changes.

  mkgpt.py first-32MiB.img total-sectors out-primary.bin out-backup.bin
out-primary: LBA 1..33 (header + entries); out-backup: the last 33 LBAs (entries + header)."""
import struct, sys, uuid, zlib

src, total, outp, outb = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
d = open(src, "rb").read()
S = 512
hdr = bytearray(d[S:2 * S])
assert hdr[:8] == b"EFI PART"
(rev, hsz, hcrc, _r, cur, bak, first, last) = struct.unpack_from("<IIIIQQQQ", hdr, 8)
guid = hdr[56:72]
(elba, n, esz, ecrc) = struct.unpack_from("<QIII", hdr, 72)
assert hsz == 92 and esz == 128 and elba == 2, (hsz, esz, elba)
ents = bytearray(d[elba * S: elba * S + n * esz])
assert zlib.crc32(ents) == ecrc, "entries CRC mismatch in the backup"
h0 = bytearray(hdr[:hsz]); struct.pack_into("<I", h0, 16, 0)
assert zlib.crc32(h0) == hcrc, "header CRC mismatch in the backup"
assert bak == total - 1, ("backup LBA", bak, total - 1)

def name(e): return e[56:128].decode("utf-16le").rstrip("\0")
used = [i for i in range(n) if ents[i * esz:(i * esz) + 16] != bytes(16)]
for i in used:
    e = ents[i * esz:(i + 1) * esz]
    print(i + 1, name(e), struct.unpack_from("<QQ", e, 32))
ud = [i for i in used if name(ents[i * esz:(i + 1) * esz]) == "userdata"]
assert len(ud) == 1 and ud[0] == 13 and 14 not in used, ("userdata entry", ud, used)
i = 13
e = bytearray(ents[i * esz:(i + 1) * esz])
fl, ll = struct.unpack_from("<QQ", e, 32)
assert (fl, ll) == (6967296, 30535615), (fl, ll)
store_end = fl + 4194304 - 1                     # 2 GiB
linux = uuid.UUID("0fc63daf-8483-4772-8e79-3d69d8477de4").bytes_le
def entry(first_lba, last_lba, label, keep_unique=None):
    x = bytearray(esz)
    x[0:16] = linux
    x[16:32] = keep_unique or uuid.uuid4().bytes_le
    struct.pack_into("<QQQ", x, 32, first_lba, last_lba, 0)
    x[56:56 + 72] = label.encode("utf-16le").ljust(72, b"\0")
    return x
ents[13 * esz:14 * esz] = entry(fl, store_end, "techo5-store", bytes(e[16:32]))
ents[14 * esz:15 * esz] = entry(store_end + 1, ll, "techo5-data")
ecrc2 = zlib.crc32(ents)

def header(cur_lba, alt_lba, ent_lba):
    h = bytearray(hdr[:hsz])
    struct.pack_into("<QQ", h, 24, cur_lba, alt_lba)
    struct.pack_into("<Q", h, 72, ent_lba)
    struct.pack_into("<I", h, 88, ecrc2)
    struct.pack_into("<I", h, 16, 0)
    struct.pack_into("<I", h, 16, zlib.crc32(h))
    return bytes(h).ljust(S, b"\0")
nent_sectors = n * esz // S
prim = header(1, total - 1, 2) + bytes(ents)
back = bytes(ents) + header(total - 1, 1, total - 1 - nent_sectors)
assert len(prim) == (1 + nent_sectors) * S and len(back) == (nent_sectors + 1) * S
open(outp, "wb").write(prim); open(outb, "wb").write(back)
print("new: 14 techo5-store", fl, store_end, "| 15 techo5-data", store_end + 1, ll)
print("entries", n, "sectors", nent_sectors, "primary", len(prim), "backup", len(back))
