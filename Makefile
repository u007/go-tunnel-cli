
test:
	go test -v -run .
	go run main.go test.env bun test .

run:
	go run main.go test.env bun client.js

build:
	go build

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o tunnel-linux-amd64
	
build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o tunnel-windows-amd64
