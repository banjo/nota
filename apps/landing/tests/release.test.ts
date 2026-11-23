import { describe, expect, test } from "vite-plus/test";
import { fetchRelease, formatSize, pickRelease, type GitHubRelease } from "../src/data/release.ts";

const asset = (name: string, size = 1_000_000) => ({
  name,
  size,
  browser_download_url: `https://example.com/${name}`,
});

const rel = (
  assets: GitHubRelease["assets"],
  extra: Partial<GitHubRelease> = {},
): GitHubRelease => ({
  tag_name: "v1.2.3",
  html_url: "https://github.com/banjo/nota/releases/tag/v1.2.3",
  published_at: "2026-12-01T10:00:00Z",
  assets,
  ...extra,
});

describe("pickRelease", () => {
  test("no release or a draft is not out", () => {
    expect(pickRelease(null)).toBeNull();
    expect(pickRelease(rel([asset("Nota.dmg")], { draft: true }))).toBeNull();
  });

  test("a release with only CLI archives does not count as the app", () => {
    expect(pickRelease(rel([asset("nota_linux_amd64.tar.gz")]))).toBeNull();
  });

  test("prefers a universal DMG and finds the Linux packages", () => {
    const r = pickRelease(
      rel([
        asset("Nota-x86_64.dmg"),
        asset("Nota-universal.dmg", 9_412_000),
        asset("Nota.AppImage"),
        asset("nota.deb"),
      ]),
    );
    expect(r?.version).toBe("1.2.3");
    expect(r?.mac?.name).toBe("Nota-universal.dmg");
    expect(r?.appImage?.name).toBe("Nota.AppImage");
    expect(r?.deb?.name).toBe("nota.deb");
  });

  test("finds the files the release workflow uploads", () => {
    const r = pickRelease(
      rel(
        [
          asset("nota_0.1.0_darwin_arm64.tar.gz"),
          asset("Nota-0.1.0-arm64.zip"),
          asset("Nota-0.1.0-arm64.dmg"),
          asset("Nota-0.1.0-x86_64.AppImage"),
          asset("nota_0.1.0_amd64.deb"),
          asset("nota-0.1.0-1.x86_64.rpm"),
          asset("checksums.txt"),
        ],
        { tag_name: "v0.1.0" },
      ),
    );
    expect(r?.version).toBe("0.1.0");
    expect(r?.mac?.name).toBe("Nota-0.1.0-arm64.dmg");
    expect(r?.appImage?.name).toBe("Nota-0.1.0-x86_64.AppImage");
    expect(r?.deb?.name).toBe("nota_0.1.0_amd64.deb");
  });
});

describe("formatSize", () => {
  test("says sizes the way a download button does", () => {
    expect(formatSize(9_412_000)).toBe("9.4 MB");
    expect(formatSize(1_500_000_000)).toBe("1.5 GB");
    expect(formatSize(800)).toBe("1 KB");
  });
});

describe("fetchRelease", () => {
  test("forced states make no request", async () => {
    const fail = (() => {
      throw new Error("no network in this test");
    }) as unknown as typeof fetch;
    expect(await fetchRelease({ NOTA_LANDING_RELEASE: "none" }, fail)).toBeNull();
    expect((await fetchRelease({ NOTA_LANDING_RELEASE: "fixture" }, fail))?.version).toBe("0.1.0");
  });

  test("a 404 or a network error is the not-released state", async () => {
    const notFound = (async () => new Response("", { status: 404 })) as typeof fetch;
    expect(await fetchRelease({}, notFound)).toBeNull();
    const offline = (async () => {
      throw new Error("offline");
    }) as typeof fetch;
    expect(await fetchRelease({}, offline)).toBeNull();
  });
});
