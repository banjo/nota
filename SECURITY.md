# Security

Please do not report security vulnerabilities in public issues.

Report them privately through GitHub's
[private vulnerability reporting](https://github.com/banjo/nota/security/advisories/new), or by
e-mail to anton.odman@gmail.com. Include what is affected (CLI, desktop app, web app or
`nota serve`), the version or commit, and the steps to reproduce.

Fixes go into the latest version; there are no supported
older releases yet.

The sync server is the part that faces the network. Its trust model, in short: notes are stored
unencrypted on the server (there is no end-to-end encryption), accounts use passkeys and device
tokens, and server hooks (`NOTA_SERVER_HOOKS`, off by default) run shell commands from synced
vaults with the server's permissions. See [docs/deploy.md](docs/deploy.md).
