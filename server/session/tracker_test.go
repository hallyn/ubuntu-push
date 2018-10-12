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

package session

import (
	"fmt"
	"net"

	. "launchpad.net/gocheck"

	"github.com/ubports/ubuntu-push/server/broker"
	"github.com/ubports/ubuntu-push/server/broker/testing"
	helpers "github.com/ubports/ubuntu-push/testing"
)

type trackerSuite struct {
	testlog *helpers.TestLogger
}

func (s *trackerSuite) SetUpTest(c *C) {
	s.testlog = helpers.NewTestLogger(c, "debug")
}

var _ = Suite(&trackerSuite{})

type testRemoteAddrable struct{}

func (tra *testRemoteAddrable) RemoteAddr() net.Addr {
	return &net.TCPAddr{net.IPv4(127, 0, 0, 1), 9999, ""}
}

func (s *trackerSuite) TestSessionTrackStart(c *C) {
	track := NewTracker(s.testlog)
	track.Start(&testRemoteAddrable{})
	c.Check(track.SessionId(), Not(Equals), "")
	regExpected := fmt.Sprintf(`DEBUG session\(%s\) connected 127\.0\.0\.1:9999\n`, track.SessionId())
	c.Check(s.testlog.Captured(), Matches, regExpected)
}

func (s *trackerSuite) TestSessionTrackRegistered(c *C) {
	track := NewTracker(s.testlog)
	track.Start(&testRemoteAddrable{})
	track.Registered(&testing.TestBrokerSession{DeviceId: "DEV-ID"})
	regExpected := fmt.Sprintf(`.*connected.*\nINFO session\(%s\) registered DEV-ID\n`, track.SessionId())
	c.Check(s.testlog.Captured(), Matches, regExpected)
}

func (s *trackerSuite) TestSessionTrackEnd(c *C) {
	track := NewTracker(s.testlog)
	track.Start(&testRemoteAddrable{})
	track.End(&broker.ErrAbort{})
	regExpected := fmt.Sprintf(`.*connected.*\nDEBUG session\(%s\) ended with: session aborted \(\)\n`, track.SessionId())
	c.Check(s.testlog.Captured(), Matches, regExpected)
}
