// Package proxy brokers HTTP requests between the plugin's net/http handler
// (producer) and external PHP workers connected over ConnectRPC (consumers).
//
// The plugin does not own worker lifecycle: workers are started, supervised,
// and scaled outside the plugin. They connect IN to proxy.Server, pull work
// from the Queue via FetchRequest, and push responses back via SendResponse.
package proxy
