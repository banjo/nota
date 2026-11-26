import { describe, expect, it } from "vite-plus/test";
import { crc32, zip } from "../src/zip.ts";

describe("zip", () => {
  it("computes CRC-32", () => {
    expect(crc32(new TextEncoder().encode("123456789"))).toBe(0xcbf43926);
    expect(crc32(new Uint8Array())).toBe(0);
  });

  it("writes stored entries with UTF-8 names and a central directory", async () => {
    const blob = await zip([
      {
        name: "Valv/Möte.md",
        data: new Blob(["# Möte\n"]),
        modified: new Date(2024, 4, 6, 7, 8, 10),
      },
      { name: "Valv/a.txt", data: new Blob(["a"]) },
    ]);
    const b = new Uint8Array(await blob.arrayBuffer());
    const v = new DataView(b.buffer);
    expect(v.getUint32(0, true)).toBe(0x04034b50);
    expect(v.getUint16(6, true)).toBe(0x0800);
    const nameLen = v.getUint16(26, true);
    expect(new TextDecoder().decode(b.slice(30, 30 + nameLen))).toBe("Valv/Möte.md");
    expect(
      new TextDecoder().decode(b.slice(30 + nameLen, 30 + nameLen + v.getUint32(18, true))),
    ).toBe("# Möte\n");
    // DOS time: 07:08:10, 2024-05-06.
    expect(v.getUint16(10, true)).toBe((7 << 11) | (8 << 5) | 5);
    expect(v.getUint16(12, true)).toBe(((2024 - 1980) << 9) | (5 << 5) | 6);
    const end = b.length - 22;
    expect(v.getUint32(end, true)).toBe(0x06054b50);
    expect(v.getUint16(end + 10, true)).toBe(2);
    const cd = v.getUint32(end + 16, true);
    expect(v.getUint32(cd, true)).toBe(0x02014b50);
    expect(cd + v.getUint32(end + 12, true)).toBe(end);
  });
});
