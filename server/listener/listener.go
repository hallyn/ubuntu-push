/*
 Copyright 2013-2014 Canonical Ltd.

 This program is free software: you can redistribute it and/or modify it
 under the terms of the GNU General Public License version 3, as published
 by the Free Software Foundation.

 This program is distributed in the hope that it will be useful, but
 WITHOUT ANY WARRANTY; without even the implied warranties of
 MERCHANTABILITY, SATISFACTORY QUALITY, or FITNESS FOR A PARTICULAR
 PURPOSE.  See the GNU General Public License for more details.

 You should have received a copy of the GNU General Public License along
 with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

// Package listener has code to listen for device connections
// and setup sessions for them.
package listener

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/ubports/ubuntu-push/logger"
)

// A DeviceListenerConfig offers the DeviceListener configuration.
type DeviceListenerConfig interface {
	// Addr to listen on.
	Addr() string
	// TLS config
	TLSServerConfig() *tls.Config
}

// DeviceListener listens and setup sessions from device connections.
type DeviceListener struct {
	net.Listener
}

// DeviceListen creates a DeviceListener for device connections based
// on config.  If lst is not nil DeviceListen just wraps it with a TLS
// layer instead of starting creating a new listener.
func DeviceListen(lst net.Listener, cfg DeviceListenerConfig) (*DeviceListener, error) {
	if lst == nil {
		var err error
		lst, err = net.Listen("tcp", cfg.Addr())
		if err != nil {
			return nil, err
		}
	}
	tlsCfg := cfg.TLSServerConfig()
	return &DeviceListener{tls.NewListener(lst, tlsCfg)}, nil
}

// handleTemporary checks and handles if the error is just a temporary network
// error.
func handleTemporary(err error) bool {
	if netError, isNetError := err.(net.Error); isNetError {
		if netError.Temporary() {
			// wait, xxx exponential backoff?
			time.Sleep(100 * time.Millisecond)
			return true
		}
	}
	return false
}

// SessionResourceManager allows to limit resource usage tracking connections.
type SessionResourceManager interface {
	ConsumeConn()
}

// NOP SessionResourceManager.
type NopSessionResourceManager struct{}

func (r *NopSessionResourceManager) ConsumeConn() {}

// AcceptLoop accepts connections and starts sessions for them.
func (dl *DeviceListener) AcceptLoop(session func(net.Conn) error, resource SessionResourceManager, logger logger.Logger) error {
	for {
		resource.ConsumeConn()
		conn, err := dl.Listener.Accept()
		if err != nil {
			if handleTemporary(err) {
				logger.Errorf("device listener: %s -- retrying", err)
				continue
			}
			return err
		}
		go func() {
			defer func() {
				if err := recover(); err != nil {
					logger.PanicStackf("terminating device connection on: %v", err)
				}
			}()
			session(conn)
		}()
	}
}
