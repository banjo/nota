// Just what the tests use of Node, which has no types installed here.
declare module "node:fs" {
  export function readFileSync(path: URL | string, encoding: "utf8"): string;
}
