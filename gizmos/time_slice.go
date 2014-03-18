// vi: sw=4 ts=4:

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
	//"os"
	//"strings"
	"time"

	//"forge.research.att.com/gopkgs/clike"
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

	ts.queues = make( map[string]*Queue, 10 )
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

	// it is NOT ok to share queues across time slices.
	ts2.queues = make( map[string]*Queue, len( ts1.queues ) )		// copy the existing references to queues
	for i := range ts1.queues {
		ts2.queues[i] = ts1.queues[i].Clone( )
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

	if q := ts.queues[*id]; q != nil {
		//fmt.Fprintf( os.Stderr, "timeslice:add_queue: inc existing  %d-%d qnum=%d id=%s swdata=%s amt=%dM\n", ts.commence, ts.conclude, qnum, *id, *swdata, amt/1000000 )
		q.Inc( amt )
	} else {
		//fmt.Fprintf( os.Stderr, "timeslice:add_queue: add new %d-%d qnum=%d id=%s swdata=%s amt=%dM\n", ts.commence, ts.conclude, qnum, *id, *swdata, amt/1000000 )
		if qnum > 0 {						// we allow a queue num of zero as the means to incr an existing queue, but we never create one with 0
			ts.queues[*id] = Mk_queue( amt, id, qnum, 200, swdata )
		}
	}
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
		s += fmt.Sprintf( `%s%s`, sep, ts.queues[i].To_str() )
		sep = " "
	}

	return s
}

func (ts *Time_slice) To_json( ) ( string ) {
	return fmt.Sprintf( `{ "commence": %d, "conclude": %d, "amt": %d, "queues": [ %s ] }`, ts.commence, ts.conclude, ts.Amt, ts.Queues2json() )
}
