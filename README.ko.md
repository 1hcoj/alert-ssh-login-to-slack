# eBPF SSH Login Alert to Slack

[English README](./README.md)

`cilium/ebpf`로 OpenSSH 세션 시작을 감지하고 Slack Web API의 `chat.postMessage`로 알림을 보내는 Linux용 에이전트입니다.

## 탐지 방식

이 eBPF 프로그램은 `syscalls:sys_enter_execve` tracepoint에 연결됩니다. 인증을 마친 `sshd`가 로그인 사용자의 셸 또는 원격 명령을 실행할 때 다음 정보를 수집합니다.

1. 현재 프로세스 이름이 `sshd`인지 확인합니다.
2. OpenSSH가 생성한 `SSH_CONNECTION=<client-ip> <client-port> <server-ip> <server-port>` 환경 변수를 읽습니다.
3. PID, UID, 접속 정보를 ring buffer를 통해 Go 프로그램에 전달합니다.
4. Go 프로그램이 UID를 로컬 계정명으로 변환하고 Slack으로 알림을 전송합니다.

이 방식은 배포판의 `sshd` 바이너리에서 내부 심볼이 제거되어 있어도 동작하며, OpenSSH 내부 구조체 오프셋에 의존하지 않습니다.

## 제한 사항

- 알림 시점은 TCP 연결 시점이 아니라 인증 성공 후 셸 또는 원격 명령이 시작되는 시점입니다.
- SSH 인증 실패는 알리지 않습니다.
- `internal-sftp`처럼 `sshd` 내부에서 모두 처리되는 세션은 `execve` 이벤트가 없어 탐지되지 않을 수 있습니다.
- `SSH_CONNECTION`은 처음 64개 환경 변수 안에서 탐색합니다. 필요하면 `bpf/sshlogin.bpf.c`의 `MAX_ENV_VARS` 값을 조정할 수 있습니다.
- 이벤트 시간은 Go 프로그램이 이벤트를 받은 시점의 호스트 현재 시간으로 기록합니다.

모든 로그인 유형과 인증 실패까지 감사해야 한다면 Linux Audit의 `USER_AUTH`, `USER_LOGIN` 레코드를 함께 사용하는 구성이 더 적합합니다.

## 개인정보 고지

이 에이전트는 실제 운영 중 Slack으로 다음 SSH 로그인 메타데이터를 전송합니다.

- 접속 출발지 IP와 포트
- 대상 서버 IP와 포트
- 로컬 계정명과 UID
- 호스트명
- 로그인 시각
- `sshd` PID

조직의 개인정보, 보안, 로그 보관 정책을 검토한 뒤 사용하세요. Slack Bot Token은 소스 코드나 공개 저장소에 포함하면 안 됩니다.

## 요구 사항

- Linux 5.8 이상 권장
- Go 1.24 이상
- clang/llvm
- libbpf 개발 헤더 (`libbpf-dev`)
- root 또는 eBPF 로딩과 perf event에 필요한 capability

Ubuntu:

```bash
sudo apt-get install -y clang llvm libbpf-dev make
```

## 빌드

```bash
make build
```

`go generate`가 eBPF C 프로그램을 컴파일하고, `bpf2go`로 생성된 객체를 Go 코드에 내장합니다.

## Slack `chat.postMessage` 설정

1. [Slack API Apps](https://api.slack.com/apps)에서 앱을 생성합니다.
2. **OAuth & Permissions**의 Bot Token Scope에 `chat:write`를 추가합니다.
3. 앱을 워크스페이스에 설치하거나 재설치합니다.
4. Bot User OAuth Token(`xoxb-...`)을 복사합니다.
5. 알림을 받을 채널에 앱을 초대합니다: `/invite @앱이름`
6. Channel ID(`C...`)를 복사합니다.

실행:

```bash
export SLACK_BOT_TOKEN='xoxb-...'
export SLACK_CHANNEL_ID='C0123456789'
sudo --preserve-env=SLACK_BOT_TOKEN,SLACK_CHANNEL_ID ./bin/ssh-login-alert
```

Slack 전송 없이 이벤트만 확인하려면:

```bash
sudo ./bin/ssh-login-alert -dry-run
```

시간대를 지정하려면:

```bash
sudo --preserve-env=SLACK_BOT_TOKEN,SLACK_CHANNEL_ID \
  ./bin/ssh-login-alert -timezone Asia/Seoul
```

## systemd 서비스

`ssh-login-alert.service`는 서버 운영 환경에서 에이전트를 안정적으로 실행하기 위한 단위 파일입니다. 필요한 이유는 다음과 같습니다.

- 서버 부팅 후 자동으로 에이전트를 시작합니다.
- 에이전트가 비정상 종료되면 자동으로 재시작합니다.
- `/etc/ssh-login-alert.env`에서 Slack 인증 정보를 읽습니다.
- eBPF 프로그램을 로드하고 tracepoint에 붙이는 데 필요한 capability를 부여합니다.

설치:

```bash
sudo install -m 0755 bin/ssh-login-alert /usr/local/sbin/ssh-login-alert
sudo install -m 0644 ssh-login-alert.service /etc/systemd/system/
sudo sh -c 'printf "%s\n" "SLACK_BOT_TOKEN=xoxb-..." "SLACK_CHANNEL_ID=C0123456789" > /etc/ssh-login-alert.env'
sudo chmod 0600 /etc/ssh-login-alert.env
sudo systemctl daemon-reload
sudo systemctl enable --now ssh-login-alert
sudo journalctl -u ssh-login-alert -f
```

일부 구형 커널이나 배포판에서는 capability를 설정해도 root 서비스로 실행해야 할 수 있습니다.

## 프로젝트 구조

```text
.
├── bpf/
│   └── sshlogin.bpf.c        # execve tracepoint에 붙는 eBPF 프로그램
├── bpf_bpfeb.go              # big-endian 대상용 bpf2go 생성 바인딩
├── bpf_bpfel.go              # little-endian 대상용 bpf2go 생성 바인딩
├── generate.go               # bpf2go 실행용 go:generate 진입점
├── main.go                   # 에이전트 생명주기, eBPF attach, ring buffer 처리
├── slack.go                  # Slack chat.postMessage 클라이언트
├── *_test.go                 # 단위 테스트
├── ssh-login-alert.service   # 서버 배포용 systemd unit
├── Makefile                  # generate/build/test 헬퍼
├── LICENSE                   # 저장소 라이선스
├── README.md                 # 영어 문서, 메인 README
└── README.ko.md              # 한국어 문서
```

## 라이선스

이 저장소는 MIT License로 공개하는 구성을 추천하며, 현재 [LICENSE](./LICENSE)에 MIT License를 추가했습니다.

단, eBPF C 소스는 커널이 GPL 호환 프로그램으로 인식할 수 있도록 로딩 시점 license string을 `Dual MIT/GPL`로 선언합니다. 이는 eBPF helper 접근 호환성을 위한 선언입니다.
