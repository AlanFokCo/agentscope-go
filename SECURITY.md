# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in agentscope-go, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email: **security@agentscope.io**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| main branch | Yes |

## Security Considerations

agentscope-go includes tools that can execute shell commands (`Bash` tool) and read/write files. When deploying agents in production:

- Use the **Permission Engine** to control tool access (prefer `Explore` or `Default` mode over `Bypass`)
- Use **Workspace sandboxing** (`DockerWorkspace` or `E2BWorkspace`) for untrusted tool execution
- Never expose the Agent Service HTTP endpoints without authentication
- Rotate API keys regularly and use `SecretStr` to prevent key leakage in logs
