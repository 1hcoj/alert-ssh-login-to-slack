# eBPF SSH Login Alert to Slack

[한국어 문서](./README.ko.md)

A Linux agent that detects successful OpenSSH session starts with eBPF and sends an alert to Slack using the Slack Web API `chat.postMessage` method.

## How detection works

The eBPF program attaches to the `syscalls:sys_enter_execve` tracepoint. When an authenticated `sshd` process starts a user's shell or remote command, it:

1. checks that the current process name is `sshd`,
2. reads OpenSSH's `SSH_CONNECTION=<client-ip> <client-port> <server-ip> <server-port>` environment variable,
3. sends PID, UID, and connection metadata to Go through a ring buffer,
4. resolves the UID to a local account name and sends the alert to Slack.

This avoids depending on OpenSSH internal symbols, which are often stripped in distribution packages.

## Limitations

- The alert is emitted when a shell or remote command starts after authentication, not when a TCP connection is opened.
- Failed SSH authentication attempts are not reported.
- Sessions handled fully inside `sshd`, such as some `internal-sftp` configurations, may not trigger an `execve` event.
- The program scans the first 64 environment variables for `SSH_CONNECTION`. You can adjust `MAX_ENV_VARS` in `bpf/sshlogin.bpf.c` if needed.
- The event timestamp is generated in Go with the host's current time when the event is received.

If you need full authentication auditing, including all login types and failures, consider pairing this agent with Linux Audit records such as `USER_AUTH` and `USER_LOGIN`.

## Privacy notice

When deployed, this agent sends SSH login metadata to Slack:

- source IP address and port,
- target server IP and port,
- local account name and UID,
- host name,
- login timestamp,
- `sshd` PID.

Do not enable this without considering your organization's privacy, security, and retention requirements. Keep Slack tokens out of source control.

## Requirements

- Linux 5.8 or newer recommended
- Go 1.24 or newer
- clang/llvm
- libbpf development headers (`libbpf-dev`)
- root, or capabilities required for eBPF loading and perf events

Ubuntu:

```bash
sudo apt-get install -y clang llvm libbpf-dev make
```

## Build

```bash
make build
```

`go generate` compiles the eBPF C program and embeds the generated object into Go code through `bpf2go`.

## Slack `chat.postMessage` setup

1. Create an app at [Slack API Apps](https://api.slack.com/apps).
2. Add the `chat:write` Bot Token Scope under **OAuth & Permissions**.
3. Install or reinstall the app to your workspace.
4. Copy the Bot User OAuth Token (`xoxb-...`).
5. Invite the app to the destination channel: `/invite @your-app-name`.
6. Copy the Channel ID (`C...`).

Run:

```bash
export SLACK_BOT_TOKEN='xoxb-...'
export SLACK_CHANNEL_ID='C0123456789'
sudo --preserve-env=SLACK_BOT_TOKEN,SLACK_CHANNEL_ID ./bin/ssh-login-alert
```

Dry run without sending Slack messages:

```bash
sudo ./bin/ssh-login-alert -dry-run
```

Use a specific timezone:

```bash
sudo --preserve-env=SLACK_BOT_TOKEN,SLACK_CHANNEL_ID \
  ./bin/ssh-login-alert -timezone Asia/Seoul
```

## systemd service

`ssh-login-alert.service` is provided for production-style deployment. It is useful because it:

- starts the agent automatically after boot,
- restarts it on failure,
- loads Slack credentials from `/etc/ssh-login-alert.env`,
- grants the capabilities needed to load and attach eBPF programs.

Install:

```bash
sudo install -m 0755 bin/ssh-login-alert /usr/local/sbin/ssh-login-alert
sudo install -m 0644 ssh-login-alert.service /etc/systemd/system/
sudo sh -c 'printf "%s\n" "SLACK_BOT_TOKEN=xoxb-..." "SLACK_CHANNEL_ID=C0123456789" > /etc/ssh-login-alert.env'
sudo chmod 0600 /etc/ssh-login-alert.env
sudo systemctl daemon-reload
sudo systemctl enable --now ssh-login-alert
sudo journalctl -u ssh-login-alert -f
```

Some older kernels or distributions may still require running the service as root even when capabilities are configured.

## Project structure

```text
.
├── bpf/
│   └── sshlogin.bpf.c        # eBPF program attached to execve tracepoint
├── bpf_bpfeb.go              # Generated bpf2go binding for big-endian targets
├── bpf_bpfel.go              # Generated bpf2go binding for little-endian targets
├── generate.go               # go:generate entrypoint for bpf2go
├── main.go                   # Agent lifecycle, eBPF attach, ring buffer handling
├── slack.go                  # Slack chat.postMessage client
├── *_test.go                 # Unit tests
├── ssh-login-alert.service   # systemd unit for server deployment
├── Makefile                  # generate/build/test helpers
├── LICENSE                   # Repository license
├── README.md                 # English documentation
└── README.ko.md              # Korean documentation
```

## License

This repository is licensed under the MIT License. See [LICENSE](./LICENSE).

The eBPF C source also declares `Dual MIT/GPL` at load time so the kernel can treat the program as GPL-compatible when needed for eBPF helper access.
