package main

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -go-package main bpf bpf/sshlogin.bpf.c -- -I/usr/include -O2 -g
