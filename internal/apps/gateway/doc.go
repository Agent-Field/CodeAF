// Package gateway serves the Apps bridge: one WebSocket per space at
// bridge.Path, call dispatch through the capability check, and topic
// subscriptions fed by the realtime hub. Lane B3.
package gateway
