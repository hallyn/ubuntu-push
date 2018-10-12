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

package gethosts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "launchpad.net/gocheck"

	"github.com/ubports/ubuntu-push/external/murmur3"
)

func TestGetHosts(t *testing.T) { TestingT(t) }

type getHostsSuite struct{}

var _ = Suite(&getHostsSuite{})

func (s *getHostsSuite) TestNew(c *C) {
	gh := New("foobar", "http://where/hosts", 10*time.Second)
	c.Check(gh.hash, Equals, fmt.Sprintf("%x", murmur3.Sum64([]byte("foobar"))))
	c.Check(gh.endpointUrl, Equals, "http://where/hosts")
}

func (s *getHostsSuite) TestGet(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x := r.FormValue("h")
		b, err := json.Marshal(map[string]interface{}{
			"domain": "example.com",
			"hosts":  []string{"http://" + x},
		})
		if err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer ts.Close()
	gh := New("foobar", ts.URL, 1*time.Second)
	res, err := gh.Get()
	c.Assert(err, IsNil)
	c.Check(*res, DeepEquals,
		Host{Domain: "example.com", Hosts: []string{"http://bdd2ae7116c85a45"}})
}

func (s *getHostsSuite) TestGetTimeout(c *C) {
	started := make(chan bool, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- true
		time.Sleep(700 * time.Millisecond)
	}))
	defer func() {
		<-started
		ts.Close()
	}()
	gh := New("foobar", ts.URL, 500*time.Millisecond)
	_, err := gh.Get()
	c.Check(err, ErrorMatches, ".*closed.*")
}

func (s *getHostsSuite) TestGetErrorScenarios(c *C) {
	status := make(chan int, 1)
	body := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(<-status)
		fmt.Fprintf(w, "%s", <-body)
	}))
	defer ts.Close()
	gh := New("foobar", ts.URL, 1*time.Second)
	scenario := func(st int, bdy string, expectedErr error) {
		status <- st
		body <- bdy
		_, err := gh.Get()
		c.Check(err, Equals, expectedErr)
	}

	scenario(http.StatusBadRequest, "", ErrRequest)
	scenario(http.StatusInternalServerError, "", ErrInternal)
	scenario(http.StatusGatewayTimeout, "", ErrTemporary)

	scenario(http.StatusOK, "{", ErrTemporary)
	scenario(http.StatusOK, "{}", ErrTemporary)
	scenario(http.StatusOK, `{"domain": "example.com"}`, ErrTemporary)
	scenario(http.StatusOK, `{"hosts": ["one"]}`, nil)
}
