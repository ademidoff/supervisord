build-linux:
	go generate
	go build -tags release -a -ldflags "-linkmode external -extldflags -static" -o supervisord

build-darwin:
	go generate
	CGO_ENABLED=0 go build -tags release -a -ldflags "-extldflags -static -s" -o supervisord
