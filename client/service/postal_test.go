/*
 Copyright 2014 Canonical Ltd.

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

package service

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"

	"code.google.com/p/go-uuid/uuid"

	. "launchpad.net/gocheck"

	"launchpad.net/ubuntu-push/bus"
	"launchpad.net/ubuntu-push/bus/notifications"
	testibus "launchpad.net/ubuntu-push/bus/testing"
	"launchpad.net/ubuntu-push/bus/windowstack"
	"launchpad.net/ubuntu-push/click"
	clickhelp "launchpad.net/ubuntu-push/click/testing"
	"launchpad.net/ubuntu-push/launch_helper"
	"launchpad.net/ubuntu-push/messaging/reply"
	helpers "launchpad.net/ubuntu-push/testing"
	"launchpad.net/ubuntu-push/testing/condition"
)

// takeNext takes a value from given channel with a 5s timeout
func takeNextBool(ch <-chan bool) bool {
	select {
	case <-time.After(5 * time.Second):
		panic("channel stuck: too long waiting")
	case v := <-ch:
		return v
	}
}

// takeNextBytes takes a value from given channel with a 5s timeout
func takeNextBytes(ch <-chan []byte) []byte {
	select {
	case <-time.After(5 * time.Second):
		panic("channel stuck: too long waiting")
	case v := <-ch:
		return v
	}
}

// takeNextHelperOutput takes a value from given channel with a 5s timeout
func takeNextHelperOutput(ch <-chan *launch_helper.HelperOutput) *launch_helper.HelperOutput {
	select {
	case <-time.After(5 * time.Second):
		panic("channel stuck: too long waiting")
	case v := <-ch:
		return v
	}
}

func takeNextError(ch <-chan error) error {
	select {
	case <-time.After(5 * time.Second):
		panic("channel stuck: too long waiting")
	case v := <-ch:
		return v
	}
}

func installTickMessageHandler(svc *PostalService) chan error {
	ch := make(chan error)
	msgHandler := svc.GetMessageHandler()
	svc.SetMessageHandler(func(app *click.AppId, nid string, output *launch_helper.HelperOutput) error {
		var err error
		if msgHandler != nil {
			err = msgHandler(app, nid, output)
		}
		ch <- err
		return err
	})
	return ch
}

type fakeHelperLauncher struct {
	i    int
	ch   chan []byte
	done func(string)
}

func (fhl *fakeHelperLauncher) InstallObserver(done func(string)) error {
	fhl.done = done
	return nil
}
func (fhl *fakeHelperLauncher) RemoveObserver() error  { return nil }
func (fhl *fakeHelperLauncher) Stop(_, _ string) error { return nil }
func (fhl *fakeHelperLauncher) HelperInfo(app *click.AppId) (string, string) {
	if app.Click {
		return "helpId", "bar"
	} else {
		return "", "lhex"
	}
}
func (fhl *fakeHelperLauncher) Launch(_, _, f1, f2 string) (string, error) {
	dat, err := ioutil.ReadFile(f1)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(f2, dat, os.ModeTemporary)
	if err != nil {
		return "", err
	}

	id := []string{"0", "1", "2"}[fhl.i]
	fhl.i++

	fhl.ch <- dat

	return id, nil
}

type postalSuite struct {
	log          *helpers.TestLogger
	bus          bus.Endpoint
	notifBus     bus.Endpoint
	counterBus   bus.Endpoint
	hapticBus    bus.Endpoint
	urlDispBus   bus.Endpoint
	winStackBus  bus.Endpoint
	fakeLauncher *fakeHelperLauncher
	getTempDir   func(string) (string, error)
}

type ualPostalSuite struct {
	postalSuite
}

type trivialPostalSuite struct {
	postalSuite
}

var _ = Suite(&ualPostalSuite{})
var _ = Suite(&trivialPostalSuite{})

func (ps *postalSuite) SetUpTest(c *C) {
	ps.log = helpers.NewTestLogger(c, "debug")
	ps.bus = testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true))
	ps.notifBus = testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true))
	ps.counterBus = testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true))
	ps.hapticBus = testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true))
	ps.urlDispBus = testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true))
	ps.winStackBus = testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true), []windowstack.WindowsInfo{})
	ps.fakeLauncher = &fakeHelperLauncher{ch: make(chan []byte)}

	ps.getTempDir = launch_helper.GetTempDir
	d := c.MkDir()
	launch_helper.GetTempDir = func(pkgName string) (string, error) {
		tmpDir := filepath.Join(d, pkgName)
		return tmpDir, os.MkdirAll(tmpDir, 0700)
	}
}

func (ps *postalSuite) TearDownTest(c *C) {
	launch_helper.GetTempDir = ps.getTempDir
}

func (ts *trivialPostalSuite) SetUpTest(c *C) {
	ts.postalSuite.SetUpTest(c)
	useTrivialHelper = true
}

func (ts *trivialPostalSuite) TearDownTest(c *C) {
	ts.postalSuite.TearDownTest(c)
	useTrivialHelper = false
}

func (ps *postalSuite) replaceBuses(pst *PostalService) *PostalService {
	pst.Bus = ps.bus
	pst.NotificationsEndp = ps.notifBus
	pst.EmblemCounterEndp = ps.counterBus
	pst.HapticEndp = ps.hapticBus
	pst.URLDispatcherEndp = ps.urlDispBus
	pst.WindowStackEndp = ps.winStackBus
	pst.launchers = map[string]launch_helper.HelperLauncher{}
	return pst
}

func (ps *postalSuite) TestStart(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Check(svc.IsRunning(), Equals, false)
	c.Check(svc.Start(), IsNil)
	c.Check(svc.IsRunning(), Equals, true)
	svc.Stop()
}

func (ps *postalSuite) TestStartTwice(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Check(svc.Start(), IsNil)
	c.Check(svc.Start(), Equals, ErrAlreadyStarted)
	svc.Stop()
}

func (ps *postalSuite) TestStartNoLog(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, nil))
	c.Check(svc.Start(), Equals, ErrNotConfigured)
}

func (ps *postalSuite) TestStartNoBus(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.Bus = nil
	c.Check(svc.Start(), Equals, ErrNotConfigured)

	svc = ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.NotificationsEndp = nil
	c.Check(svc.Start(), Equals, ErrNotConfigured)
}

func (ps *postalSuite) TestTakeTheBusFail(c *C) {
	nEndp := testibus.NewMultiValuedTestingEndpoint(condition.Work(true), condition.Work(false))
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.NotificationsEndp = nEndp
	_, err := svc.takeTheBus()
	c.Check(err, NotNil)
}

func (ps *postalSuite) TestTakeTheBusOk(c *C) {
	nEndp := testibus.NewMultiValuedTestingEndpoint(condition.Work(true), condition.Work(true), []interface{}{uint32(1), "hello"})
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.NotificationsEndp = nEndp
	_, err := svc.takeTheBus()
	c.Check(err, IsNil)
}

func (ps *postalSuite) TestStartFailsOnBusDialFailure(c *C) {
	// XXX actually, we probably want to autoredial this
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.Bus = testibus.NewTestingEndpoint(condition.Work(false), nil)
	c.Check(svc.Start(), ErrorMatches, `.*(?i)cond said no.*`)
	svc.Stop()
}

func (ps *postalSuite) TestStartGrabsName(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Assert(svc.Start(), IsNil)
	callArgs := testibus.GetCallArgs(ps.bus)
	defer svc.Stop()
	c.Assert(callArgs, NotNil)
	c.Check(callArgs[0].Member, Equals, "::GrabName")
}

func (ps *postalSuite) TestStopClosesBus(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Assert(svc.Start(), IsNil)
	svc.Stop()
	callArgs := testibus.GetCallArgs(ps.bus)
	c.Assert(callArgs, NotNil)
	c.Check(callArgs[len(callArgs)-1].Member, Equals, "::Close")
}

//
// Post() tests

func (ps *postalSuite) TestPostWorks(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.msgHandler = nil
	ch := installTickMessageHandler(svc)
	fakeLauncher2 := &fakeHelperLauncher{ch: make(chan []byte)}
	svc.launchers = map[string]launch_helper.HelperLauncher{
		"click":  ps.fakeLauncher,
		"legacy": fakeLauncher2,
	}
	c.Assert(svc.Start(), IsNil)
	rvs, err := svc.post(aPackageOnBus, []interface{}{anAppId, `{"message":{"world":1}}`}, nil)
	c.Assert(err, IsNil)
	c.Check(rvs, IsNil)
	rvs, err = svc.post(aPackageOnBus, []interface{}{anAppId, `{"message":{"moon":1}}`}, nil)
	c.Assert(err, IsNil)
	c.Check(rvs, IsNil)
	rvs, err = svc.post("_", []interface{}{"_classic-app", `{"message":{"mars":42}}`}, nil)
	c.Assert(err, IsNil)
	c.Check(rvs, IsNil)

	if ps.fakeLauncher.done != nil {
		// wait for the two posts to "launch"
		takeNextBytes(ps.fakeLauncher.ch)
		takeNextBytes(ps.fakeLauncher.ch)
		takeNextBytes(fakeLauncher2.ch)

		go ps.fakeLauncher.done("0") // OneDone
		go ps.fakeLauncher.done("1") // OneDone
		go fakeLauncher2.done("0")
	}

	c.Check(takeNextError(ch), IsNil) // one,
	c.Check(takeNextError(ch), IsNil) // two,
	c.Check(takeNextError(ch), IsNil) // three posts
	c.Assert(svc.mbox, HasLen, 2)
	box, ok := svc.mbox[anAppId]
	c.Check(ok, Equals, true)
	msgs := box.AllMessages()
	c.Assert(msgs, HasLen, 2)
	c.Check(msgs[0], Equals, `{"world":1}`)
	c.Check(msgs[1], Equals, `{"moon":1}`)
	box, ok = svc.mbox["_classic-app"]
	c.Assert(ok, Equals, true)
	msgs = box.AllMessages()
	c.Assert(msgs, HasLen, 1)
	c.Check(msgs[0], Equals, `{"mars":42}`)
}

func (ps *postalSuite) TestPostSignal(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.msgHandler = nil

	hInp := &launch_helper.HelperInput{
		App: clickhelp.MustParseAppId(anAppId),
	}
	res := &launch_helper.HelperResult{Input: hInp}

	svc.handleHelperResult(res)

	// and check it fired the right signal
	callArgs := testibus.GetCallArgs(ps.bus)
	l := len(callArgs)
	if l < 1 {
		c.Fatal("not enough elements in resposne from GetCallArgs")
	}
	c.Check(callArgs[l-1].Member, Equals, "::Signal")
	c.Check(callArgs[l-1].Args, DeepEquals, []interface{}{"Post", aPackageOnBus, []interface{}{anAppId}})
}

func (ps *postalSuite) TestPostFailsIfPostFails(c *C) {
	bus := testibus.NewTestingEndpoint(condition.Work(true),
		condition.Work(false))
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.Bus = bus
	svc.SetMessageHandler(func(*click.AppId, string, *launch_helper.HelperOutput) error { return errors.New("fail") })
	_, err := svc.post("/hello", []interface{}{"xyzzy"}, nil)
	c.Check(err, NotNil)
}

func (ps *postalSuite) TestPostFailsIfBadArgs(c *C) {
	for i, s := range []struct {
		args []interface{}
		errt error
	}{
		{nil, ErrBadArgCount},
		{[]interface{}{}, ErrBadArgCount},
		{[]interface{}{1}, ErrBadArgCount},
		{[]interface{}{anAppId, 1}, ErrBadArgType},
		{[]interface{}{anAppId, "zoom"}, ErrBadJSON},
		{[]interface{}{1, "hello"}, ErrBadArgType},
		{[]interface{}{1, 2, 3}, ErrBadArgCount},
		{[]interface{}{"bar", "hello"}, ErrBadAppId},
	} {
		reg, err := new(PostalService).post(aPackageOnBus, s.args, nil)
		c.Check(reg, IsNil, Commentf("iteration #%d", i))
		c.Check(err, Equals, s.errt, Commentf("iteration #%d", i))
	}
}

//
// Post (Broadcast) tests

func (ps *postalSuite) TestPostBroadcast(c *C) {

	bus := testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true), uint32(1))
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))

	svc.NotificationsEndp = bus
	svc.launchers = map[string]launch_helper.HelperLauncher{
		"legacy": ps.fakeLauncher,
	}
	c.Assert(svc.Start(), IsNil)

	msgId := uuid.New()
	svc.SetMessageHandler(func(app *click.AppId, nid string, output *launch_helper.HelperOutput) error {
		expectedAppId, _ := click.ParseAppId("_ubuntu-system-settings")
		c.Check(app, DeepEquals, expectedAppId)
		c.Check(nid, Equals, msgId)
		return nil
	})
	decoded := map[string]interface{}{
		"daily/mako": []interface{}{float64(102), "tubular"},
	}
	// marshal decoded  to json
	payload, _ := json.Marshal(decoded)
	appId, _ := click.ParseAppId("_ubuntu-system-settings")
	err := svc.Post(appId, msgId, payload)
	c.Assert(err, IsNil)

	if ps.fakeLauncher.done != nil {
		inputData := takeNextBytes(ps.fakeLauncher.ch)
		expectedData, _ := json.Marshal(decoded)
		c.Check(inputData, DeepEquals, expectedData)
		go ps.fakeLauncher.done("0") // OneDone
	}
}

func (ps *postalSuite) TestPostBroadcastDoesNotFail(c *C) {
	bus := testibus.NewTestingEndpoint(condition.Work(true),
		condition.Work(false))
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.launchers = map[string]launch_helper.HelperLauncher{
		"legacy": ps.fakeLauncher,
	}
	c.Assert(svc.Start(), IsNil)
	svc.NotificationsEndp = bus
	svc.SetMessageHandler(func(*click.AppId, string, *launch_helper.HelperOutput) error {
		ps.log.Debugf("about to fail")
		return errors.New("fail")
	})
	ch := installTickMessageHandler(svc)
	decoded := map[string]interface{}{
		"daily/mako": []interface{}{float64(102), "tubular"},
	}
	// marshal decoded  to json
	payload, _ := json.Marshal(decoded)
	appId, _ := click.ParseAppId("_ubuntu-system-settings")
	msgId := uuid.New()
	err := svc.Post(appId, msgId, payload)
	c.Assert(err, IsNil)

	if ps.fakeLauncher.done != nil {
		takeNextBytes(ps.fakeLauncher.ch)
		go ps.fakeLauncher.done("0") // OneDone
	}

	c.Check(takeNextError(ch), NotNil) // the messagehandler failed
	c.Check(err, IsNil)                // but broadcast was oblivious
	c.Check(ps.log.Captured(), Matches, `(?sm).*about to fail$`)
}

//
// Notifications tests
func (ps *postalSuite) TestNotificationsWorks(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	nots, err := svc.popAll(aPackageOnBus, []interface{}{anAppId}, nil)
	c.Assert(err, IsNil)
	c.Assert(nots, NotNil)
	c.Assert(nots, HasLen, 1)
	c.Check(nots[0], HasLen, 0)
	c.Assert(svc.mbox, IsNil)
	svc.mbox = make(map[string]*mBox)
	nots, err = svc.popAll(aPackageOnBus, []interface{}{anAppId}, nil)
	c.Assert(err, IsNil)
	c.Assert(nots, NotNil)
	c.Assert(nots, HasLen, 1)
	c.Check(nots[0], HasLen, 0)
	box := new(mBox)
	svc.mbox[anAppId] = box
	m1 := json.RawMessage(`"m1"`)
	m2 := json.RawMessage(`"m2"`)
	box.Append(m1, "n1")
	box.Append(m2, "n2")
	nots, err = svc.popAll(aPackageOnBus, []interface{}{anAppId}, nil)
	c.Assert(err, IsNil)
	c.Assert(nots, NotNil)
	c.Assert(nots, HasLen, 1)
	c.Check(nots[0], DeepEquals, []string{string(m1), string(m2)})
}

func (ps *postalSuite) TestNotificationsFailsIfBadArgs(c *C) {
	for i, s := range []struct {
		args []interface{}
		errt error
	}{
		{nil, ErrBadArgCount},
		{[]interface{}{}, ErrBadArgCount},
		{[]interface{}{1}, ErrBadArgType},
		{[]interface{}{"potato"}, ErrBadAppId},
	} {
		reg, err := new(PostalService).popAll(aPackageOnBus, s.args, nil)
		c.Check(reg, IsNil, Commentf("iteration #%d", i))
		c.Check(err, Equals, s.errt, Commentf("iteration #%d", i))
	}
}

func (ps *postalSuite) TestMessageHandlerPublicAPI(c *C) {
	svc := new(PostalService)
	c.Assert(svc.msgHandler, IsNil)
	var ext = &launch_helper.HelperOutput{}
	e := errors.New("Hello")
	f := func(_ *click.AppId, _ string, s *launch_helper.HelperOutput) error { ext = s; return e }
	c.Check(svc.GetMessageHandler(), IsNil)
	svc.SetMessageHandler(f)
	c.Check(svc.GetMessageHandler(), NotNil)
	hOutput := &launch_helper.HelperOutput{[]byte("37"), nil}
	c.Check(svc.msgHandler(nil, "", hOutput), Equals, e)
	c.Check(ext, DeepEquals, hOutput)
}

func (ps *postalSuite) TestPostCallsMessageHandler(c *C) {
	ch := make(chan *launch_helper.HelperOutput)
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.launchers = map[string]launch_helper.HelperLauncher{
		"click": ps.fakeLauncher,
	}
	c.Assert(svc.Start(), IsNil)
	// check the message handler gets called
	f := func(_ *click.AppId, _ string, s *launch_helper.HelperOutput) error { ch <- s; return nil }
	svc.SetMessageHandler(f)
	c.Check(svc.Post(&click.AppId{Click: true}, "thing", json.RawMessage("{}")), IsNil)

	if ps.fakeLauncher.done != nil {
		takeNextBytes(ps.fakeLauncher.ch)

		go ps.fakeLauncher.done("0") // OneDone
	}

	c.Check(takeNextHelperOutput(ch), DeepEquals, &launch_helper.HelperOutput{})
	err := errors.New("ouch")
	svc.SetMessageHandler(func(*click.AppId, string, *launch_helper.HelperOutput) error { return err })
	// but the error doesn't bubble out
	c.Check(svc.Post(&click.AppId{}, "", json.RawMessage("{}")), IsNil)
}

func (ps *postalSuite) TestMessageHandlerPresents(c *C) {
	endp := testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true), uint32(1))
	svc := NewPostalService(nil, ps.log)
	svc.Bus = endp
	svc.EmblemCounterEndp = endp
	svc.HapticEndp = endp
	svc.NotificationsEndp = endp
	svc.URLDispatcherEndp = ps.urlDispBus
	svc.WindowStackEndp = ps.winStackBus
	svc.launchers = map[string]launch_helper.HelperLauncher{}
	c.Assert(svc.Start(), IsNil)

	// Persist is false so we just check the log
	card := &launch_helper.Card{Icon: "icon-value", Summary: "summary-value", Body: "body-value", Popup: true, Persist: false}
	vib := &launch_helper.Vibration{Duration: 500}
	emb := &launch_helper.EmblemCounter{Count: 2, Visible: true}
	output := &launch_helper.HelperOutput{Notification: &launch_helper.Notification{Card: card, EmblemCounter: emb, Vibrate: vib}}
	err := svc.messageHandler(&click.AppId{}, "", output)
	c.Assert(err, IsNil)
	args := testibus.GetCallArgs(endp)
	l := len(args)
	if l < 4 {
		c.Fatal("not enough elements in resposne from GetCallArgs")
	}
	mm := make([]string, 4)
	for i, m := range args[l-4:] {
		mm[i] = m.Member
	}
	sort.Strings(mm)
	// check the Present() methods were called.
	// For dbus-backed presenters, just check the right dbus methods are called
	c.Check(mm, DeepEquals, []string{"::SetProperty", "::SetProperty", "Notify", "VibratePattern"})
	// For the other ones, check the logs
	c.Check(ps.log.Captured(), Matches, `(?sm).* no persistable card:.*`)
	c.Check(ps.log.Captured(), Matches, `(?sm).* no Sound in the notification.*`)
}

func (ps *postalSuite) TestMessageHandlerReportsFailedNotifies(c *C) {
	endp := testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true), 1)
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.NotificationsEndp = endp
	c.Assert(svc.Start(), IsNil)
	card := &launch_helper.Card{Icon: "icon-value", Summary: "summary-value", Body: "body-value", Popup: true}
	notif := &launch_helper.Notification{Card: card}
	output := &launch_helper.HelperOutput{Notification: notif}
	err := svc.messageHandler(&click.AppId{}, "", output)
	c.Assert(err, NotNil)
}

func (ps *postalSuite) TestMessageHandlerInhibition(c *C) {
	endp := testibus.NewTestingEndpoint(condition.Work(true), condition.Work(true), []windowstack.WindowsInfo{{0, "com.example.test_test-app", true, 0}})
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	svc.WindowStackEndp = endp
	c.Assert(svc.Start(), IsNil)
	output := &launch_helper.HelperOutput{} // Doesn't matter
	err := svc.messageHandler(clickhelp.MustParseAppId("com.example.test_test-app_0"), "", output)
	c.Check(err, IsNil)
	c.Check(ps.log.Captured(), Matches, `(?sm).* Notification skipped because app is focused.*`)
}

func (ps *postalSuite) TestMessageHandlerReportsButIgnoresUnmarshalErrors(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Assert(svc.Start(), IsNil)
	output := &launch_helper.HelperOutput{[]byte(`broken`), nil}
	err := svc.messageHandler(nil, "", output)
	c.Check(err, IsNil)
	c.Check(ps.log.Captured(), Matches, "(?msi).*skipping notification: nil.*")
}

func (ps *postalSuite) TestMessageHandlerReportsButIgnoresNilNotifies(c *C) {
	endp := testibus.NewTestingEndpoint(condition.Work(true), condition.Work(false))
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Assert(svc.Start(), IsNil)
	svc.NotificationsEndp = endp
	output := &launch_helper.HelperOutput{[]byte(`{}`), nil}
	err := svc.messageHandler(nil, "", output)
	c.Assert(err, IsNil)
	c.Check(ps.log.Captured(), Matches, "(?msi).*skipping notification: nil.*")
}

func (ps *postalSuite) TestHandleActionsDispatches(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Assert(svc.Start(), IsNil)
	aCh := make(chan *notifications.RawAction)
	rCh := make(chan *reply.MMActionReply)
	bCh := make(chan bool)
	go func() {
		aCh <- nil // just in case?
		aCh <- &notifications.RawAction{Action: "potato://"}
		close(aCh)
		bCh <- true
	}()
	go svc.handleActions(aCh, rCh)
	takeNextBool(bCh)
	args := testibus.GetCallArgs(ps.urlDispBus)
	c.Assert(args, HasLen, 1)
	c.Check(args[0].Member, Equals, "DispatchURL")
	c.Assert(args[0].Args, HasLen, 1)
	c.Assert(args[0].Args[0], Equals, "potato://")
}

func (ps *postalSuite) TestHandleMMUActionsDispatches(c *C) {
	svc := ps.replaceBuses(NewPostalService(nil, ps.log))
	c.Assert(svc.Start(), IsNil)
	aCh := make(chan *notifications.RawAction)
	rCh := make(chan *reply.MMActionReply)
	bCh := make(chan bool)
	go func() {
		rCh <- nil // just in case?
		rCh <- &reply.MMActionReply{Action: "potato://", Notification: "foo.bar"}
		close(rCh)
		bCh <- true
	}()
	go svc.handleActions(aCh, rCh)
	takeNextBool(bCh)
	args := testibus.GetCallArgs(ps.urlDispBus)
	c.Assert(args, HasLen, 1)
	c.Check(args[0].Member, Equals, "DispatchURL")
	c.Assert(args[0].Args, HasLen, 1)
	c.Assert(args[0].Args[0], Equals, "potato://")
}