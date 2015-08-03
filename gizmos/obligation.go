//vi: sw=4 ts=4:
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

	Mnemonic:	obligation
	Abstract:	This is an object that tracks the obligation of something over time.
				An obligation has an overal commence and conclude time (UNIX
				timestamps) and a maximum capacity. The obligation is subdivided
				into time windows between the commence and conclude times with
				each time winodw tracking an obligated capacity. By default, the
				obligation spans from the epoch until well into the future (2025ish);
				there is probably no reason for a user application to change this.

				The obligation now supports the concept of queues associated with
				each timeslice.  This allows the user to further subdivide a slice
				of time based on the 'consumer' of that segment of the slice. Queues
				are actually managed by the time_slice object, but must be recognised
				here so that they can be 'passed through' etc. The use of queues are
				optional; a user programm may need only to treat the obligation as a
				whole value for each timeslice and thus can use the original inc/dec
				functions to manage.  When using queues, the inc/dec functions should
				not be used.

				The obligation 'exposes' queues to a caller using a queue ID (name)
				and reserves a special name, "priority," to generically refer to
				a reserved queue used for priority traffic.  While it shouldn't be
				necessary to know, the priority queue will always map to queue 1 and
				any other named queue will map to 2 through qmax.

	Date:		22 November 2013
	Author:		E. Scott Daniels

	Mods:		11 Feb 2014 : Added support for queues and removed the direct references
					to timeslice fields.
*/

package gizmos

import (
	"fmt"
	"time"
)

const (
	DEF_END_TS = 1735707600		// jan 1, 2025 -- it we're still being used then I'll be surprised!
)

type Obligation struct {
	Max_capacity int64				// the total capacity that any one slice may have assigned
	tslist		*Time_slice
}

// -----------------------------------------------------------------------------------------------------------

/*
	constructor
*/
func Mk_obligation( max_capacity int64 ) (ob *Obligation) {
	ob = &Obligation { }
	ob.Max_capacity = max_capacity
	ob.tslist = Mk_time_slice( 0, DEF_END_TS, 0 )

	return
}

/*
	Destruction.
*/

func (ob *Obligation) Nuke() {
	var (
		nxt	*Time_slice
	)
	for ts := ob.tslist; ts != nil; ts = nxt {
		nxt = ts.Next
		ts.Nuke()
	}
}

/*
	Return the total capacity that this obligation supports.
*/
func (ob *Obligation) Get_max_capacity() ( int64 ) {
	return ob.Max_capacity
}

/*
	Runs the list of timeslices looking for a queue id that is not used across all of the slices. Returns
	the id, or -1 if no id is available. Queue numbers 0 and 1 are reserved and thus are never returned.
*/
func (ob *Obligation) suss_open_qnum( commence int64, conclude int64 ) ( int ) {
	var (
		used	[]byte
	)

	used = make( []byte, 4096 )				// we could use a bit mask to save space, but right now I don't see the need

	for ts := ob.tslist; ts != nil  && !ts.Is_after( conclude ); ts = ts.Next {		// !is_after means conclude is in ts, or before, not just before!
		if ts.Includes ( commence ) || ts.Includes( conclude ) {					// our window overlaps in some manner
			nqueues, qlist := ts.Get_qnums()
			for i := 0; i < nqueues; i++ {
				used[qlist[i]] = 1
			}
		}
	}

	for i := 2; i < len( used ); i++ {
		if used[i] == 0 {
			return i
		}
	}
	
	return -1
}

