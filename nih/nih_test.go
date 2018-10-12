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

package nih

import (
	"testing"

	. "launchpad.net/gocheck"

	"github.com/ubports/ubuntu-push/nih/cnih"
)

func TestNIH(t *testing.T) { TestingT(t) }

type nihSuite struct{}

var _ = Suite(&nihSuite{})

func (ns *nihSuite) TestQuote(c *C) {
	for i, s := range []struct {
		raw    []byte
		quoted []byte
	}{
		{[]byte("test"), []byte("test")},
		{[]byte("foo/bar.baz"), []byte("foo_2fbar_2ebaz")},
		{[]byte("test_thing"), []byte("test_5fthing")},
		{[]byte("\x01\x0f\x10\xff"), []byte("_01_0f_10_ff")},
		{[]byte{}, []byte{'_'}},
	} {
		c.Check(string(s.quoted), Equals, cnih.Quote(s.raw), Commentf("iter %d (%s)", i, string(s.quoted)))
		c.Check(string(Quote(s.raw)), DeepEquals, string(s.quoted), Commentf("iter %d (%s)", i, string(s.quoted)))
		c.Check(Unquote(s.quoted), DeepEquals, s.raw, Commentf("iter %d (%s)", i, string(s.quoted)))
		c.Check(string(Quote(s.raw)), Equals, cnih.Quote(s.raw), Commentf("iter %d (%s)", i, string(s.quoted)))
	}

	// check one cnih doesn't like
	c.Check(Quote([]byte{0}), DeepEquals, []byte("_00"))

	// check we don't panic with some weird ones
	for i, s := range []string{"foo_", "foo_a", "foo_zz"} {
		c.Check(Unquote([]byte(s)), DeepEquals, []byte("foo"), Commentf("iter %d (%s)", i, s))
	}
}
