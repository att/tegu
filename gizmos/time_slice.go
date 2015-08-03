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

	Mnemonic:	time_slice
	Abstract:	Represents a slice in time with for which a single value (amt)
				is known.   The amount is subdivided into queues which represent
				a fraction of the total amount that has been pledged to a particular
				host's traffic.

				The object is designed to be managed as a link list member. The
				'owner' of the object may arrange the list as needed and methods like
				split() will automatically insert new time_slice objects into the list
				when needed. Similarly, Amt is exported so that the object owner may
				easily manipulate it without a method call.

				The timeslice may optionally be given queue information.  Adding a
				queue to the set will increase the timeslice's amount used (amt). Removing
				a queue will decrement the amount used.  Queue numbering is tricky because
				it is desired to keep the same queue number for a port/host from the
				start until the end of the reservation for the host which may span more
				than one timeslice.

	Date:		23 November 2013
	Author:		E. Scott Daniels

	Mods:		29 Jun 2014 - Changes to support user link limits.
				07 Jul 2014 - Now generates queue strings if the bandwidth amount is
					greater than zero.
				18 Jun 2015 - Allow a queue to be added only if the amount is positive.
				22 Jun 2015 - Added check for nil qid pointer on add.
*/

package gizmos

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	//"strings"
	"time"

	//"github.com/att/att/gopkgs/clike"
)

/*
	defines a time window for which an amount of has been pledged.
*/
type Time_slice struct {
	Next		*Time_slice	// next/prev must be exported to allow wrapper the ability to adjust
	Prev		*Time_slice
	Amt			int64			// amount promissed for this time period	
	commence	int64			// start timestamp (UNIX) of the time period
	conclude	int64			// ending timestamp
	queues		map[string]*Queue			// list of queues that further define the slice
	limits		map[string]*Fence			// user fences that limit their capacity on the link
}

/*
	constructor
*/
func Mk_time_slice( commence int64, conclude int64, amt int64 ) ( ts *Time_slice ) {
	ts = &Time_slice {
		commence:	commence,
		conclude:	conclude,
		Amt:		amt,
		Next:		nil,
		Prev:		nil,
	}

	ts.queues = make( map[string]*Queue, 10 )	// default values are suggestions, not hard limits
	ts.limits = make( map[string]*Fence, 10 )
	return
}

/*
	Destruction.
*/
func (ts *Time_slice) Nuke( ) {
	ts.Next = nil
	ts.Prev = nil
	ts.queues = nil
}


/*
	Split a time slice on a given time boundary. The new object is inserted
	into the list. The return values are ts1 and ts2 where ts1 is the timeslice
	occuring chronologicly before ts2. The queues will be shared between the
	two slices. The split point becomes the commence time for the inserted
	block. For example, if the slice spans time 100 to 500 and the split at
	200 is requested, the existing block will span 100 to 199, and the inserted
	block will span 200 to 500.

	If the split point is exactly equal to the start/end timestamp for to, then
	no action is taken and only ts1 is returned (ts2 will be nil).

	If the split point is not inside of the timeslice referenced then two
	nil pointers are returned.
*/
func (ts *Time_slice) Split( split_pt int64 ) ( ts1, ts2 *Time_slice ) {
	ts1 = nil
	ts2 = nil

	if split_pt < ts.commence || split_pt > ts.conclude {
		return
	}

	ts1 = ts
	if ts.commence == split_pt  || ts.conclude == split_pt {
		return;	
	}

	ts2 = Mk_time_slice( split_pt, ts.conclude, ts.Amt )

	ts2.commence = split_pt			// adjust the time window of each
	ts1.conclude = split_pt - 1

	ts2.Next = ts1.Next				// add new block to the list
	if ts2.Next != nil {
		ts2.Next.Prev = ts2
	}
	ts2.Prev = ts1
	ts1.Next = ts2

	// it is NOT ok to share queues or limits across time slices, so copy them.
	ts2.queues = make( map[string]*Queue, len( ts1.queues ) )
	for i := range ts1.queues {
		ts2.queues[i] = ts1.queues[i].Clone( )
	}

	ts2.limits = make( map[string]*Fence, len( ts1.limits ) )
	for k :=range ts1.limits {
		ts2.limits[k] = ts1.limits[k].Clone( 0 )		// fences already in limits have been adjusted, so no need to pass capacity
	}

	return
}

/*
	change the concluding time. we will vet it to ensure that it's not
	before the commence time and is the last block as that is the only
	one allowed to have it's time extended.
*/
func (ts *Time_slice) Extend( timestamp int64 ) {
	if( timestamp > ts.commence && ts.Next == nil ) {
		ts.conclude = timestamp
	}
}

/*
	Check a timestamp against this timeslice true if the timestamp is contained within the timeslice.
*/
func (ts *Time_slice) Includes( timestamp int64 ) ( bool ) {
	//fmt.Fprintf( os.Stderr, "start=%d timestamp=%d end=%d   %v\n",  ts.commence, timestamp,  ts.conclude, ts.commence <= timestamp  &&  ts.conclude >= timestamp )
	return ts.commence <= timestamp  &&  ts.conclude >= timestamp
}

/*
	Return true if the slice is completely before the given timestamp.
*/
func (ts *Time_slice) Is_before( timestamp int64 ) (  bool ) {
	return  ts.conclude < timestamp
}