/*
	Private function that actually does the work, and can accept queue information so that we can use
	it for eaither inc-usage or add queue public functions. Passing in a queue number of 0 will cause the
	amount to be added to an existing queue's amount, and discarded if the queue for qid doesn't exist
	(a function of the underlying time-slice object). If a queue number < 0 is passed in, no effort to
	set/manage queues is made.
*/
func (ob *Obligation) inc_utilisation( commence int64, conclude int64, amt int64, qnum int, qid *string, qswdata *string ) {
	var (
		ts1 *Time_slice = nil		// temp hold of timeslice for various reasons
	)

	for ts := ob.tslist; ts != nil; ts = ts.Next {
		if !ts.Is_before( commence ) {					// only consider slices that overlap or are after the given window

			if ts.Includes( commence ) {					// starts in this block
				ts1, ts = ts.Split( commence )				// split and leave ts set to the first slice of the given window
				if ts == nil {								// if commence and start of ts matched, there is no split, so pick up the original slice again
					ts = ts1
				}
			}

			if  ts.Includes( conclude ) {					// our end is inside this block, split it off, and inc just the frist portion
				ts1, _ = ts.Split( conclude+1 )				// split so that conclude time is in the slice, not first of next; we can safely ignore the latter slice
				if ts1 != nil {								// if this slice already ends on conclude, ts1 will be nil, otherwise we advance to the new block
					ts = ts1
				}	
				if qnum >= 0 {
					ts.Add_queue( qnum, qid, qswdata, amt )	// adds the queue if qid does not exist, else it increases the amount
				}

				ts.Amt += amt								// increase just the early part; capaccity past the split point remains the same
				if ts.Amt < 0 {								// if decrementing don't allow it to go neg
					ts.Amt = 0
				}
				return
			}

			ts.Amt += amt;			// this is either the slice split at the commence point, or a slice that is included in the entire commence/conclude widow
			if ts.Amt < 0 {			// if decrementing don't allow it to go neg
				ts.Amt = 0
			}
			if qnum >= 0 {
				ts.Add_queue( qnum, qid, qswdata, amt )	// adds the queue if qid does not exist, else it increases the amount
			}

			ts1 = ts										// must hold last block in case we fall out of loop
		}
	}

	// if we get here, the concluding time is > the last tslice on the list; extend it's time (cap has already been increased)
	ts1.Extend( conclude )
	return
}

/*
	Runs the list of time slices, and increases the amount used by amt.
	This function assumes that the capacity has been vetted already and thus makes no checks
	to see if the increase takes a timeframe beyond the obligation.
*/
func (ob *Obligation) Inc_utilisation( commence int64, conclude int64, amt int64 ) {
	ob.inc_utilisation( commence, conclude, amt, -1, nil, nil )
}

/*
	Decreases the capacity of a link's time window by the value of dec_cap.
*/
func (ob *Obligation) Dec_utilisation( commence int64, conclude int64, dec_cap int64 ) {
	ob.inc_utilisation( commence, conclude, -dec_cap, -1, nil, nil )
}

/*
	Runs the list of time slices and returns true if the capacity increase (amt) can
	be satisifed across the given time window.
*/
func (ob *Obligation) Has_capacity( commence int64, conclude int64, amt int64 ) ( result bool ) {
	var (
		ts *Time_slice
	)
		
	ts = ob.tslist
	if ts.Is_before( time.Now().Unix() ) {					// if first block is completely before the current time
		ob.Prune( )											// prune out what we can
	}

	result = true
	for ts = ob.tslist; ts != nil; ts = ts.Next {
		if ts.Is_after( conclude ) {					// reached the end of slices that could overlay the window
			return
		}

		if ts.Overlaps( commence, conclude ) {
			if ts.Amt + amt > ob.Max_capacity {		
				result = false;	
				return
			}
		}
	}

	return					// assume that the last block in the list ends earlier than the conclusion passed in
}

/*
	Adds a queue to the obligation starting with the commence and ending with the conclude timestamps.
	This function does NOT check to see if the obligaion can support the amount being added assuming that
	the user has done this during path discovery or some other determination that this obligation needs to
	be used.  swdata is a string that provides switch and port data to what ever mechanism is actually
	adjusting the switch and thus needs to know switch/port and maybe more.  The format of the string isn't
	important to the obligation.
*/
func (ob *Obligation) Add_queue( qid *string, swdata *string,  amt int64, commence int64, conclude int64 ) ( err error ) {
	var (
		qnum int
	)

	if (*qid)[:8] == "priority" { 			// allow for priority-in and priority-out designations to map to queue 1
		qnum = 1
	} else {
		qnum = ob.suss_open_qnum( commence, conclude )				// we'll assign this number to the queue across all timeslices
	}

	if qnum < 1 {
		err = fmt.Errorf( "unable to add queue to obligation, no available queue numbers: %s", *qid )
		return
	}

	err = nil
	ob.inc_utilisation( commence, conclude, amt, qnum, qid, swdata )

	return	
}

