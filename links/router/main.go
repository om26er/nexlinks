package main

import (
    "context"
    "encoding/hex"
    "flag"
    "fmt"
    "github.com/gammazero/nexus/v3/transport/serialize"
    "golang.org/x/crypto/ed25519"
    "io"
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
    //metaProcRegList = string(wamp.MetaProcRegList)
    //metaProcRegGet = string(wamp.MetaProcRegGet)
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

    // Create router instance.
    routerConfig := &router.Config{
        RealmConfigs: []*router.RealmConfig{
            {
                URI:           wamp.URI(realm),
                AnonymousAuth: true,
                AllowDisclose: true,
                MetaStrict: true,
            },
        },
    }

    nxr, err := router.NewRouter(routerConfig, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer nxr.Close()

    wsCloser, err := setupWebSocketTransport(nxr, netAddr, wsPort)
    if err != nil {
        log.Fatal(err)
    }
    defer func(wsCloser io.Closer) {
        err := wsCloser.Close()
        if err != nil {
            log.Println("WEBSOCKET TRANSPORT CLOSED")
        }
    }(wsCloser)
    log.Printf("Websocket server listening on ws://%s:%d/", netAddr, wsPort)

    // FIXME: make service discovery configurable
    mdns := publishName(realm)
    defer mdns.Shutdown()

    // PUBKEY IS 81deeb0a11c4f3919e6c35adc1980516dfd8ca84e01929b51070d6a7d3e6c012
    cfg := constructLinkConfig("1c0ecd558e88e9fc51c10e0373a582bdb11db7d14e9bd514fa2703a92b7a5617", "realm1")
    session, err := connectRemoteLeg("tcp://localhost:8081", cfg, nxr, 10)
    if err == nil {
        log.Println("Established remote connection")
        log.Println(session)
    }

    defer session.Done()

    // Wait for SIGINT (CTRL-c), then close servers and exit.
    shutdown := make(chan os.Signal, 1)
    signal.Notify(shutdown, os.Interrupt)
    <-shutdown
    // Servers close at exit due to defer calls.
}

func setupWebSocketTransport(localRouter router.Router, netAddr string, wsPort int) (io.Closer, error) {
    // Create websocket server.
    wss := router.NewWebsocketServer(localRouter)
    wss.Upgrader.EnableCompression = true
    wss.EnableTrackingCookie = true
    wss.KeepAlive = 30 * time.Second

    // Run websocket server.
    wsAddr := fmt.Sprintf("%s:%d", netAddr, wsPort)
    return wss.ListenAndServe(wsAddr)
}

func publishName(realm string) *zeroconf.Server {
    hostname, _ := os.Hostname()

    server, err := zeroconf.Register(hostname, "_st._tcp", "local.", 8080,
        []string{fmt.Sprintf("realm=%s", realm)}, nil)

    if err != nil {
        log.Fatal(err)
    }

    return server
}

func setupInvocationForwarding(localSession *client.Client, remoteSession *client.Client) {

    regs := make(map[int]string)

    onRegCreate := func(event *wamp.Event) {
        if len(event.Arguments) > 0 {
            id, ok := wamp.AsID(event.Arguments[0])
            if ok {
                log.Println(id)
            }

            if details, ok := wamp.AsDict(event.Arguments[1]); ok {
                if uri, ok := wamp.AsString(details["uri"]); ok {

                    eventHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
                        response, _ := localSession.Call(ctx, uri, wamp.Dict{}, inv.Arguments, inv.ArgumentsKw, nil)
                        return client.InvokeResult{Args: response.Arguments, Kwargs: response.ArgumentsKw}
                    }

                    err := remoteSession.Register(uri, eventHandler, wamp.Dict{})
                    if err != nil {
                        log.Println("We got a problem here....")
                    } else {
                        regs[int(id)] = uri
                    }
                }
            }
        }
    }

    onRegDelete := func(event *wamp.Event) {
        if len(event.Arguments) > 0 {
            id, ok := wamp.AsID(event.Arguments[0])
            if ok {
                if uri, ok := regs[int(id)]; ok {
                    // Success of failure, we need to remove registration from our store
                    _ = remoteSession.Unregister(uri)
                    delete(regs, int(id))
                }
            }
        }
    }

    err := localSession.Subscribe(metaEventRegOnCreate, onRegCreate, nil)
    if err != nil {
        log.Fatal("subscribe error:", err)
    }
    log.Println("Subscribed to", metaEventRegOnCreate)

    err = localSession.Subscribe(metaEventRegOnDelete, onRegDelete, nil)
    if err != nil {
        log.Fatal("subscribe error:", err)
    }
    log.Println("Subscribed to", metaEventRegOnDelete)
}

func setupEventForwarding(localSession *client.Client, remoteSession *client.Client) {
    log.Println(localSession, remoteSession)
}

func constructLinkConfig(privateKeyHex string, realm string) client.Config {
    helloDict := wamp.Dict{}
    helloDict["authrole"] = "router2router"

    privkey, _ := hex.DecodeString(privateKeyHex)
    pvk := ed25519.NewKeyFromSeed(privkey)
    key := pvk.Public().(ed25519.PublicKey)
    publicKey := hex.EncodeToString(key)

    helloDict["authextra"] = wamp.Dict{"pubkey": publicKey}

    cfg := client.Config{
        Realm:         realm,
        HelloDetails:  helloDict,
        Serialization: serialize.CBOR,
        AuthHandlers: map[string]client.AuthFunc{
            "cryptosign": func(c *wamp.Challenge) (string, wamp.Dict) {
                challengeHex, _ := wamp.AsString(c.Extra["challenge"])
                challengeBytes, _ := hex.DecodeString(challengeHex)

                signed := ed25519.Sign(pvk, challengeBytes)
                signedHex := hex.EncodeToString(signed)
                result := signedHex + challengeHex
                return result, wamp.Dict{}
            },
        },
    }

    return cfg
}

func connectRemoteLeg(remoteRouterURL string, config client.Config, localRouter router.Router,
    reconnectSeconds time.Duration) (*client.Client, error) {

    remoteSession, err := client.ConnectNet(context.Background(), remoteRouterURL, config)

    if err != nil {
        log.Println(fmt.Sprintf("Unable to connect to remote leg, retrying in %d seconds", reconnectSeconds))
        time.Sleep(reconnectSeconds * time.Second)
        return connectRemoteLeg(remoteRouterURL, config, localRouter, reconnectSeconds)
    } else {
        logger := log.New(os.Stdout, "", log.LstdFlags)
        cfg := client.Config{
            Realm:  config.Realm,
            Logger: logger,
        }
        localSession, _ := client.ConnectLocal(localRouter, cfg)

        setupInvocationForwarding(localSession, remoteSession)
        setupEventForwarding(localSession, remoteSession)
    }

    return remoteSession, err
}
