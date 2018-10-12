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

package service

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/pborman/uuid"

	"github.com/ubports/ubuntu-push/bus"
	"github.com/ubports/ubuntu-push/bus/accounts"
	"github.com/ubports/ubuntu-push/bus/emblemcounter"
	"github.com/ubports/ubuntu-push/bus/haptic"
	"github.com/ubports/ubuntu-push/bus/notifications"
	"github.com/ubports/ubuntu-push/bus/unitygreeter"
	"github.com/ubports/ubuntu-push/bus/windowstack"
	"github.com/ubports/ubuntu-push/click"
	"github.com/ubports/ubuntu-push/click/cnotificationsettings"
	"github.com/ubports/ubuntu-push/launch_helper"
	"github.com/ubports/ubuntu-push/logger"
	"github.com/ubports/ubuntu-push/messaging"
	"github.com/ubports/ubuntu-push/messaging/reply"
	"github.com/ubports/ubuntu-push/nih"
	"github.com/ubports/ubuntu-push/sounds"
	"github.com/ubports/ubuntu-push/urldispatcher"
	"github.com/ubports/ubuntu-push/util"
)

type messageHandler func(*click.AppId, string, *launch_helper.HelperOutput) bool

// a Presenter is something that knows how to present a Notification
type Presenter interface {
	Present(*click.AppId, string, *launch_helper.Notification) bool
}

type notificationCentre interface {
	Presenter
	GetCh() chan *reply.MMActionReply
	RemoveNotification(string, bool)
	Tags(*click.AppId) []string
	Clear(*click.AppId, ...string) int
}

// PostalServiceSetup is a configuration object for the service
type PostalServiceSetup struct {
	InstalledChecker  click.InstalledChecker
	FallbackVibration *launch_helper.Vibration
	FallbackSound     string
}

// PostalService is the dbus api
type PostalService struct {
	DBusService
	mbox          map[string]*mBox
	msgHandler    messageHandler
	launchers     map[string]launch_helper.HelperLauncher
	HelperPool    launch_helper.HelperPool
	messagingMenu notificationCentre
	// the endpoints are only exposed for testing from client
	// XXX: uncouple some more so this isn't necessary
	EmblemCounterEndp bus.Endpoint
	AccountsEndp      bus.Endpoint
	HapticEndp        bus.Endpoint
	NotificationsEndp bus.Endpoint
	UnityGreeterEndp  bus.Endpoint
	WindowStackEndp   bus.Endpoint
	// presenters:
	Presenters    []Presenter
	emblemCounter *emblemcounter.EmblemCounter
	accounts      accounts.Accounts
	haptic        *haptic.Haptic
	notifications *notifications.RawNotifications
	sound         sounds.Sound
	// the url dispatcher, used for stuff.
	urlDispatcher urldispatcher.URLDispatcher
	unityGreeter  *unitygreeter.UnityGreeter
	windowStack   *windowstack.WindowStack
	// fallback values for simplified notification usage
	fallbackVibration *launch_helper.Vibration
	fallbackSound     string
}

var (
	PostalServiceBusAddress = bus.Address{
		Interface: "com.ubuntu.Postal",
		Path:      "/com/ubuntu/Postal",
		Name:      "com.ubuntu.Postal",
	}
)

var (
	SystemUpdateUrl  = "settings:///system/system-update"
	useTrivialHelper = os.Getenv("UBUNTU_PUSH_USE_TRIVIAL_HELPER") != ""
)

// NewPostalService() builds a new service and returns it.
func NewPostalService(setup *PostalServiceSetup, log logger.Logger) *PostalService {
	var svc = &PostalService{}
	svc.Log = log
	svc.Bus = bus.SessionBus.Endpoint(PostalServiceBusAddress, log)
	svc.installedChecker = setup.InstalledChecker
	svc.fallbackVibration = setup.FallbackVibration
	svc.fallbackSound = setup.FallbackSound
	svc.NotificationsEndp = bus.SessionBus.Endpoint(notifications.BusAddress, log)
	svc.EmblemCounterEndp = bus.SessionBus.Endpoint(emblemcounter.BusAddress, log)
	svc.AccountsEndp = bus.SystemBus.Endpoint(accounts.BusAddress, log)
	svc.HapticEndp = bus.SessionBus.Endpoint(haptic.BusAddress, log)
	svc.UnityGreeterEndp = bus.SessionBus.Endpoint(unitygreeter.BusAddress, log)
	svc.WindowStackEndp = bus.SessionBus.Endpoint(windowstack.BusAddress, log)
	svc.msgHandler = svc.messageHandler
	svc.launchers = launch_helper.DefaultLaunchers(log)
	return svc
}

// SetMessageHandler() sets the message-handling callback
func (svc *PostalService) SetMessageHandler(callback messageHandler) {
	svc.lock.RLock()
	defer svc.lock.RUnlock()
	svc.msgHandler = callback
}

// GetMessageHandler() returns the (possibly nil) messaging handler callback
func (svc *PostalService) GetMessageHandler() messageHandler {
	svc.lock.RLock()
	defer svc.lock.RUnlock()
	return svc.msgHandler
}

