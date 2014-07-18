// vi: sw=4 ts=4:

/*

	Mnemonic:	queue
	Abstract:	Represents a queue, mapping it to a source host and 
				a specific bandwidth maximum.

	Date:		06 February 2014
	Author:		E. Scott Daniels

	Mods:		07 Jul 2014 - Added To_str_pos() function to generate strings
					only if the bandwidth for the queue is greater than zero.
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
	//"time"

	//"forge.research.att.com/gopkgs/clike"
)

type Queue struct {
	Id			*string			// the id of the queue; likely a host/VM name, mac, or ip or vm1-vm2 pair
	bandwidth	int64			// bandwidth associated with the queue
	pri			int				// priority given to ovs when setting queues	
	qnum		int				// the queue number (we cannot depend on ordering)
	exref		*string			// switch/port (other info?) that queue setting function will need 
}

/*
	constructor
*/
func Mk_queue( bw int64, id *string, num int, priority int, ref_data *string ) ( q *Queue ) {
	q = &Queue {
		bandwidth:	bw,
		Id:			id,
		qnum:		num,
		pri:		priority,
		exref:		ref_data,
	}

	return
}

/*
	Clones the queue into a new object.
*/
func (q *Queue) Clone( ) ( cq *Queue ) {
	cid :=  *q.Id
	cexref := *q.exref

	cq = &Queue {
		bandwidth: q.bandwidth,
		Id:	&cid,
		qnum: q.qnum,
		pri:	q.pri,
		exref:	&cexref,
	}

	return
}

/*
	Increase the amount assigned to the queue by amt.
*/
func (q *Queue) Inc( amt int64 ) {
	if q != nil {
		q.bandwidth += amt
	}
}

/*
	Decrease the amount assigned to the queue by amt.
*/
func (q *Queue) Dec( amt int64 ) {
	if q != nil {
		q.bandwidth -= amt
	}
}

/*
	Destruction
*/
func (q *Queue) Nuke() {
	if q != nil {
		q.Id = nil
		q.exref = nil
	}
}

/*
	Sets the bandwidh for the queue to the value (bps) passed in. 
*/
func (q *Queue) Set_bandwidth(  b int64 ) {
	if q != nil {
		q.bandwidth = b;
	}
}

/*
	Adjust the priority of the queue to  the value passed in.  
	Priority values should be between 1 and 1024 with the larger
	values being lower in priority.
*/
func (q *Queue) Set_priority( p int ) {
	if q != nil {
		q.pri = p;
	}
}

/*
	Returns the queue number for this queue. The queue number is the 
	value that is placed on flow-mods which are sent to the switch
	as an enqueue action and that are associated with a min/max
	and/or QoS group on the switch.  A value of -1 is returned 
	on error. 
*/
func (q *Queue) Get_num( ) ( int ) {
	if q != nil {
		return q.qnum;
	}

	return -1
}

/*
	Returns a pointer to the external reference string associated with this queue.
*/
func (q *Queue) Get_eref( ) ( *string ) {
	if q != nil {
		return q.exref
	}

	return nil
}

/*
	Genrate a string that can be given on a queue setting command line.
	Format is:  <external-reference>,<id>,<queuenumber>,<bandwidth-min>,<bandwidth-max>,<priority>
	For the moment, both min/max bandwidth are the same, but we'll allow for them to be different
	in future.
*/
func ( q *Queue ) To_str( ) ( string ) {

	if q == nil {
		return ""
	}

	st := fmt.Sprintf( "%s,%s,%d,%d,%d,%d", *q.exref, *q.Id, q.qnum, q.bandwidth, q.bandwidth, q.pri );
	return st
}

/*
	Return a string only if bandwidth value is positive. 
*/
func ( q *Queue ) To_str_pos( ) ( string ) {

	if q == nil || q.bandwidth <= 0 {
		return ""
	}

	st := fmt.Sprintf( "%s,%s,%d,%d,%d,%d", *q.exref, *q.Id, q.qnum, q.bandwidth, q.bandwidth, q.pri );
	return st
}

/*
	Returns a json string that represents this queue. The information includes num, priority, 
	bandwidh, id and external reference string. 
*/
func (q *Queue) To_json( ) ( string ) {
	if q == nil {
		return ""
	}

	st := fmt.Sprintf( `{ "num": %d, "pri": %d, "bandw": %d, "id": %q, "eref": %q }`, q.qnum, q.pri, q.bandwidth, *q.Id, *q.exref )

	return st
}
