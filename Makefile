.PHONY: build run clean

build:
	go build -o local-smtp-forwarder .

run:
	go run .

clean:
	rm -f local-smtp-forwarder local-smtp-forwarder-linux-*
