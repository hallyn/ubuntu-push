package identifier

import (
	. "launchpad.net/gocheck"
	"testing"
)

// hook up gocheck
func Test(t *testing.T) { TestingT(t) }

type IdentifierSuite struct{}
var _ = Suite(&IdentifierSuite{})

// test the basics
func (s *IdentifierSuite) TestGenerate(c *C) {
	id := New()

	c.Check(id.Generate(), Equals, nil)
	c.Check(id.String(), HasLen, 128)
}

//tests the interfaces of the different classes
func (s *IdentifierSuite) TestIdentifierInterface(c *C) {
	_ = []Id{New()}
}