// Start() dials the bus, grab the name, and listens for method calls.
func (svc *PostalService) Start() error {
	return svc.DBusService.Start(bus.DispatchMap{
		"PopAll":          svc.popAll,
		"Post":            svc.post,
		"ListPersistent":  svc.listPersistent,
		"ClearPersistent": svc.clearPersistent,
		"SetCounter":      svc.setCounter,
	}, PostalServiceBusAddress, svc.init)
}

func (svc *PostalService) init() error {
	actionsCh, err := svc.takeTheBus()
	if err != nil {
		return err
	}
	svc.urlDispatcher = urldispatcher.New(svc.Log)

	svc.accounts = accounts.New(svc.AccountsEndp, svc.Log)
	err = svc.accounts.Start()
	if err != nil {
		return err
	}

	svc.sound = sounds.New(svc.Log, svc.accounts, svc.fallbackSound)
	svc.notifications = notifications.Raw(svc.NotificationsEndp, svc.Log, svc.sound)
	svc.emblemCounter = emblemcounter.New(svc.EmblemCounterEndp, svc.Log)
	svc.haptic = haptic.New(svc.HapticEndp, svc.Log, svc.accounts, svc.fallbackVibration)
	svc.messagingMenu = messaging.New(svc.Log)
	svc.Presenters = []Presenter{
		svc.notifications,
		svc.emblemCounter,
		svc.haptic,
		svc.messagingMenu,
	}
	if useTrivialHelper {
		svc.HelperPool = launch_helper.NewTrivialHelperPool(svc.Log)
	} else {
		svc.HelperPool = launch_helper.NewHelperPool(svc.launchers, svc.Log)
	}
	svc.unityGreeter = unitygreeter.New(svc.UnityGreeterEndp, svc.Log)
	svc.windowStack = windowstack.New(svc.WindowStackEndp, svc.Log)

	go svc.consumeHelperResults(svc.HelperPool.Start())
	go svc.handleActions(actionsCh, svc.messagingMenu.GetCh())
	return nil
}

// xxx Stop() closing channels and helper launcher

// handleactions loops on the actions channels waiting for actions and handling them
func (svc *PostalService) handleActions(actionsCh <-chan *notifications.RawAction, mmuActionsCh <-chan *reply.MMActionReply) {
Handle:
	for {
		select {
		case action, ok := <-actionsCh:
			if !ok {
				break Handle
			}
			if action == nil {
				svc.Log.Debugf("handleActions got nil action; ignoring")
			} else {
				url := action.Action
				// remove the notification from the messaging menu
				svc.messagingMenu.RemoveNotification(action.Nid, true)
				// this ignores the error (it's been logged already)
				svc.urlDispatcher.DispatchURL(url, action.App)
			}
		case mmuAction, ok := <-mmuActionsCh:
			if !ok {
				break Handle
			}
			if mmuAction == nil {
				svc.Log.Debugf("handleActions (MMU) got nil action; ignoring")
			} else {
				svc.Log.Debugf("handleActions (MMU) got: %v", mmuAction)
				url := mmuAction.Action
				// remove the notification from the messagingmenu map
				svc.messagingMenu.RemoveNotification(mmuAction.Notification, false)
				// this ignores the error (it's been logged already)
				svc.urlDispatcher.DispatchURL(url, mmuAction.App)
			}

		}
	}
}

func (svc *PostalService) takeTheBus() (<-chan *notifications.RawAction, error) {
	endps := []struct {
		name string
		endp bus.Endpoint
	}{
		{"notifications", svc.NotificationsEndp},
		{"emblemcounter", svc.EmblemCounterEndp},
		{"accounts", svc.AccountsEndp},
		{"haptic", svc.HapticEndp},
		{"unitygreeter", svc.UnityGreeterEndp},
		{"windowstack", svc.WindowStackEndp},
	}
	for _, endp := range endps {
		if endp.endp == nil {
			svc.Log.Errorf("endpoint for %s is nil", endp.name)
			return nil, ErrNotConfigured
		}
	}

	var wg sync.WaitGroup
	wg.Add(len(endps))
	for _, endp := range endps {
		go func(name string, endp bus.Endpoint) {
			util.NewAutoRedialer(endp).Redial()
			wg.Done()
		}(endp.name, endp.endp)
	}
	wg.Wait()

	return notifications.Raw(svc.NotificationsEndp, svc.Log, nil).WatchActions()
}

func (svc *PostalService) listPersistent(path string, args, _ []interface{}) ([]interface{}, error) {
	app, err := svc.grabDBusPackageAndAppId(path, args, 0)
	if err != nil {
		return nil, err
	}

	tagmap := svc.messagingMenu.Tags(app)
	return []interface{}{tagmap}, nil
}

