package main

import (
	"fmt"
	"log"
	"os"

	"github.com/grandcat/zeroconf"
)

func PublishName(realm string, serviceName string) *zeroconf.Server {
	hostname, _ := os.Hostname()

	server, err := zeroconf.Register(hostname, fmt.Sprintf("_%s._tcp", serviceName), "local.", 8080,
		[]string{fmt.Sprintf("realm=%s", realm)}, nil)

	if err != nil {
		log.Fatal(err)
	}

	return server
}
