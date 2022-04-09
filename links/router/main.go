package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/gammazero/nexus/v3/router/auth"
	"github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"time"

	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/wamp"
)

const (
	metaProcRegList      = string(wamp.MetaProcRegList)
	metaProcRegGet       = string(wamp.MetaProcRegGet)
	metaEventRegOnCreate = string(wamp.MetaEventRegOnCreate)
	metaEventRegOnDelete = string(wamp.MetaEventRegOnDelete)

	metaProcSubList      = string(wamp.MetaProcSubList)
	metaProcSubGet       = string(wamp.MetaProcSubGet)
	metaEventSubOnCreate = string(wamp.MetaEventSubOnCreate)
	metaEventSubOnDelete = string(wamp.MetaEventSubOnDelete)
)

func main() {
	var (
		realm   = "realm1"
		netAddr = "localhost"
		wsPort  = 8080

		linkPrivateKey       = "1c0ecd558e88e9fc51c10e0373a582bdb11db7d14e9bd514fa2703a92b7a5617"
		linkRealm            = "realm1"
		linkRouterURL        = "ws://localhost:8081/ws"
		linkReconnectSeconds = 5
	)

	flag.StringVar(&netAddr, "netaddr", netAddr, "network address to listen on")
	flag.IntVar(&wsPort, "ws-port", wsPort, "websocket port")
	flag.StringVar(&realm, "realm", realm, "realm name")

	flag.StringVar(&linkPrivateKey, "link-private-key", linkPrivateKey, "RLink private key")
	flag.StringVar(&linkRealm, "link-realm", linkRealm, "RLink realm")
	flag.StringVar(&linkRouterURL, "link-url", linkRouterURL, "RLink URL")
	flag.IntVar(&linkReconnectSeconds, "link-reconnect-interval", linkReconnectSeconds, "RLink reconnect interval")
	flag.Parse()

	logger := logrus.New()

	var tks = &keyStore{
		provider:  "static",
		publicKey: "f1c01c480112705361beb5e4eda8544f951abb7ca918f76327a3a5240f352292",
	}

	cryptosign := auth.NewCryptoSignAuthenticator(tks, 10 * time.Second)
	anonymous := auth.AnonymousAuth{ AuthRole: "anonymous" }

	// Create router instance.
	routerConfig := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:           wamp.URI(realm),
				AnonymousAuth: true,
				AllowDisclose: true,
				MetaStrict:    true,
				Authenticators: []auth.Authenticator{cryptosign, &anonymous},
			},
		},
	}

	nxr, err := router.NewRouter(routerConfig, nil)
	if err == nil {
		defer nxr.Close()
	} else {
		logger.Fatal(err)
	}

	// Setup listening websocket transport
	wsCloser, err := SetupWebSocketTransport(nxr, netAddr, wsPort)
	if err == nil {
		logger.Infoln(fmt.Sprintf("Websocket server listening on ws://%s:%d/ws", netAddr, wsPort))
		defer wsCloser.Close()
	} else {
		logger.Fatal(err)
	}

	// Setup listening unix socket transport
	sockPath := "/tmp/nexus.sock"
	udsCloser, err := SetupUNIXSocketTransport(&nxr, sockPath)
	if err == nil {
		logger.Infoln(fmt.Sprintf("UDS listening on %s", sockPath))
		defer udsCloser.Close()
	} else {
		logger.Fatal(err)
	}

	// FIXME: make service discovery configurable
	mdns, _ := PublishName(realm, "st")
	defer mdns.Shutdown()

	// PUBKEY IS 81deeb0a11c4f3919e6c35adc1980516dfd8ca84e01929b51070d6a7d3e6c012
	cfg := ConstructLinkConfig(linkPrivateKey, linkRealm)
	// Run this function in a goroutine. The code internally makes sure to keep
	// a connection to the remote leg of the WAMP router, which means in case
	// the connection is lost, it reconnects.
	go ConnectRemoteLeg(linkRouterURL, &cfg, &nxr, time.Duration(linkReconnectSeconds))

	// Wait for SIGINT (CTRL-c), then close servers and exit.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt)
	<-shutdown
}

type keyStore struct {
	provider  string
	publicKey string
}

func (ks *keyStore) AuthKey(authid, authmethod string) ([]byte, error) {
	return hex.DecodeString(ks.publicKey)
}

func (ks *keyStore) AuthRole(authid string) (string, error) { return "user", nil }

// PasswordInfo Not used when using WAMP cryptosign authentication
func (ks *keyStore) PasswordInfo(authid string) (string, int, int) { return "", 0, 0 }

func (ks *keyStore) Provider() string { return ks.provider }
