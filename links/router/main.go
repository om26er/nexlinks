package main

import (
    "flag"
    "io"
    "log"
    "os"
    "os/signal"
    "time"

    "github.com/gammazero/nexus/v3/router"
    "github.com/gammazero/nexus/v3/wamp"
)

const (
    metaProcRegList = string(wamp.MetaProcRegList)
    metaProcRegGet = string(wamp.MetaProcRegGet)
    metaEventRegOnCreate = string(wamp.MetaEventRegOnCreate)
    metaEventRegOnDelete = string(wamp.MetaEventRegOnDelete)

    metaProcSubList = string(wamp.MetaProcSubList)
    metaProcSubGet = string(wamp.MetaProcSubGet)
    metaEventSubOnCreate = string(wamp.MetaEventSubOnCreate)
    metaEventSubOnDelete = string(wamp.MetaEventSubOnDelete)
)

func main() {
    var (
        realm = "realm1"
        netAddr  = "localhost"
        wsPort         = 8080

        linkPrivateKey = "1c0ecd558e88e9fc51c10e0373a582bdb11db7d14e9bd514fa2703a92b7a5617"
        linkRealm      = "realm1"
        linkRouterURL = "tcp://localhost:8081"
        linkReconnectSeconds = 5
    )

    flag.StringVar(&netAddr, "netaddr", netAddr, "network address to listen on")
    flag.IntVar(&wsPort, "ws-port", wsPort, "websocket port")
    flag.StringVar(&realm, "realm", realm, "realm name")

    flag.StringVar(&linkPrivateKey, "link-private-key", linkPrivateKey, "RLink private key")
    flag.StringVar(&linkRealm, "link-realm", linkRealm, "RLink realm")
    flag.StringVar(&linkRouterURL, "link-url", linkRealm, "RLink URL")
    flag.IntVar(&linkReconnectSeconds, "link-reconnect-interval", linkReconnectSeconds, "RLink reconnect interval")
    flag.Parse()

    // Create router instance.
    routerConfig := &router.Config{
            RealmConfigs: []*router.RealmConfig{
            {
                URI:           wamp.URI(realm),
                AnonymousAuth: true,
                AllowDisclose: true,
                MetaStrict: false,
                StrictURI: false,
            },
        },
    }

    nxr, err := router.NewRouter(routerConfig, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer nxr.Close()

    wsCloser, err := SetupWebSocketTransport(nxr, netAddr, wsPort)
    if err != nil {
        log.Fatal(err)
    }
    defer func(wsCloser io.Closer) {
        err := wsCloser.Close()
        if err != nil {
            log.Println("WEBSOCKET TRANSPORT CLOSED")
        }
    }(wsCloser)
    log.Printf("Websocket server listening on ws://%s:%d/ws", netAddr, wsPort)

    // FIXME: make service discovery configurable
    mdns, _ := PublishName(realm, "st")
    defer mdns.Shutdown()

    // PUBKEY IS 81deeb0a11c4f3919e6c35adc1980516dfd8ca84e01929b51070d6a7d3e6c012
    cfg := ConstructLinkConfig(linkPrivateKey, linkRealm)
    go ConnectRemoteLeg(linkRouterURL, &cfg, &nxr, time.Duration(linkReconnectSeconds))

    // Wait for SIGINT (CTRL-c), then close servers and exit.
    shutdown := make(chan os.Signal, 1)
    signal.Notify(shutdown, os.Interrupt)
    <-shutdown
}

