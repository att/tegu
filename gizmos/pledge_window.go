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

	Mnemonic:	pledge_window
	Abstract:	Struct that manages a window of time for any pledge type and the
				functions which make testing times against the window easier.
				Pledge times are managed at the second level; no need for more
				precision for this. This structure is local to gizmos so nothing
				should be visible to the outside world.

	Date:		20 May 2015
	Author:		E. Scott Daniels

	Mods:		28 Jul 2015 : Added upper bounds check for Expiry time.
				01 Dec 2015 : Added datacache tags and made fields externally 
					visible so that the datacache package can convert the struct
					to a map.
*/

package gizmos

import (
	"os"
	"fmt"
	"time"
)

type pledge_window struct {
	Commence	int64	`dcache:"_"`
	Expiry		int64	`dcache:"_"`
}

/*
	Make a new pledge_window. If the Commence time is earlier than now, it is adjusted
	to be now.  If the expry time is before the adjusted Commence time, then a nil
	pointer and error are returned.
*/
func mk_pledge_window( Commence int64, Expiry int64 ) ( pw *pledge_window, err error ) {
	now := time.Now().Unix()
	err = nil
	pw = nil

	if Commence < now {
		Commence = now
	}

	if Expiry < Commence {
		err = fmt.Errorf( "bad Expiry submitted, already expired: now=%d Expiry=%d", now, Expiry );
		obj_sheep.Baa( 2, "pledge: %s", err )
		return
	}

	if ! Valid_obtime( Expiry ) {			// if Expiry is less than max obligation time
		err = fmt.Errorf( "bad Expiry submitted, too far in the future: Expiry=%d", Expiry );
		obj_sheep.Baa( 2, "pledge: %s", err )
		return
	}

	pw = &pledge_window {
		Commence: Commence,
		Expiry: Expiry,
	}

	return
}

/*
	Adjust window. Returns a valid Commence time (if earlier than now) or 0 if the
	time window is not valid.
func adjust_window( Commence int64, conclude int64 ) ( adj_start int64, err error ) {

	now := time.Now().Unix()
	err = nil

	if Commence < now {				// ajust forward to better play with windows on the paths
		adj_start = now
	} else {
		adj_start = Commence
	}

	if conclude <= adj_start {						// bug #156 fix
		err = fmt.Errorf( "bad Expiry submitted, already expired: now=%d Expiry=%d", now, conclude );
		obj_sheep.Baa( 2, "pledge: %s", err )
		return
	}

	return
}
*/

func (p *pledge_window) clone( ) ( npw *pledge_window ) {
	if p == nil {
		return nil
	}

	npw = &pledge_window {
		Expiry: p.Expiry,
		Commence: p.Commence,
	}

	return
}

/*
	Return the state as a string and the amount of time in the
	past (seconds) that the pledge expired, or the amount of
	time in the future that the pledge will be active. Caption
	is a string such as "ago"  that can be used following the value
	if needed.
*/
func (p *pledge_window) state_str( ) ( state string, caption string, diff int64 ) {
	if p == nil {
		return "EXPIRED", "no window", 0
	}

	now := time.Now().Unix()

	if now >= p.Expiry {
		state = "EXPIRED"
		caption = "ago"
	} else {
		if now < p.Commence {
			state = "PENDING"
			diff = p.Commence - now
			caption = "from now"
		} else {
			state = "ACTIVE"
			diff = p.Expiry -  now
			caption = "remaining"
		}
	}

	return state, caption, diff
}

/*
	Extend the Expiry time by n seconds. N may be negative and will not set the
	Expiry time earlier than now.
*/
func (p *pledge_window) extend_by( n int64 ) {
	if p == nil {
		return
	}

	p.Expiry += n;

	if n < 0 {
		now := time.Now().Unix()
		if p.Expiry < now {
			p.Expiry = now
		}
	}
}

/*
	Set the Expiry time to the timestamp passed in.
	It is valid to set the Expiry time to a time before the current time.
*/
func (p *pledge_window) set_expiry_to( new_time int64 ) {
	p.Expiry = new_time;
}

/*
	Returns true if the pledge has expired (the current time is greather than
	the Expiry time in the pledge).
*/
func (p *pledge_window) is_expired( ) ( bool ) {
	if p == nil {
		return true
	}

	return time.Now().Unix( ) >= p.Expiry
}

/*
	Returns true if the pledge has not become active (the Commence time is >= the current time).
*/
func (p *pledge_window) is_pending( ) ( bool ) {
	if p == nil {
		return false
	}
	return time.Now().Unix( ) < p.Commence
}

/*
	Returns true if the pledge is currently active (the Commence time is <= than the current time
	and the Expiry time is > the current time.
*/
func (p *pledge_window) is_active( ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return p.Commence < now  && p.Expiry > now
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds (window) have passed.
*/
func (p *pledge_window) is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return (p.Commence >= now) && p.Commence <= (now + window)
}

func (p *pledge_window) get_values( ) ( Commence int64, Expiry int64 ) {
	if p == nil {
		return 0, 0
	}

	return p.Commence, p.Expiry
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
	If pledge_window is nil, then we return true.
*/
func (p *pledge_window) concluded_recently( window int64 ) ( bool ) {
	if p == nil {
		return true
	}

	now := time.Now().Unix()
	return (p.Expiry < now)  && (p.Expiry >= now - window)
}

/*
	Returns true if pledge started recently (between now and now - window seconds) and
	has not expired yet. If the pledge started within the window, but expired before
	the call to this function false is returned.
*/
func (p *pledge_window) commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return (p.Commence >= (now - window)) && (p.Commence <= now ) && (p.Expiry > now)
}

/*
	Returns true if pledge expired long enough ago, based on the window timestamp
	passed in,  that it can safely be discarded.  The window is the number of
	seconds that the pledge must have been expired to be considered extinct.
*/
func (p *pledge_window) is_extinct( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return p.Expiry <= now - window
}

/*
	Test this window (p) against a second window (p2) to see if they overlap.
	Windows where Commence is equal to Expiry, or Expiry is equal to Commence
	(6, and 8 below) are not considered overlapping.

             pc|---------------------------------|pe
               .                                 .
   T   p2c|----.------|p2e                       .             (1)
   T           .p2c|-----------|p2e              .             (2)
   T           .                        p2c|-----.-----|p2e    (3)
   T   p2c|----.---------------------------------.----|p2e     (4)
   T        p2c|---------------------------------|p2e          (5)
   F           .                              p2c|--------|p2e (6)
   F  p2c|--|  .                                 .             (7)
   F  p2c|-----|                                 .             (8)
   F           .                                 .  p2c|--|p2e (9)
*/
func (p *pledge_window) overlaps( p2 *pledge_window ) ( bool ) {
	if p == nil || p2 == nil {
fmt.Fprintf( os.Stderr, ">>>> one/both are nil %v | %v\n", p, p2 )
		return false
	}

	if p2.Commence >= p.Commence  &&  p2.Commence < p.Expiry {	//(2,3)
		return true;
	}

	if p2.Expiry > p.Commence  &&  p2.Expiry <= p.Expiry {		//(1,2)
		return true;
	}

	if p2.Commence <= p.Commence  &&  p2.Expiry >= p.Expiry {	//(4,5)
		return true;
	}

	return false
}
