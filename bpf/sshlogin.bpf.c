//go:build ignore

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

#define TASK_COMM_LEN 16
#define CONNECTION_LEN 128
#define MAX_ENV_VARS 64

struct trace_event_raw_sys_enter {
	__u64 unused;
	long id;
	unsigned long args[6];
};

struct login_event {
	__u64 monotonic_ns;
	__u32 pid;
	__u32 uid;
	char comm[TASK_COMM_LEN];
	char connection[CONNECTION_LEN];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 20);
} events SEC(".maps");

static __always_inline int is_sshd(const char comm[TASK_COMM_LEN])
{
	return comm[0] == 's' && comm[1] == 's' && comm[2] == 'h' &&
	       comm[3] == 'd' && comm[4] == '\0';
}

static __always_inline int is_ssh_connection(const char *value)
{
	const char prefix[] = "SSH_CONNECTION=";

#pragma unroll
	for (int i = 0; i < sizeof(prefix) - 1; i++) {
		if (value[i] != prefix[i])
			return 0;
	}
	return 1;
}

SEC("tracepoint/syscalls/sys_enter_execve")
int trace_sshd_execve(struct trace_event_raw_sys_enter *ctx)
{
	char comm[TASK_COMM_LEN];
	char value[CONNECTION_LEN];
	const char *env_value;
	const char *const *envp = (const char *const *)ctx->args[2];
	int found = 0;

	if (bpf_get_current_comm(comm, sizeof(comm)) < 0 || !is_sshd(comm))
		return 0;

	/*
	 * OpenSSH exports SSH_CONNECTION as:
	 * "<client-ip> <client-port> <server-ip> <server-port>".
	 * A fixed upper bound keeps verifier analysis predictable.
	 */
	for (int i = 0; i < MAX_ENV_VARS; i++) {
		if (bpf_probe_read_user(&env_value, sizeof(env_value), &envp[i]) < 0)
			break;
		if (!env_value)
			break;
		if (bpf_probe_read_user_str(value, sizeof(value), env_value) < 0)
			continue;
		if (is_ssh_connection(value)) {
			found = 1;
			break;
		}
	}

	if (!found)
		return 0;

	struct login_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
	if (!event)
		return 0;

	__u64 pid_tgid = bpf_get_current_pid_tgid();
	__u64 uid_gid = bpf_get_current_uid_gid();

	event->monotonic_ns = bpf_ktime_get_ns();
	event->pid = pid_tgid >> 32;
	event->uid = (__u32)uid_gid;
	__builtin_memcpy(event->comm, comm, sizeof(event->comm));
	__builtin_memcpy(event->connection, value, sizeof(event->connection));
	bpf_ringbuf_submit(event, 0);
	return 0;
}

char LICENSE[] SEC("license") = "Dual MIT/GPL";
