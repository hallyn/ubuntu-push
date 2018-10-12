/*
 Copyright 2014-2016 Canonical Ltd.

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

// Package haptic can present notifications as a vibration pattern
// using the usensord/haptic interface
package haptic

import (
	"github.com/ubports/ubuntu-push/bus"
	"github.com/ubports/ubuntu-push/bus/accounts"
	"github.com/ubports/ubuntu-push/click"
	"github.com/ubports/ubuntu-push/click/cnotificationsettings"
	"github.com/ubports/ubuntu-push/launch_helper"
	"github.com/ubports/ubuntu-push/logger"
)

// usensord/haptic lives on a well-knwon bus.Address
var BusAddress bus.Address = bus.Address{
	Interface: "com.canonical.usensord.haptic",
	Path:      "/com/canonical/usensord/haptic",
	Name:      "com.canonical.usensord",
}

// Haptic encapsulates info needed to call out to usensord/haptic
type Haptic struct {
	bus      bus.Endpoint
	log      logger.Logger
	acc      accounts.Accounts
	fallback *launch_helper.Vibration
}

// New returns a new Haptic that'll use the provided bus.Endpoint
func New(endp bus.Endpoint, log logger.Logger, acc accounts.Accounts, fallback *launch_helper.Vibration) *Haptic {
	return &Haptic{endp, log, acc, fallback}
}

var vibrateInSilentMode = cnotificationsettings.VibrateInSilentMode
var canUseVibrationsNotify = cnotificationsettings.CanUseVibrationsNotify

// Present presents the notification via a vibrate pattern
func (haptic *Haptic) Present(app *click.AppId, nid string, notification *launch_helper.Notification) bool {
	if notification == nil {
		panic("please check notification is not nil before calling present")
	}

	if !canUseVibrationsNotify(app) {
		haptic.log.Debugf("[%s] vibrate disabled by user for this app.", nid)
		return false
	}

	if haptic.acc.SilentMode() {
		if !vibrateInSilentMode() {
			haptic.log.Debugf("[%s] vibrate disabled by user when in Silent Mode.", nid)
			return false
		}
	}

	if !haptic.acc.Vibrate() {
		haptic.log.Debugf("[%s] vibrate disabled by user.", nid)
		return false
	}

	vib := notification.Vibration(haptic.fallback)
	if vib == nil {
		haptic.log.Debugf("[%s] notification has no Vibrate.", nid)
		return false
	}
	pattern := vib.Pattern
	repeat := vib.Repeat
	if repeat == 0 {
		repeat = 1
	}
	if len(pattern) == 0 {
		haptic.log.Debugf("[%s] not enough information in the Vibrate to create a pattern", nid)
		return false
	}
	haptic.log.Debugf("[%s] vibrating %d times to the tune of %v", nid, repeat, pattern)
	err := haptic.bus.Call("VibratePattern", bus.Args(pattern, repeat))
	if err != nil {
		haptic.log.Errorf("[%s] call to VibratePattern returned %v", nid, err)
		return false
	}
	return true
}
