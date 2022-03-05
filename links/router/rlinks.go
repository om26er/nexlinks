package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ed25519"

	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/router"
	"github.com/gammazero/nexus/v3/transport/serialize"
	"github.com/gammazero/nexus/v3/wamp"
)

func ConnectRemoteLeg(remoteRouterURL string, config *client.Config, localRouter *router.Router,
	reconnectSeconds time.Duration) {

	remoteSession, err := client.ConnectNet(context.Background(), remoteRouterURL, *config)

	if err != nil {
		log.Println(fmt.Sprintf("Unable to connect to remote leg, retrying in %d seconds", reconnectSeconds))
		time.Sleep(reconnectSeconds * time.Second)
		ConnectRemoteLeg(remoteRouterURL, config, localRouter, reconnectSeconds)
	} else {
		log.Println("Established remote connection")

		logger := log.New(os.Stdout, "", log.LstdFlags)
		cfg := client.Config{
			Realm:  config.Realm,
			Logger: logger,
		}
		localSession, _ := client.ConnectLocal(*localRouter, cfg)

		SetupInvocationForwarding(localSession, remoteSession)
		SetupEventForwarding(localSession, remoteSession)
	}

	select {
	case <- remoteSession.Done():
		ConnectRemoteLeg(remoteRouterURL, config, localRouter, reconnectSeconds)
	}
}

func SetupInvocationForwarding(localSession *client.Client, remoteSession *client.Client) {

	regs := make(map[int]string)

	onRegCreate := func(event *wamp.Event) {
		if len(event.Arguments) > 0 {
			id, ok := wamp.AsID(event.Arguments[0])
			if ok {
				log.Println(id)
			}

			if details, ok := wamp.AsDict(event.Arguments[1]); ok {
				if uri, ok := wamp.AsString(details["uri"]); ok {

					invocationHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
						response, _ := localSession.Call(ctx, uri, wamp.Dict{}, inv.Arguments, inv.ArgumentsKw, nil)
						return client.InvokeResult{Args: response.Arguments, Kwargs: response.ArgumentsKw}
					}

					err := remoteSession.Register(uri, invocationHandler, wamp.Dict{})
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

	// Return IDs for all currently registered procedures on the router
	result, err := localSession.Call(context.Background(), metaProcRegList, nil, nil, nil, nil)
	if idMap, ok := wamp.AsDict(result.Arguments[0]); ok {
		if idsExact, ok := wamp.AsList(idMap["exact"]); ok {
			for _, id := range idsExact {
				result, err := localSession.Call(context.Background(), metaProcRegGet, nil, wamp.List{id}, nil, nil)
				if err == nil {
					regDetails, _ := wamp.AsDict(result.Arguments[0])
					uri, _ := wamp.AsString(regDetails["uri"])
					// Don't try to forward internal procedures
					if !strings.HasPrefix(uri, "wamp.") {

						invocationHandler := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
							response, _ := localSession.Call(ctx, uri, wamp.Dict{}, inv.Arguments, inv.ArgumentsKw, nil)
							return client.InvokeResult{Args: response.Arguments, Kwargs: response.ArgumentsKw}
						}

						err := remoteSession.Register(uri, invocationHandler, nil)
						if err != nil {
							log.Println("We got a problem here....")
						} else {
							if id, ok := wamp.AsID(id); ok {
								regs[int(id)] = uri
							}
						}
					}
				} else {
					log.Println(err, id)
				}
			}
		}
	}

	err = localSession.Subscribe(metaEventRegOnCreate, onRegCreate, nil)
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

func SetupEventForwarding(localSession *client.Client, remoteSession *client.Client) {
	subs := make(map[int]string)

	onSubCreate := func(event *wamp.Event) {
		if len(event.Arguments) > 0 {
			id, _ := wamp.AsID(event.Arguments[0])

			if details, ok := wamp.AsDict(event.Arguments[1]); ok {
				log.Println(event.Arguments[1])
				if topic, ok := wamp.AsString(details["uri"]); ok {
					eventHandler := func(event *wamp.Event) {
						err := localSession.Publish(topic, wamp.Dict{}, event.Arguments, event.ArgumentsKw)
						if err != nil {
							return
						}
					}

					err := remoteSession.Subscribe(topic, eventHandler, nil)
					if err != nil {
						log.Println("We got a problem here....")
					} else {
						subs[int(id)] = topic
					}
				}
			}
		}
	}

	onSubDelete := func(event *wamp.Event) {
		if len(event.Arguments) > 0 {
			id, ok := wamp.AsID(event.Arguments[0])
			if ok {
				if uri, ok := subs[int(id)]; ok {
					// Success of failure, we need to remove registration from our store
					_ = remoteSession.Unsubscribe(uri)
					delete(subs, int(id))
					log.Println(fmt.Sprintf("Unsubscribed topic %s", uri))
				}
			}
		}
	}

	// Return IDs for all currently registered procedures on the router
	result, err := localSession.Call(context.Background(), metaProcSubList, nil, nil, nil, nil)
	if idMap, ok := wamp.AsDict(result.Arguments[0]); ok {
		if idsExact, ok := wamp.AsList(idMap["exact"]); ok {
			for _, id := range idsExact {
				result, err := localSession.Call(context.Background(), metaProcSubGet, nil, wamp.List{id}, nil, nil)
				if err == nil {
					regDetails, _ := wamp.AsDict(result.Arguments[0])
					uri, _ := wamp.AsString(regDetails["uri"])
					// Don't try to forward internal procedures
					if !strings.HasPrefix(uri, "wamp.") {

						eventHandler := func(event *wamp.Event) {
							err := localSession.Publish(uri, wamp.Dict{}, event.Arguments, event.ArgumentsKw)
							if err != nil {
								return
							}
						}

						err := remoteSession.Subscribe(uri, eventHandler, nil)
						if err != nil {
							log.Println("We got a problem here....")
						} else {
							if id, ok := wamp.AsID(id); ok {
								subs[int(id)] = uri
							}
						}
					}
				} else {
					log.Println(err, id)
				}
			}
		}
	}

	err = localSession.Subscribe(metaEventSubOnCreate, onSubCreate, nil)
	if err != nil {
		log.Fatal("subscribe error:", err)
	}
	log.Println("Subscribed to", metaEventSubOnCreate)

	err = localSession.Subscribe(metaEventSubOnDelete, onSubDelete, nil)
	if err != nil {
		log.Fatal("subscribe error:", err)
	}
	log.Println("Subscribed to", metaEventSubOnDelete)
}

func ConstructLinkConfig(privateKeyHex string, realm string) client.Config {
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
