# gofi_client
GoFi File Sync Client

## install
* `cd $GOPATH`
* `go install github.com/dvwallin/gofi_client`

## usage
* `cd <path-you-wanna-crawl-for-files`
* `gofi_server -target_url 10.0.0.10:1985 //give the ip of the machine you have the gofi_server running on`

## about
Just an UDP client that crawls through the dir you're standing in recursively and sends the file-info (name, path, size, if its a directory) and machine-data (ip, hostname) to the gofi_server.
It's a form of file-indexer for remote machines.

## links
GoFi server: https://github.com/dvwallin/gofi_server
