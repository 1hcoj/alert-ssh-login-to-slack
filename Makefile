.PHONY: generate build test clean

generate:
	go generate ./...

build: generate
	go build -o bin/ssh-login-alert .

test: generate
	go test ./...

clean:
	rm -rf bin bpf_bpfel.go bpf_bpfel.o bpf_bpfeb.go bpf_bpfeb.o
