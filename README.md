# eBPF SSH Login Alert to Slack

`cilium/ebpf`로 OpenSSH 세션 시작을 감지하고 Slack Web API의 `chat.postMessage`로 알림을 보내는 Linux용 에이전트입니다.

## 탐지 방식

eBPF 프로그램은 `syscalls:sys_enter_execve` tracepoint에 연결됩니다. 인증을 마친 `sshd`가 로그인 사용자의 셸 또는 원격 명령을 실행할 때:

1. 실행 전 프로세스 이름이 `sshd`인지 확인합니다.
2. OpenSSH가 생성한 `SSH_CONNECTION=<client-ip> <client-port> <server-ip> <server-port>` 환경 변수를 읽습니다.
3. PID, 현재 UID, 접속 정보를 ring buffer로 Go 프로그램에 전달합니다.
4. Go 프로그램이 UID를 로컬 계정명으로 변환하고 기기의 현재 Datetime을 붙여 Slack으로 전송합니다.

이 방식은 배포판에서 `sshd` 내부 심볼이 제거되어도 동작하며 OpenSSH 내부 구조체 오프셋에 의존하지 않습니다.

### 의미와 제한

- 알림 시점은 TCP 연결이나 인증 시도 시점이 아니라 **인증 성공 후 세션의 셸/명령이 시작되는 시점**입니다.
- 잘못된 비밀번호 같은 인증 실패는 알리지 않습니다.
- `internal-sftp`처럼 별도 `execve` 없이 sshd 내부에서 처리되는 세션은 이 구현으로 탐지되지 않을 수 있습니다.
- `SSH_CONNECTION`이 처음 64개 환경 변수 안에 있어야 합니다. 일반적인 OpenSSH 환경에서는 충분하지만 `bpf/sshlogin.bpf.c`의 `MAX_ENV_VARS`로 조정할 수 있습니다.
- 이벤트 시간은 eBPF 이벤트를 받은 즉시 Go에서 `time.Now()`로 기록합니다. `-timezone`을 생략하면 서버의 로컬 timezone을 사용합니다.

모든 인증 성공 유형 및 SFTP까지 감사해야 한다면 Linux Audit의 `USER_AUTH`/`USER_LOGIN` 레코드를 병행하는 구성이 더 적합합니다. 네트워크 패킷만 보는 XDP/TC 프로그램은 암호화된 SSH 안의 인증 결과와 계정명을 판별할 수 없습니다.

## 요구 사항

- Linux 5.8 이상 권장
- Go 1.24 이상
- clang/llvm
- libbpf 개발 헤더 (`libbpf-dev`)
- root 또는 `CAP_BPF`, `CAP_PERFMON`, `CAP_SYS_RESOURCE`

Ubuntu:

```bash
sudo apt-get install -y clang llvm libbpf-dev make
```

## 빌드

```bash
make build
```

`go generate`가 C 코드를 eBPF ELF로 컴파일하고 생성된 Go 코드는 바이너리에 내장합니다.

## Slack `chat.postMessage` 설정

1. [Slack API Apps](https://api.slack.com/apps)에서 앱을 생성합니다.
2. **OAuth & Permissions → Bot Token Scopes**에 `chat:write`를 추가합니다.
3. **Install to Workspace** 또는 **Reinstall to Workspace**를 실행합니다.
4. 발급된 **Bot User OAuth Token**(`xoxb-...`)을 복사합니다.
5. 알림 채널에서 앱을 초대합니다: `/invite @앱이름`
6. 채널 정보에서 Channel ID(`C...`)를 복사합니다.

Bot Token은 비밀값입니다. 저장소나 이미지에 포함하지 마세요.

```bash
export SLACK_BOT_TOKEN='xoxb-...'
export SLACK_CHANNEL_ID='C0123456789'
sudo --preserve-env=SLACK_BOT_TOKEN,SLACK_CHANNEL_ID ./bin/ssh-login-alert
```

먼저 Slack 전송 없이 이벤트를 확인할 수도 있습니다.

```bash
sudo ./bin/ssh-login-alert -dry-run
```

한국 시간으로 표시하려면:

```bash
sudo --preserve-env=SLACK_BOT_TOKEN,SLACK_CHANNEL_ID \
  ./bin/ssh-login-alert -timezone Asia/Seoul
```

## systemd 설치

```bash
sudo install -m 0755 bin/ssh-login-alert /usr/local/sbin/ssh-login-alert
sudo install -m 0644 ssh-login-alert.service /etc/systemd/system/
sudo sh -c 'printf "%s\n" "SLACK_BOT_TOKEN=xoxb-..." "SLACK_CHANNEL_ID=C0123456789" > /etc/ssh-login-alert.env'
sudo chmod 0600 /etc/ssh-login-alert.env
sudo systemctl daemon-reload
sudo systemctl enable --now ssh-login-alert
sudo journalctl -u ssh-login-alert -f
```

일부 구형 커널/배포판은 세분화된 capability만으로 eBPF 로딩이 되지 않을 수 있습니다. 이 경우 서비스의 capability 설정을 해당 환경에 맞게 조정하거나 root 서비스로 실행해야 합니다.

## 데이터 흐름

```text
sshd 인증 성공
    -> 사용자 권한으로 execve(shell/command)
    -> eBPF tracepoint가 UID + SSH_CONNECTION 수집
    -> ring buffer
    -> Go에서 UID -> 계정명, 현재 Datetime 변환
    -> Slack Web API chat.postMessage
```
