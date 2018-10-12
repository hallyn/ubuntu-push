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

// Package suites contains reusable acceptance test suites.
package suites

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"time"

	. "launchpad.net/gocheck"

	"github.com/ubports/ubuntu-push/server/acceptance"
	"github.com/ubports/ubuntu-push/server/acceptance/kit"
	helpers "github.com/ubports/ubuntu-push/testing"
)

// ServerHandle holds the information to attach a client to the test server.
type ServerHandle struct {
	ServerAddr     string
	ServerHTTPAddr string
	ServerEvents   <-chan string
	// last started session
	LastSession *acceptance.ClientSession
}

// Start a client.
func (h *ServerHandle) StartClient(c *C, devId string, levels map[string]int64) (events <-chan string, errorCh <-chan error, stop func()) {
	return h.StartClientAuth(c, devId, levels, "", "")
}

// Start a client with auth.
func (h *ServerHandle) StartClientAuth(c *C, devId string, levels map[string]int64, auth string, cookie string) (events <-chan string, errorCh <-chan error, stop func()) {
	cliEvents, errCh, stop := h.StartClientAuthFlex(c, devId, levels, auth, cookie, regexp.QuoteMeta(devId))
	c.Assert(NextEvent(cliEvents, errCh), Matches, "connected .*")
	return cliEvents, errCh, stop
}

// Start a client with auth, take a devId regexp, don't check any client event.
func (h *ServerHandle) StartClientAuthFlex(c *C, devId string, levels map[string]int64, auth, cookie, devIdRegexp string) (events <-chan string, errorCh <-chan error, stop func()) {
	errCh := make(chan error, 1)
	cliEvents := make(chan string, 10)
	sess := testClientSession(h.ServerAddr, devId, "m1", "img1", false)
	sess.Levels = levels
	sess.Auth = auth
	if auth != "" {
		sess.ExchangeTimeout = 5 * time.Second
	}
	if cookie != "" {
		sess.SetCookie(cookie)
		sess.ReportSetParams = true
	}
	err := sess.Dial()
	c.Assert(err, IsNil)
	h.LastSession = sess
	clientShutdown := make(chan bool, 1) // abused as an atomic flag
	intercept := func(ic *interceptingConn, op string, b []byte) (bool, int, error) {
		// read after ack
		if op == "read" && len(clientShutdown) > 0 {
			// exit the sess.Run() goroutine, client will close
			runtime.Goexit()
		}
		return false, 0, nil
	}
	sess.Connection = &interceptingConn{sess.Connection, 0, 0, intercept}
	go func() {
		errCh <- sess.Run(cliEvents)
	}()
	c.Assert(NextEvent(h.ServerEvents, nil), Matches, ".*session.* connected .*")
	c.Assert(NextEvent(h.ServerEvents, nil), Matches, ".*session.* registered "+devIdRegexp)
	return cliEvents, errCh, func() { clientShutdown <- true }
}

// AcceptanceSuite has the basic functionality of the acceptance suites.
type AcceptanceSuite struct {
	// hook to start the server(s)
	StartServer func(c *C, s *AcceptanceSuite, handle *ServerHandle)
	// populated by StartServer
	ServerHandle
	kit.APIClient // has ServerAPIURL
	// KillGroup should be populated by StartServer with functions
	// to kill the server process
	KillGroup map[string]func(os.Signal)
}

// Start a new server for each test.
func (s *AcceptanceSuite) SetUpTest(c *C) {
	s.KillGroup = make(map[string]func(os.Signal))
	s.StartServer(c, s, &s.ServerHandle)
	c.Assert(s.ServerHandle.ServerEvents, NotNil)
	c.Assert(s.ServerHandle.ServerAddr, Not(Equals), "")
	c.Assert(s.ServerAPIURL, Not(Equals), "")
	s.SetupClient(nil, false, http.DefaultMaxIdleConnsPerHost)
}

func (s *AcceptanceSuite) TearDownTest(c *C) {
	for _, f := range s.KillGroup {
		f(os.Kill)
	}
}

func testClientSession(addr string, deviceId, model, imageChannel string, reportPings bool) *acceptance.ClientSession {
	tlsConfig, err := kit.MakeTLSConfig("push-delivery", false, helpers.SourceRelative("../ssl/testing.cert"), "")
	if err != nil {
		panic(fmt.Sprintf("could not read ssl/testing.cert: %v", err))
	}
	return &acceptance.ClientSession{
		ExchangeTimeout: 100 * time.Millisecond,
		ServerAddr:      addr,
		DeviceId:        deviceId,
		Model:           model,
		ImageChannel:    imageChannel,
		ReportPings:     reportPings,
		TLSConfig:       tlsConfig,
	}
}

// typically combined with -gocheck.vv or test selection
var logTraffic = flag.Bool("logTraffic", false, "log traffic")

type connInterceptor func(ic *interceptingConn, op string, b []byte) (bool, int, error)

type interceptingConn struct {
	net.Conn
	totalRead    int
	totalWritten int
	intercept    connInterceptor
}

func (ic *interceptingConn) Write(b []byte) (n int, err error) {
	done := false
	before := ic.totalWritten
	if ic.intercept != nil {
		done, n, err = ic.intercept(ic, "write", b)
	}
	if !done {
		n, err = ic.Conn.Write(b)
	}
	ic.totalWritten += n
	if *logTraffic {
		fmt.Printf("W[%v]: %d %#v %v %d\n", ic.Conn.LocalAddr(), before, string(b[:n]), err, ic.totalWritten)
	}
	return
}

func (ic *interceptingConn) Read(b []byte) (n int, err error) {
	done := false
	before := ic.totalRead
	if ic.intercept != nil {
		done, n, err = ic.intercept(ic, "read", b)
	}
	if !done {
		n, err = ic.Conn.Read(b)
	}
	ic.totalRead += n
	if *logTraffic {
		fmt.Printf("R[%v]: %d %#v %v %d\n", ic.Conn.LocalAddr(), before, string(b[:n]), err, ic.totalRead)
	}
	return
}

// Long after the end of the tests.
var future = time.Now().Add(9 * time.Hour).Format(time.RFC3339)
