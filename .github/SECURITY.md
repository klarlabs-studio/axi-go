# Security Policy

## Supported Versions

axi-go is in active development. Security fixes are provided for the latest release on `main`.

| Version | Supported |
|---------|-----------|
| main    | ✅        |
| older   | ❌        |

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Instead, report privately via one of these channels:

1. **GitHub Security Advisories** (preferred): Use the [Report a vulnerability](https://github.com/klarlabs-studio/axi-go/security/advisories/new) button on the Security tab
2. **Email**: Include as much detail as possible — minimal reproduction, affected versions, impact assessment

### What to include

- A description of the vulnerability
- Steps to reproduce (minimal code example if possible)
- The affected components and versions
- Potential impact (data exposure, privilege escalation, DoS, etc.)
- Any suggested mitigation

### What to expect

- **Acknowledgment** within 3 business days
- **Initial assessment** within 7 days (severity rating, whether fix is needed)
- **Fix timeline** depends on severity:
  - Critical: patch within 7 days
  - High: patch within 30 days
  - Medium/Low: next scheduled release
- **Disclosure** coordinated with reporter; CVE requested for significant issues

## Security practices in this project

- **Zero external dependencies** — minimizes supply chain attack surface
- **GitHub Actions pinned to commit SHAs** — prevents mutable tag attacks
- **govulncheck in CI** — scans for known Go vulnerabilities on every PR
- **CodeQL analysis** — static analysis for common vulnerability patterns
- **Race detector in CI** — catches concurrency bugs that could lead to security issues
- **Dependabot** — automated dependency updates for tooling (gomod, GitHub Actions)

## Hall of fame

Reporters who responsibly disclose vulnerabilities will be credited here (with consent).