/*
	Return true if the slice is completely after the given timestamp.
*/
func (ts *Time_slice) Is_after( timestamp int64 ) (  bool ) {
	return  ts.commence > timestamp
}

/*
	Return true if the time window passed in overlaps with this slice.
*/
func (ts *Time_slice) Overlaps( wstart int64, wend int64 ) ( bool ) {
	return ts.Includes( wstart ) || ts.Includes( wend )	
}

/*
	Extracts the queue numbers from each queue in the list and builds an
	array with the numbers. Returns the list and the number of queues
	that were present.
*/
func (ts *Time_slice) Get_qnums( ) ( nqueues int, list []int ) {

	nqueues = 0

	list = make( []int, len( ts.queues ) )
	for i := range ts.queues {
		if n := ts.queues[i].Get_num(); n > 0 {
			list[nqueues] = n
			nqueues++
		}	
	}

	return
}

/*
	Check to see if the capacity provided for the named user would bust the user's limit.
	Returns true if the capacity can be added without going over the fence.

	If the user is not in the set of users which have a reservation over this time_slice,
	then we return true with the assumption that some earlier process has checked the
	user's request against either the default cap, or the user's cap; this saves us from
	having to install every user into the has everytime a timeslice is created.
*/
func (ts *Time_slice) Has_usr_capacity( usr *string, cap int64 ) ( result bool, err error ) {
	if ts.limits != nil  &&  usr != nil {
		if f, ok := ts.limits[*usr]; ok {
			if ! f.Has_capacity( cap ) {
				have, need := f.Get_have_need( cap )		// what do we have and what would be needed
				err = fmt.Errorf( "user exceeds limit: need %d have %d", have, need )
				return false, err
			}
		}
	}

	return true, nil
}

/*
	Return queue info for the queue matching the ID passed in.
*/
func (ts *Time_slice) Get_queue_info( id *string ) ( qnum int, swdata *string ) {
	qnum = -1
	swdata = nil

	q := ts.queues[*id]
	if q != nil {
		qnum = q.Get_num( )
		swdata = q.Get_eref()			// get the switch data (external reference in queue terms)
	}

	return
}

/*
	Add a queue to the slice. If the queue id exsits, then we'll inc the amount already set
	by amt rather than creating a new one.
*/
func (ts *Time_slice) Add_queue( qnum int, id *string, swdata *string, amt int64 ) {
	if ts == nil {
		return
	}

	if id == nil {
		obj_sheep.Baa( 1, "timeslice/add_queue: internal mishap: nil qid for q=%d amt=%d", qnum, amt )
		return
	}

	if q := ts.queues[*id]; q != nil {
		q.Inc( amt )
	} else {
		if amt > 0 {							// allow it to be adjusted by negative amount, but don't create this way
			if qnum > 0 {						// we allow a queue num of zero as the means to incr an existing queue, but we never create one with 0
				ts.queues[*id] = Mk_queue( amt, id, qnum, 200, swdata )
			}
		}
	}
}

/*
	Increases the amount consumed by the user during this timeslice. The usr in this
	case is a fence containing default values should we need to create a new fence for
	the user in this slice.  This function does NOT check to see if the increase would
	bust the limit, but the underlying fence might chop the requested capacity at the limit.
*/
func (ts *Time_slice) Inc_usr(	usr *Fence, amt int64, cap int64  ) {
	if usr == nil {
		return
	}

	f := ts.limits[*usr.Name]
	if f == nil {
		f = usr.Clone( cap )
		ts.limits[*f.Name] = f
	}

	f.Inc_used( amt )
}

// --------------- string and json support ---------------------------------------------------------

func (ts *Time_slice) To_str( ) ( string ) {
	s := time.Unix( ts.commence, 0 )
	e := time.Unix( ts.conclude, 0 )
	return fmt.Sprintf( "from %s to %s: %d", s.Format( time.RFC822Z ), e.Format( time.RFC822Z ), ts.Amt )
}

/*
	Generates a string of json information that describes each queue in the timeslice.
*/
func ( ts *Time_slice ) Queues2json( ) ( string ) {
	s := ""
	sep := ""

	for i := range ts.queues {
		s += fmt.Sprintf( `%s%s`, sep, ts.queues[i].To_json() )
		sep = ","
	}

	return s
}

/*
	Generates a set of newline separated information about each queue in the timeslice.
*/
func ( ts *Time_slice ) Queues2str( ) ( string ) {
	s := ""
	sep := ""

	for i := range ts.queues {
		qs := ts.queues[i].To_str_pos( ) 				// queue string only if it's a positive value (allow queues with zero size to drop off)
		if qs != "" {
			s += fmt.Sprintf( `%s%s`, sep, qs )
			sep = " "
		}
	}

	return s
}

func (ts *Time_slice) To_json( ) ( string ) {
	jstr := fmt.Sprintf( `{ "commence": %d, "conclude": %d, "amt": %d, "fences": [`, ts.commence, ts.conclude, ts.Amt )
	sep := " "
	for _, v := range ts.limits {
		jstr += fmt.Sprintf( `%s%s`, sep, v.To_json( ) )
		sep = ","
	}

	jstr += fmt.Sprintf( `], "queues": [ %s ] }`, ts.Queues2json() )

	return jstr
}
