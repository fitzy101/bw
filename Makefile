GOBUILDFLAGS=
GC=go build
SRC=cmd/bw.go
PROG=bw

bw: $(SRC) depend test
	$(GC) $(GOBUILDFLAGS) -o $(PROG) $(SRC)
	chmod +x $(PROG)

test:
	go test -race -coverprofile=coverage.txt -covermode=atomic ./...

depend:
	go get -t ./...

cover: test
	go tool cover -html coverage.txt

clean:
	@if [ -f $(PROG) ]; then rm $(PROG); fi
	@if [ -f coverage.txt ]; then rm coverage.txt; fi

build:
	go install .
