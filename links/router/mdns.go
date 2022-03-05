package main

import (
	"fmt"
	"os"

	"github.com/grandcat/zeroconf"
)

func PublishName(realm string, serviceName string) (*zeroconf.Server, error) {
	hostname, _ := os.Hostname()

	return zeroconf.Register(hostname, fmt.Sprintf("_%s._tcp", serviceName), "local.", 8080,
		[]string{fmt.Sprintf("realm=%s", realm)}, nil)

}
