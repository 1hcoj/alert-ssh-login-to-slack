//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

type rawLoginEvent struct {
	MonotonicNs uint64
	Pid         uint32
	Uid         uint32
	Comm        [16]int8
	Connection  [128]int8
}

type login struct {
	Username   string
	UID        uint32
	PID        uint32
	ClientIP   string
	ClientPort string
	ServerIP   string
	ServerPort string
	Hostname   string
	OccurredAt time.Time
}

func main() {
	var (
		slackToken   = flag.String("slack-token", os.Getenv("SLACK_BOT_TOKEN"), "Slack Bot User OAuth Token")
		slackChannel = flag.String("slack-channel", os.Getenv("SLACK_CHANNEL_ID"), "Slack destination channel ID")
		timezone     = flag.String("timezone", os.Getenv("TZ"), "IANA timezone used in alerts (default: system local timezone)")
		dryRun       = flag.Bool("dry-run", false, "print alerts without sending them to Slack")
	)
	flag.Parse()

	if !*dryRun && (*slackToken == "" || *slackChannel == "") {
		log.Fatal("SLACK_BOT_TOKEN and SLACK_CHANNEL_ID are required (unless -dry-run is used)")
	}

	location, err := loadLocation(*timezone)
	if err != nil {
		log.Fatalf("load timezone: %v", err)
	}

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("remove memlock limit: %v", err)
	}

	var objects bpfObjects
	if err := loadBpfObjects(&objects, nil); err != nil {
		log.Fatalf("load eBPF objects: %v", err)
	}
	defer objects.Close()

	tracepoint, err := link.Tracepoint("syscalls", "sys_enter_execve", objects.TraceSshdExecve, nil)
	if err != nil {
		log.Fatalf("attach execve tracepoint: %v", err)
	}
	defer tracepoint.Close()

	reader, err := ringbuf.NewReader(objects.Events)
	if err != nil {
		log.Fatalf("open ring buffer: %v", err)
	}
	defer reader.Close()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = reader.Close()
	}()

	notifier := newSlackNotifier(*slackToken, *slackChannel)
	log.Printf("watching successful SSH session starts on %s", hostname)

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			log.Printf("read eBPF event: %v", err)
			continue
		}

		var event rawLoginEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.NativeEndian, &event); err != nil {
			log.Printf("decode eBPF event: %v", err)
			continue
		}

		item, err := eventToLogin(event, hostname, time.Now().In(location))
		if err != nil {
			log.Printf("discard malformed SSH event: %v", err)
			continue
		}

		if *dryRun {
			log.Printf("SSH login user=%s uid=%d source=%s:%s at=%s",
				item.Username, item.UID, item.ClientIP, item.ClientPort, item.OccurredAt.Format(time.RFC3339))
			continue
		}

		if err := notifier.Notify(ctx, item); err != nil {
			log.Printf("send Slack alert: %v", err)
		}
	}
}

func eventToLogin(event rawLoginEvent, hostname string, occurredAt time.Time) (login, error) {
	connection := cString(event.Connection[:])
	const prefix = "SSH_CONNECTION="
	if !strings.HasPrefix(connection, prefix) {
		return login{}, fmt.Errorf("SSH_CONNECTION is missing")
	}

	fields := strings.Fields(strings.TrimPrefix(connection, prefix))
	if len(fields) != 4 {
		return login{}, fmt.Errorf("unexpected SSH_CONNECTION value %q", connection)
	}
	if net.ParseIP(fields[0]) == nil || net.ParseIP(fields[2]) == nil {
		return login{}, fmt.Errorf("invalid IP address in %q", connection)
	}

	return login{
		Username:   usernameForUID(event.Uid),
		UID:        event.Uid,
		PID:        event.Pid,
		ClientIP:   fields[0],
		ClientPort: fields[1],
		ServerIP:   fields[2],
		ServerPort: fields[3],
		Hostname:   hostname,
		OccurredAt: occurredAt,
	}, nil
}

func usernameForUID(uid uint32) string {
	value := strconv.FormatUint(uint64(uid), 10)
	account, err := user.LookupId(value)
	if err != nil {
		return "uid:" + value
	}
	return account.Username
}

func cString(value []int8) string {
	data := make([]byte, 0, len(value))
	for _, character := range value {
		if character == 0 {
			break
		}
		data = append(data, byte(character))
	}
	return string(data)
}

func loadLocation(name string) (*time.Location, error) {
	if name == "" {
		return time.Local, nil
	}
	return time.LoadLocation(name)
}
