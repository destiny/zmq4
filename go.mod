module github.com/destiny/zmq4/v25

go 1.23.0

toolchain go1.24.5

require (
	github.com/go-zeromq/goczmq/v4 v4.2.2
	github.com/pebbe/zmq4 v1.2.11
	go.uber.org/goleak v1.3.0
	golang.org/x/crypto v0.41.0
	golang.org/x/sync v0.16.0
	golang.org/x/text v0.28.0
)

require golang.org/x/sys v0.35.0 // indirect
