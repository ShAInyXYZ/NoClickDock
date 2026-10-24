//go:build !linux

package localservers

import (
	"context"
	"errors"
)

type unavailableBackend struct{}

func newBackend() backend { return unavailableBackend{} }

var unsupported = errors.New("Local Servers discovery currently requires Linux /proc")

func (unavailableBackend) Scan(context.Context) ([]server, error) { return nil, unsupported }
func (unavailableBackend) Verify(server) error                    { return unsupported }
func (unavailableBackend) Stop(context.Context, server) error     { return unsupported }
func (unavailableBackend) Unit(process) string                    { return "" }
func (unavailableBackend) Service(context.Context, string) (service, error) {
	return service{}, unsupported
}
func (unavailableBackend) Exclusive(server, service) bool                { return false }
func (unavailableBackend) Control(context.Context, server, string) error { return unsupported }
func (unavailableBackend) Launch(context.Context, launch, string) error  { return unsupported }
func (unavailableBackend) Logs(context.Context, server) (string, error)  { return "", unsupported }
func readLaunch(string) (launch, error)                                  { return launch{}, unsupported }
func supervised(process) bool                                            { return false }

func privateLaunchFile(string) error { return unsupported }

func capturePrivate(*process) {}
