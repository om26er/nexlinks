package main

import (
    "context"
    "encoding/hex"
    "flag"
    "fmt"
    "github.com/gammazero/nexus/v3/transport/serialize"
    "golang.org/x/crypto/ed25519"
    "log"
    "os"
    "os/signal"
    "time"

    "github.com/gammazero/nexus/v3/client"
    "github.com/gammazero/nexus/v3/router"
    "github.com/gammazero/nexus/v3/wamp"

    "github.com/grandcat/zeroconf"
)

type keyStore struct {
    provider string
    publicKey   string
}

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

    // create local (embedded) RPC callee client that provides the time in the
    // requested timezones.
    callee, err := createLocalCallee(nxr, realm)
    if err != nil {
        log.Fatal(err)
    }
    defer callee.Close()

    // Create websocket server.
    wss := router.NewWebsocketServer(nxr)
    wss.Upgrader.EnableCompression = true
    wss.EnableTrackingCookie = true
    wss.KeepAlive = 30 * time.Second

    hostname, _ := os.Hostname()

    server, err := zeroconf.Register(hostname, "_st._tcp", "local.", 8080,
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

    session, err := connectToCrossbar()
    if err == nil {
        println(session)
    }

    subscribeMetaOnRegCreate(nxr, realm, session)

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

func subscribeMetaOnRegCreate(nxr router.Router, realm string, session *client.Client) {
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
                    eventHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {

                        response, _ := cli.Call(ctx, uri, wamp.Dict{}, inv.Arguments, inv.ArgumentsKw, nil)

                        return client.InvokeResult{Args: response.Arguments, Kwargs: response.ArgumentsKw}
                    }


                    session.Register(uri, eventHandler, wamp.Dict{})
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


func connectToCrossbar() (*client.Client, error) {
    helloDict := wamp.Dict{}
    helloDict["authid"] = "node1"
    helloDict["authrole"] = "router2router"

    privkey, _ := hex.DecodeString("1c0ecd558e88e9fc51c10e0373a582bdb11db7d14e9bd514fa2703a92b7a5617")
    var pvk = ed25519.NewKeyFromSeed(privkey)

    key := pvk.Public().(ed25519.PublicKey)
    publicKey := hex.EncodeToString(key)
    helloDict["authextra"] = wamp.Dict{"pubkey": publicKey}


    cfg := client.Config{
        Realm:         "realm1",
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

    session, err := client.ConnectNet(context.Background(), "tcp://localhost:8081", cfg)

    if err != nil {
        time.Sleep(5 * time.Second)
        connectToCrossbar()
        fmt.Println(err)
    } else {
        println(session.Connected())
        // FIXME: use a better logger and only print such messages in debug mode.
        //logger.Println("Connected to ", baseUrl)
    }

    return session, err

    //defer session.Close()
}
