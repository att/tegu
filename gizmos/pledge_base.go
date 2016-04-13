// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


/*

	Mnemonic:	pledge base struct
	Abstract:	This defines the basic data members and functions that are included in
				all pledge structs.  It implements most, but not all of, the Pledge interface.

				Pledge types should include an anonymous, unnamed Pledge_base as the first
				element of their defining type, in order to pull in these fields and functions.

				Note: pledge_window could probably be combined with this file now, but as
				the main point of this exercise was to remove duplicated functions, I will
				leave that separate for now.

	Date:		16 Aug 2015
	Author:		E. Scott Daniels / Robert Eby

	Mods:		12 Apr 2016 - Duplicate refresh support.
*/

package gizmos

type Pledge_base struct {
	id			*string			// name that the client can use to manage (modify/delete)
	window		*pledge_window	// the window of time for which the pledge is active
	pushed		bool			// set when pledge has been pushed into openflow or openvswitch
	paused		bool			// set if reservation has been paused
	usrkey		*string			// a 'cookie' supplied by the user to prevent any other user from modifying
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
*/
func (p *Pledge_base) Concluded_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.concluded_recently( window )
}

/*
	Returns true if pledge started recently (between now and now - window seconds) and
	has not expired yet. If the pledge started within the window, but expired before
	the call to this function false is returned.
*/
func (p *Pledge_base) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.commenced_recently( window )
}

/*
	Returns a pointer to the ID string of the pledge.
*/
func (p *Pledge_base) Get_id( ) ( *string ) {
	if p == nil {
		return nil
	}
	return p.id
}

/*
	Return the commence and expiry times.
*/
func (p *Pledge_base) Get_window( ) ( int64, int64 ) {
	if p == nil {
		return 0, 0
	}
	return p.window.get_values()
}

/*
	Returns true if the pledge is currently active (the commence time is <= than the current time
	and the expiry time is > the current time.
*/
func (p *Pledge_base) Is_active( ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_active()
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds have passed.
*/
func (p *Pledge_base) Is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_active_soon( window )
}

/*
	Returns true if the pledge has expired (the current time is greater than
	the expiry time in the pledge).
*/
func (p *Pledge_base) Is_expired( ) ( bool ) {
	if p == nil {
		return true
	}
	return p.window.is_expired()
}

/*
	Returns true if pledge expired long enough ago that it can safely be discarded.
	The window is the number of seconds that the pledge must have been expired to
	be considered extinct.
*/
func (p *Pledge_base) Is_extinct( window int64 ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_extinct( window )
}

/*
	Returns true if the pledge has not become active (the commence time is >= the current time).
*/
func (p *Pledge_base) Is_pending( ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_pending()
}

/*
	Returns true if the pushed flag has been set to true.
*/
func (p *Pledge_base) Is_pushed( ) (bool) {
	if p == nil {
		return false
	}
	return p.pushed
}

/*
	Returns true if the reservation is paused.
*/
func (p *Pledge_base) Is_paused( ) ( bool ) {
	if p == nil {
		return false
	}
	return p.paused
}

/*
	Check the cookie passed in and return true if it matches the cookie on the
	pledge.
*/
func (p *Pledge_base) Is_valid_cookie( c *string ) ( bool ) {
	if p == nil || c == nil {
		return false
	}
	return *c == *p.usrkey
}

// There is NOT a toggle pause on purpose; don't add one :)

/*
	Puts the pledge into paused state and optionally resets the pushed flag.
*/
func (p *Pledge_base) Pause( reset bool ) {
	if p != nil {
		p.paused = true
		if reset {
			p.pushed = false;
		}
	}
}

/*
	Puts the pledge into an unpaused (normal) state and optionally resets the pushed flag.
*/
func (p *Pledge_base) Resume( reset bool ) {
	if p != nil {
		p.paused = false
		if reset {
			p.pushed = false;
		}
	}
}

/*
	Sets a new expiry value on the pledge.
*/
func (p *Pledge_base) Set_expiry ( v int64 ) {
	if p != nil {
		p.window.set_expiry_to( v )
		p.pushed = false		// force it to be resent to adjust times
	}
}

/*
	Sets the pushed flag to true.
*/
func (p *Pledge_base) Set_pushed( ) {
	if p != nil {
		p.pushed = true
	}
}

/*
	Resets the pushed flag to false.
*/
func (p *Pledge_base) Reset_pushed( ) {
	if p != nil {
		p.pushed = false
	}
}

/*
	
*/
func (p *Pledge_base) Same_anchors( a1 *string, a2 *string ) (bool ) {
	return false;
}
