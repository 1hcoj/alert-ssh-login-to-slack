# eBPF SSH Login Alert to Slack

[English README](./README.md)

`cilium/ebpf`로 OpenSSH 세션 시작을 감지하고 Slack Web API의 `chat.postMessage`로 알림을 보내는 Linux용 에이전트입니다.

## 탐지 방식

이 eBPF 프로그램은 `syscalls:sys_enter_execve` tracepoint에 연결됩니다. 인증을 마친 `sshd`가 로그인 사용자의 셸 또는 원격 명령을 실행할 때 다음 정보를 수집합니다.

1. 현재 프로세스 이름이 `sshd`인지 확인합니다.
2. OpenSSH가 생성한 `SSH_CONNECTION=<client-ip> <client-port> <server-ip> <server-port>` 환경 변수를 읽습니다.
3. PID, UID, 접속 정보를 ring buffer를 통해 Go 프로그램에 전달합니다.
4. Go 프로그램이 UID를 로컬 계정명으로 변환하고 Slack으로 알림을 전송합니다.

![slack-capture](slack-capture.png)

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
- Docker
- Docker 없이 로컬 빌드할 경우 Go 1.24 이상, clang/llvm, libbpf 개발 헤더
- root, privileged 컨테이너 실행, 또는 eBPF에 필요한 capability와 커널 파일시스템 마운트

## 로컬 빌드

```bash
make build
```

`go generate`가 eBPF C 프로그램을 컴파일하고, `bpf2go`로 생성된 객체를 Go 코드에 내장합니다.

## Docker image 생성 및 push

Docker 이미지를 생성하고 push합니다.

```bash
docker build -t 1hcoj/alert-ssh-login-to-slack:latest .
docker push 1hcoj/alert-ssh-login-to-slack:latest
```

Dockerfile은 multi-stage build를 사용합니다. builder image에서는 Go, clang/llvm, libbpf 헤더, make를 설치하고, runtime image에는 컴파일된 에이전트와 CA 인증서, timezone 데이터만 포함합니다.

## Slack `chat.postMessage` 설정

1. [Slack API Apps](https://api.slack.com/apps)에서 앱을 생성합니다.
2. **OAuth & Permissions**의 Bot Token Scope에 `chat:write`를 추가합니다.
3. 앱을 워크스페이스에 설치하거나 재설치합니다.
4. Bot User OAuth Token(`xoxb-...`)을 복사합니다.
5. 알림을 받을 채널에 앱을 초대합니다: `/invite @앱이름`
6. Channel ID(`C...`)를 복사합니다.

## Docker로 실행

Slack 전송 없이 이벤트만 확인하려면:

```bash
docker run --rm \
  --name ssh-login-alert \
  --privileged \
  --pid=host \
  -v /sys/kernel/tracing:/sys/kernel/tracing:rw \
  -v /sys/fs/bpf:/sys/fs/bpf:rw \
  -v /etc/passwd:/etc/passwd:ro \
  -v /etc/group:/etc/group:ro \
  1hcoj/alert-ssh-login-to-slack:latest \
  -dry-run
```

Slack 알림을 활성화해서 실행하려면:

```bash
docker run -d \
  --name ssh-login-alert \
  --restart unless-stopped \
  --privileged \
  --pid=host \
  -e SLACK_BOT_TOKEN='xoxb-...' \
  -e SLACK_CHANNEL_ID='C0123456789' \
  -v /sys/kernel/tracing:/sys/kernel/tracing:rw \
  -v /sys/fs/bpf:/sys/fs/bpf:rw \
  -v /etc/passwd:/etc/passwd:ro \
  -v /etc/group:/etc/group:ro \
  1hcoj/alert-ssh-login-to-slack:latest \
  -timezone Asia/Seoul
```

각 옵션이 필요한 이유는 다음과 같습니다.

- `--privileged`: 컨테이너가 eBPF 프로그램을 로드하고 attach할 수 있도록 합니다.
- `--pid=host`: 호스트 프로세스 가시성과 컨테이너 실행 환경을 맞춥니다.
- `/sys/kernel/tracing`: eBPF attach 로직이 사용하는 tracepoint 메타데이터를 노출합니다.
- `/sys/fs/bpf`: 커널/eBPF 스택에서 필요한 host BPF filesystem을 노출합니다.
- `/etc/passwd`, `/etc/group`: 호스트 UID를 호스트 계정명으로 해석하기 위해 read-only로 마운트합니다.
- `--restart unless-stopped`: 에이전트 장애 또는 호스트 재부팅 후 Docker daemon이 시작될 때 컨테이너를 다시 실행합니다.

## 프로젝트 구조

```text
.
├── bpf/
│   └── sshlogin.bpf.c        # execve tracepoint에 붙는 eBPF 프로그램
├── bpf_bpfeb.go              # big-endian 대상용 bpf2go 생성 바인딩
├── bpf_bpfel.go              # little-endian 대상용 bpf2go 생성 바인딩
├── Dockerfile                # multi-stage Docker build
├── generate.go               # bpf2go 실행용 go:generate 진입점
├── main.go                   # 에이전트 생명주기, eBPF attach, ring buffer 처리
├── slack.go                  # Slack chat.postMessage 클라이언트
├── *_test.go                 # 단위 테스트
├── Makefile                  # generate/build/test 헬퍼
├── LICENSE                   # 저장소 라이선스
├── README.md                 # 영어 문서, 메인 README
└── README.ko.md              # 한국어 문서
```

## 라이선스

이 저장소는 MIT License로 공개하는 구성을 추천하며, 현재 [LICENSE](./LICENSE)에 MIT License를 추가했습니다.

단, eBPF C 소스는 커널이 GPL 호환 프로그램으로 인식할 수 있도록 로딩 시점 license string을 `Dual MIT/GPL`로 선언합니다. 이는 eBPF helper 접근 호환성을 위한 선언입니다.
