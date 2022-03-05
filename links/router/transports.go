package main

import (
	"fmt"
	"io"
	"time"

	"github.com/gammazero/nexus/v3/router"
)

func SetupWebSocketTransport(localRouter router.Router, netAddr string, wsPort int) (io.Closer, error) {
	// Create websocket server.
	wss := router.NewWebsocketServer(localRouter)
	wss.Upgrader.EnableCompression = true
	wss.EnableTrackingCookie = true
	wss.KeepAlive = 30 * time.Second

	// Run websocket server.
	wsAddr := fmt.Sprintf("%s:%d", netAddr, wsPort)
	return wss.ListenAndServe(wsAddr)
}
