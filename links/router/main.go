package main

import (
    "context"
    "flag"
    "fmt"
    "github.com/gammazero/nexus/v3/router/auth"
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
    metaProcRegList = string(wamp.MetaProcRegList)
    metaProcRegGet = string(wamp.MetaProcRegGet)
    metaEventRegOnCreate = string(wamp.MetaEventRegOnCreate)
    metaEventRegOnDelete = string(wamp.MetaEventRegOnDelete)
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

    cryptosign := auth.NewCryptoSignAuthenticator()

    // Create router instance.
    routerConfig := &router.Config{
        RealmConfigs: []*router.RealmConfig{
            &router.RealmConfig{
                URI:           wamp.URI(realm),
                AnonymousAuth: true,
                AllowDisclose: true,
                MetaStrict: true,
                Authenticators: []auth.Authenticator{cryptosign},
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

    subscribeMetaOnRegCreate(nxr, realm)

    // Create websocket server.
    wss := router.NewWebsocketServer(nxr)
    wss.Upgrader.EnableCompression = true
    wss.EnableTrackingCookie = true
    wss.KeepAlive = 30 * time.Second

    server, err := zeroconf.Register("GoZeroconf3", "_workstation._tcp", "local.", 8080,
        []string{fmt.Sprintf("realm=%s", realm)}, nil)

    if err != nil {
        log.Fatal(err)
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
    logger := log.New(os.Stdout, "", log.LstdFlags)
    cfg := client.Config{
        Realm:  realm,
        Logger: logger,
    }
    callee, err := client.ConnectLocal(nxr, cfg)
    if err != nil {
        return nil, err
    }

    // Register procedure "time"
    const timeProc = "pk.codebase.time"
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

func subscribeMetaOnRegCreate(nxr router.Router, realm string) {
    logger := log.New(os.Stdout, "", log.LstdFlags)
    cfg := client.Config{
        Realm:  realm,
        Logger: logger,
    }
    cli, err := client.ConnectLocal(nxr, cfg)
    if err != nil {
        return
    }

    onRegCreate := func(event *wamp.Event) {

        if len(event.Arguments) != 0 {
            id, ok := wamp.AsID(event.Arguments[0])
            if ok {
                println(id)
            }

            if details, ok := wamp.AsDict(event.Arguments[1]); ok {
                if uri, ok := wamp.AsString(details["uri"]); ok {
                    println(uri)
                }
            }
        }
    }

    err = cli.Subscribe(metaEventRegOnCreate, onRegCreate, nil)
    if err != nil {
        logger.Fatal("subscribe error:", err)
    }
    logger.Println("Subscribed to", metaEventRegOnCreate)
}
