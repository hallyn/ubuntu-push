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

package broker

import (
	"encoding/json"

	. "launchpad.net/gocheck"

	"github.com/ubports/ubuntu-push/server/store"
	help "github.com/ubports/ubuntu-push/testing"
)

type exchangesImplSuite struct{}

var _ = Suite(&exchangesImplSuite{})

func (s *exchangesImplSuite) TestFilterByLevel(c *C) {
	notifs := help.Ns(
		json.RawMessage(`{"a": 3}`),
		json.RawMessage(`{"a": 4}`),
		json.RawMessage(`{"a": 5}`),
	)
	res := filterByLevel(5, 5, notifs)
	c.Check(len(res), Equals, 0)
	res = filterByLevel(4, 5, notifs)
	c.Check(len(res), Equals, 1)
	c.Check(res[0].Payload, DeepEquals, json.RawMessage(`{"a": 5}`))
	res = filterByLevel(3, 5, notifs)
	c.Check(len(res), Equals, 2)
	c.Check(res[0].Payload, DeepEquals, json.RawMessage(`{"a": 4}`))
	res = filterByLevel(2, 5, notifs)
	c.Check(len(res), Equals, 3)
	res = filterByLevel(1, 5, notifs)
	c.Check(len(res), Equals, 3)
	// too ahead, pick only last
	res = filterByLevel(10, 5, notifs)
	c.Check(len(res), Equals, 1)
	c.Check(res[0].Payload, DeepEquals, json.RawMessage(`{"a": 5}`))
}

func (s *exchangesImplSuite) TestFilterByLevelEmpty(c *C) {
	res := filterByLevel(5, 0, nil)
	c.Check(len(res), Equals, 0)
	res = filterByLevel(5, 10, nil)
	c.Check(len(res), Equals, 0)
}

func (s *exchangesImplSuite) TestChannelFilter(c *C) {
	payloads := []json.RawMessage{
		json.RawMessage(`{"a/x": 3}`),
		json.RawMessage(`{"b/x": 4}`),
		json.RawMessage(`{"a/y": 5}`),
		json.RawMessage(`{"a/x": 6}`),
	}
	decoded := make([]map[string]interface{}, 4)
	for i, p := range payloads {
		err := json.Unmarshal(p, &decoded[i])
		c.Assert(err, IsNil)
	}
	notifs := help.Ns(payloads...)

	other := store.InternalChannelId("1")

	c.Check(channelFilter("", store.SystemInternalChannelId, nil, nil), IsNil)
	c.Check(channelFilter("", other, notifs[1:], decoded), DeepEquals, payloads[1:])

	// use tag when channel is the sytem channel

	c.Check(channelFilter("c/z", store.SystemInternalChannelId, notifs, decoded), HasLen, 0)

	c.Check(channelFilter("a/x", store.SystemInternalChannelId, notifs, decoded), DeepEquals, []json.RawMessage{payloads[0], payloads[3]})

	c.Check(channelFilter("a/x", store.SystemInternalChannelId, notifs[1:], decoded), DeepEquals, []json.RawMessage{payloads[3]})

}