/*
	Increase the amount assigned to the queue. If the queue ID isn't known to the obilgation
	then no action will be taken (a function of the underlying time_slice object).
*/
func (ob *Obligation) Inc_queue( qid *string, amt int64, commence int64, conclude int64 ) {
	ob.inc_utilisation( commence, conclude, amt, 0, qid, nil )
}

/*
	Decrease the amount assigned to the queue. If the queue ID isn't known to the obilgation
	then no action will be taken (a function of the underlying time_slice object).
*/
func (ob *Obligation) Dec_queue( qid *string, amt int64, commence int64, conclude int64 ) {
	ob.inc_utilisation( commence, conclude, -amt, 0, qid, nil )
}


/*
	run the timeslice list and prune away any leading blocks that are in the past
*/
func (ob *Obligation) Prune( ) {
	var(
		ts *Time_slice
		nxt *Time_slice
		now int64
	)

	now = time.Now().Unix();	
	for ts = ob.tslist; ts != nil && ts.Is_before( now ); ts = nxt {
		nxt = ts.Next

		if nxt != nil {				// remove the block from the list
			nxt.Prev = nil
		}

		ts.Nuke()
		ob.tslist = nxt			// must advance the head of the list
	}

	return
 }

/*
	return the obligation for the indicated time
*/
func ( ob *Obligation ) Get_allocation( utime int64 ) ( int64 ) {
	for ts := ob.tslist; ts != nil; ts = ts.Next {						// run the time slice list looking for the one that contains utime
		if ts.Includes( utime ) {
			return ts.Amt
		}
	}

	return 0
}

/*
	Returns the maximum amount obligated for any timeslice that hasn't expired.
*/
func ( ob *Obligation ) Get_max_allocation( ) ( int64 ) {
	var max int64 = 0
	
	now := time.Now().Unix();	

	for ts := ob.tslist; ts != nil; ts = ts.Next {						// run the time slice list looking for the one that contains utime
		if ! ts.Is_before( now ) && ts.Amt > max {
			max =  ts.Amt
		}
	}

	return max
}


/*
	Returns the queue number for the queue that has the given ID at the indicated time. If no
	such queue exists, then 0 (best effort queue) is returned.
*/
func (ob *Obligation) Get_queue( qid *string, tstamp int64 ) ( qnum int ) {

	qnum = 0

	for ts := ob.tslist; ts != nil && qnum == 0;  ts = ts.Next {
		if ts.Includes( tstamp ) {
			qnum, _ = ts.Get_queue_info( qid )				// ignore switch id info, we don't need that
		}
	}

	if qnum <= 0 {			// get_queue_info returns -1 if qid isn't known, flip to 0
		qnum = 0
	}

	return
}

// ----------------- json and string things -------------------------------

/*
	Find the timeslice that contains the timestamp passed in, and then use it to generate a string
	of queue information that should be useful in setting and/or managing real switch queues.
*/
func (ob *Obligation) Queues2str( usr_ts int64 ) ( string ) {
	if ob == nil {
		return ""
	}

	for ts := ob.tslist; ts != nil; ts = ts.Next {
		if ts.Includes( usr_ts ) {
			return ts.Queues2str( )
		}
	}

	return ""
}

/*
	Generate a json blob that represents the obligation. The json will list the max capacity
	for the obligation and then an entry for each timeslice.
*/
func (ob *Obligation) To_json( ) ( s string ) {
	var (
		ts *Time_slice
	)

	s = fmt.Sprintf( `{ "max_capacity": %d, "timeslices": [ `, ob.Max_capacity )

	for ts = ob.tslist; ts != nil; ts = ts.Next {
		s += fmt.Sprintf( "%s", ts.To_json( ) )
		if ts.Next != nil {
			s += ","
		}
	}

	s += fmt.Sprintf( " ] }" )

	return
}