func (svc *PostalService) clearPersistent(path string, args, _ []interface{}) ([]interface{}, error) {
	if len(args) == 0 {
		return nil, ErrBadArgCount
	}
	app, err := svc.grabDBusPackageAndAppId(path, args[:1], 0)
	if err != nil {
		return nil, err
	}
	tags := make([]string, len(args)-1)
	for i, itag := range args[1:] {
		tag, ok := itag.(string)
		if !ok {
			return nil, ErrBadArgType
		}
		tags[i] = tag
	}
	return []interface{}{uint32(svc.messagingMenu.Clear(app, tags...))}, nil
}

func (svc *PostalService) setCounter(path string, args, _ []interface{}) ([]interface{}, error) {
	app, err := svc.grabDBusPackageAndAppId(path, args, 2)
	if err != nil {
		return nil, err
	}

	count, ok := args[1].(int32)
	if !ok {
		return nil, ErrBadArgType
	}
	visible, ok := args[2].(bool)
	if !ok {
		return nil, ErrBadArgType
	}

	svc.emblemCounter.SetCounter(app, count, visible)
	return nil, nil
}

func (svc *PostalService) popAll(path string, args, _ []interface{}) ([]interface{}, error) {
	app, err := svc.grabDBusPackageAndAppId(path, args, 0)
	if err != nil {
		return nil, err
	}

	svc.lock.Lock()
	defer svc.lock.Unlock()

	if svc.mbox == nil {
		return []interface{}{[]string(nil)}, nil
	}
	appId := app.Original()
	box, ok := svc.mbox[appId]
	if !ok {
		return []interface{}{[]string(nil)}, nil
	}
	msgs := box.AllMessages()
	delete(svc.mbox, appId)

	return []interface{}{msgs}, nil
}

var newNid = uuid.New

func (svc *PostalService) post(path string, args, _ []interface{}) ([]interface{}, error) {
	app, err := svc.grabDBusPackageAndAppId(path, args, 1)
	if err != nil {
		return nil, err
	}
	notif, ok := args[1].(string)
	if !ok {
		return nil, ErrBadArgType
	}
	var dummy interface{}
	rawJSON := json.RawMessage(notif)
	err = json.Unmarshal(rawJSON, &dummy)
	if err != nil {
		return nil, ErrBadJSON
	}

	svc.Post(app, "", rawJSON)
	return nil, nil
}

// Post() signals to an application over dbus that a notification
// has arrived. If nid is "" generate one.
func (svc *PostalService) Post(app *click.AppId, nid string, payload json.RawMessage) {
	if nid == "" {
		nid = newNid()
	}
	arg := launch_helper.HelperInput{
		App:            app,
		NotificationId: nid,
		Payload:        payload,
	}
	var kind string
	if app.Click {
		kind = "click"
	} else {
		kind = "legacy"
	}
	svc.HelperPool.Run(kind, &arg)
}

func (svc *PostalService) consumeHelperResults(ch chan *launch_helper.HelperResult) {
	for res := range ch {
		svc.handleHelperResult(res)
	}
}

func (svc *PostalService) handleHelperResult(res *launch_helper.HelperResult) {
	svc.lock.Lock()
	defer svc.lock.Unlock()
	if svc.mbox == nil {
		svc.mbox = make(map[string]*mBox)
	}

	app := res.Input.App
	nid := res.Input.NotificationId
	output := res.HelperOutput

	appId := app.Original()
	box, ok := svc.mbox[appId]
	if !ok {
		box = new(mBox)
		svc.mbox[appId] = box
	}
	box.Append(output.Message, nid)

	if svc.msgHandler != nil {
		b := svc.msgHandler(app, nid, &output)
		if !b {
			svc.Log.Debugf("msgHandler did not present the notification")
		}
	}

	svc.Bus.Signal("Post", "/"+string(nih.Quote([]byte(app.Package))), []interface{}{appId})
}

func (svc *PostalService) validateActions(app *click.AppId, notif *launch_helper.Notification) bool {
	if notif.Card == nil || len(notif.Card.Actions) == 0 {
		return true
	}
	return svc.urlDispatcher.TestURL(app, notif.Card.Actions)
}

var areNotificationsEnabled = cnotificationsettings.AreNotificationsEnabled

func (svc *PostalService) messageHandler(app *click.AppId, nid string, output *launch_helper.HelperOutput) bool {
	if output == nil || output.Notification == nil {
		svc.Log.Debugf("skipping notification: nil.")
		return false
	}
	// validate actions
	if !svc.validateActions(app, output.Notification) {
		// no need to log, (it's been logged already)
		return false
	}

	locked := svc.unityGreeter.IsActive()
	focused := svc.windowStack.IsAppFocused(app)

	if !locked && focused {
		svc.Log.Debugf("notification skipped because app is focused.")
		return false
	}

	if !areNotificationsEnabled(app) {
		svc.Log.Debugf("notification skipped (except emblem counter) because app has notifications disabled")
		return svc.emblemCounter.Present(app, nid, output.Notification)
	}

	b := false
	for _, p := range svc.Presenters {
		// we don't want this to shortcut :)
		b = p.Present(app, nid, output.Notification) || b
	}
	return b
}
