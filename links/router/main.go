package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "time"

    "github.com/gammazero/nexus/v3/client"
    "github.com/gammazero/nexus/v3/router"
    "github.com/gammazero/nexus/v3/wamp"

    "github.com/grandcat/zeroconf"
)

const (
    metaOnJoin  = string(wamp.MetaEventSessionOnJoin)
)

func main() {
    var (
        realm = "realm1"
        netAddr  = "localhost"
        wsPort   = 8080
    )
    flag.StringVar(&netAddr, "netaddr", netAddr, "network address to listen on")
    flag.IntVar(&wsPort, "ws-port", wsPort, "websocket port")
    flag.StringVar(&realm, "realm", realm, "realm name")
    flag.Parse()

    // Create router instance.
    routerConfig := &router.Config{
        RealmConfigs: []*router.RealmConfig{
            &router.RealmConfig{
                URI:           wamp.URI(realm),
                AnonymousAuth: true,
                AllowDisclose: true,
            },
        },
    }
    nxr, err := router.NewRouter(routerConfig, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer nxr.Close()

    // create local (embedded) RPC callee client that provides the time in the
    // requested timezones.
    callee, err := createLocalCallee(nxr, realm)
    if err != nil {
        log.Fatal(err)
    }
    defer callee.Close()

    subscribeMetaOnJoin(nxr, realm)

    // Create websocket server.
    wss := router.NewWebsocketServer(nxr)
    // Enable websocket compression, which is used if clients request it.
    wss.Upgrader.EnableCompression = true
    // Configure server to send and look for client tracking cookie.
    wss.EnableTrackingCookie = true
    // Set keep-alive period to 30 seconds.
    wss.KeepAlive = 30 * time.Second

    server, err := zeroconf.Register("GoZeroconf3", "_workstation._tcp", "local.", 8080, []string{"txtv=0", "lo=1", "la=2"}, nil)
    if err != nil {
        panic(err)
    }

    defer server.Shutdown()

    // Run websocket server.
    wsAddr := fmt.Sprintf("%s:%d", netAddr, wsPort)
    wsCloser, err := wss.ListenAndServe(wsAddr)
    if err != nil {
        log.Fatal(err)
    }
    defer wsCloser.Close()
    log.Printf("Websocket server listening on ws://%s/", wsAddr)

    // Wait for SIGINT (CTRL-c), then close servers and exit.
    shutdown := make(chan os.Signal, 1)
    signal.Notify(shutdown, os.Interrupt)
    <-shutdown
    // Servers close at exit due to defer calls.
}

func createLocalCallee(nxr router.Router, realm string) (*client.Client, error) {
    logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
    cfg := client.Config{
        Realm:  realm,
        Logger: logger,
    }
    callee, err := client.ConnectLocal(nxr, cfg)
    if err != nil {
        return nil, err
    }

    // Register procedure "time"
    const timeProc = "worldtime"
    if err = callee.Register(timeProc, worldTime, nil); err != nil {
        return nil, fmt.Errorf("Failed to register %q: %s", timeProc, err)
    }
    log.Printf("Registered procedure %q with router", timeProc)

    return callee, nil
}

func worldTime(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
    now := time.Now()
    results := wamp.List{fmt.Sprintf("UTC: %s", now.UTC())}

    return client.InvokeResult{Args: results}
}

func subscribeMetaOnJoin(nxr router.Router, realm string) {
    logger := log.New(os.Stdout, "CALLEE> ", log.LstdFlags)
    cfg := client.Config{
        Realm:  realm,
        Logger: logger,
    }
    cli, err := client.ConnectLocal(nxr, cfg)
    if err != nil {
        return
    }
    // Define function to handle on_join events received.
    onJoin := func(event *wamp.Event) {
        if len(event.Arguments) != 0 {
            if details, ok := wamp.AsDict(event.Arguments[0]); ok {
                onJoinID, _ := wamp.AsID(details["session"])
                authid, _ := wamp.AsString(details["authid"])
                logger.Printf("Client %v joined realm (authid=%s)\n", onJoinID, authid)
                return
            }
        }
        logger.Println("Client joined realm - no data provided")
    }

    // Subscribe to on_join topic.
    err = cli.Subscribe(metaOnJoin, onJoin, nil)
    if err != nil {
        logger.Fatal("subscribe error:", err)
    }
    logger.Println("Subscribed to", metaOnJoin)
}