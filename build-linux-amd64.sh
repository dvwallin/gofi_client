CC=/usr/local/musl/bin/musl-gcc CXX=x86_64-linux-musl-g++ GOARCH=amd64 GOOS=linux CGO_ENABLED=1 go build -o gofi_client-linux-amd64 -ldflags "-linkmode external -extldflags -static" main.go
