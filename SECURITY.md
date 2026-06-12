# Security Policy

## Supported versions

Only the latest released version of Loba receives security fixes. Always update
to the newest release (`loba update`, or re-run `play.sh` for the dev channel)
before reporting an issue.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately through GitHub's
[Security Advisories](https://github.com/zwenger/TUI-LOBA/security/advisories/new)
("Report a vulnerability"). If that is unavailable, contact the maintainer
directly rather than posting publicly.

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce (a proof of concept if possible).
- The affected version (`loba` prints its version on launch).

You can expect an acknowledgement within a few days. Once a fix is released,
we're happy to credit you in the advisory unless you prefer to stay anonymous.

## Threat model notes

Loba is **server-authoritative**: all game logic runs on the host and clients
are untrusted renderers. When a game is hosted with `--public`, the host process
is reachable from the internet through the public tunnel — treat any connecting
peer as potentially hostile. Reports about denial-of-service, impersonation, or
input handling on that surface are especially welcome.
