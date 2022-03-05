build:
	go build github.com/om26er/nexlinks/links/router

run:
	go run github.com/om26er/nexlinks/links/router

run_crossbar:
	cd crossbar; crossbar start

clean:
	rm -f router
