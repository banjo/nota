// The latest GitHub release of banjo/nota, read once at build time. The
// page is prerendered, so a new release needs a rebuild (and a deploy)
// before the download buttons show it.
//
// NOTA_LANDING_RELEASE forces a state, for screenshots and tests:
//   "none"     the app is not out yet (no request is made)
//   "fixture"  a made-up v1.0.0 with every asset
// GITHUB_TOKEN or GH_TOKEN is sent when set (the repo is private until launch).

export interface ReleaseAsset {
  name: string;
  url: string;
  size: number;
}

export interface AppRelease {
  version: string;
  url: string;
  published: string;
  mac?: ReleaseAsset;
  appImage?: ReleaseAsset;
  deb?: ReleaseAsset;
}

interface GitHubAsset {
  name: string;
  size: number;
  browser_download_url: string;
}

export interface GitHubRelease {
  tag_name: string;
  html_url: string;
  published_at: string | null;
  draft?: boolean;
  prerelease?: boolean;
  assets: GitHubAsset[];
}

const asset = (a: GitHubAsset | undefined): ReleaseAsset | undefined =>
  a && { name: a.name, url: a.browser_download_url, size: a.size };

/**
 * The app's downloads in a release, or null when it has none: a release
 * with only CLI archives does not count as the app being out.
 */
export function pickRelease(r: GitHubRelease | null | undefined): AppRelease | null {
  if (!r || r.draft) return null;
  const find = (re: RegExp) => asset(r.assets.find((a) => re.test(a.name)));
  // Prefer a universal or arm64 DMG over an Intel-only one.
  const dmgs = r.assets.filter((a) => /\.dmg$/i.test(a.name));
  const mac = asset(dmgs.find((a) => /universal|arm64|aarch64/i.test(a.name)) ?? dmgs[0]);
  const appImage = find(/\.AppImage$/i);
  const deb = find(/\.deb$/i);
  if (!mac && !appImage && !deb) return null;
  return {
    version: r.tag_name.replace(/^v/, ""),
    url: r.html_url,
    published: r.published_at ?? "",
    mac,
    appImage,
    deb,
  };
}

/** 12.3 MB, the way a download button says it. */
export function formatSize(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(bytes / 1e3))} KB`;
}

const FIXTURE: GitHubRelease = {
  tag_name: "v1.0.0",
  html_url: "https://github.com/banjo/nota/releases/tag/v1.0.0",
  published_at: "2026-12-01T10:00:00Z",
  assets: [
    {
      name: "Nota-1.0.0-universal.dmg",
      size: 9_412_000,
      browser_download_url:
        "https://github.com/banjo/nota/releases/download/v1.0.0/Nota-1.0.0-universal.dmg",
    },
    {
      name: "Nota-1.0.0-x86_64.AppImage",
      size: 14_830_000,
      browser_download_url:
        "https://github.com/banjo/nota/releases/download/v1.0.0/Nota-1.0.0-x86_64.AppImage",
    },
    {
      name: "nota_1.0.0_amd64.deb",
      size: 8_120_000,
      browser_download_url:
        "https://github.com/banjo/nota/releases/download/v1.0.0/nota_1.0.0_amd64.deb",
    },
  ],
};

/** The build's environment (Astro runs on Node; no @types/node here). */
const buildEnv: Record<string, string | undefined> =
  (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};

export async function fetchRelease(
  env: Record<string, string | undefined> = buildEnv,
  doFetch: typeof fetch = fetch,
): Promise<AppRelease | null> {
  const forced = env.NOTA_LANDING_RELEASE;
  if (forced === "none") return null;
  if (forced === "fixture") return pickRelease(FIXTURE);
  const token = env.GITHUB_TOKEN || env.GH_TOKEN;
  try {
    const res = await doFetch("https://api.github.com/repos/banjo/nota/releases/latest", {
      headers: {
        Accept: "application/vnd.github+json",
        "User-Agent": "nota-landing-build",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
    });
    // 404: no release yet (or a private repo without a token).
    if (!res.ok) return null;
    return pickRelease((await res.json()) as GitHubRelease);
  } catch {
    return null; // offline build: the honest "not out yet" state
  }
}
