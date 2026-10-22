// Package client provides the daemon API client context for UI components.
package client

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	daemonclient "github.com/masterkeysrd/tasksmith/client"
)

var clientContext = kitex.CreateContext[*daemonclient.Client](nil)

// Props configures the API client Provider.
type Props struct {
	Client *daemonclient.Client
}

// Provider supplies the daemon client to its descendants independently of any
// query cache or data-fetching library.
func Provider(props Props, children ...kitex.Node) kitex.Node {
	if props.Client == nil {
		panic("ui/client: Provider requires a daemon client")
	}
	return clientContext.Provider(props.Client, children...)
}

// UseClient returns the daemon client supplied by the nearest Provider.
func UseClient() *daemonclient.Client {
	c := kitex.UseContext(clientContext)
	if c == nil {
		panic("ui/client: UseClient must be used within a client.Provider")
	}
	return c
}
