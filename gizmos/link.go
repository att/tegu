// vi: sw=4 ts=4:

/*

	Mnemonic:	link
	Abstract:	Manages a link in the network. 
				A link is unidirectional representing data flow between two switches from the 
				backward switch to the forward switch.  For each link an obligation is used
				to manage the usage that has been pledged for the link at various points in 
				times.  For a path between two switches that is bi-directional, there will be 
				two links which likley share a common obligation. Having a link manage only 
				unidirectional traffic makes queue setting for priorities on the last link
				in a path easier, and also makes path finding simpler.
			
				sw1 ------link------ sw2
	   			backward             forward
			
				Potential comfusing naming, but it is logical....
				The set_forward_queue() function will set the queue on _sw1_ NOT on the forward
				swtich!!  The logic is that the port/queue on sw1 is the port/queue where 
				data is pushed to go to the forward switch.  Similarly, set_backward_queue()
				will set the proper port/queue on sw2 -- where to put data in the backward
				direction. 
			
				Initially the link maintained only 'forward' information, there was no 
				concept of the backward switch, but the need to manage queues for individual
				reservations introduced the need, and while confusing the struct was extended
				using the related term. 

	Date:		22 November 2013
	Author:		E. Scott Daniels

*/

package gizmos

import (
	//"bufio"
	"fmt"
	//"os"
	//"strings"
	//"time"
)

// --------------------------------------------------------------------------------------

/*
	Defines a link between two network eleents (we assume switches). The concept 
	of forward is data traveling from switch1 toward switch2 over the link and 
	backwards (data flowing from switch2 to switch1) is established.
*/
type Link struct {
	forward		*Switch				// switch in the forward direction
	backward	*Switch				// switch in the reverse direction
	port1		int					// the port on sw1 in the direction of sw2
	port2		int					// the port on sw2 in the direction of sw1
	id			*string				// reference id for the link	
	sw1			*string				// human name for forward switch
	sw2			*string				// human name for backward switch
	allotment	*Obligation			// the obligation that exsists for the link (obligations are timesliced)

	Cost		int					// the cost of traversing the link for shortest path computation
}

/*
	Constructor.
	If bond is supplied, it is assumed to be a one element slice containing another
	link from which the allotment obligation is fetched and will be referenced by the 
	link rather than creating a new obligation. Binding two links to an obligation
	allows for easy accounting of total usage allocated (both directions) for the
	bidirectional path that the two links represent. 
*/
func Mk_link( sw1 *string, sw2 *string, capacity int64, bond ...*Link ) ( l *Link ) {
	var id string

	id = fmt.Sprintf( "%s-%s", *sw1, *sw2 )

	l = &Link { 
		id: &id,
		sw1: sw1,
		sw2: sw2,
		Cost:	1,				// for now all links are equal
	}

	if bond == nil || bond[0] == nil {
		l.allotment = Mk_obligation( capacity )
	} else {
		l.allotment = bond[0].Get_allotment( )
	}

	return
}

/*
	Constructor.
	This will build a 'virtual' link which differs from a real link on a few points:
		* the link is between ports on the same switch, so both switch references
			are to the same place
		* the ID is constructed as switch-name.port1.port2
		* there probably is no 'reverse' link created so bond is likely not applied

	Other than a different set of parameters on construction, a virtual link and
	link representing a real network link, should not be any different from the 
	application's perspective. 
*/
func Mk_vlink( sw *string, p1 int, p2 int, capacity int64, bond ...*Link ) ( l *Link ) {
	var id string

	id = fmt.Sprintf( "%s.%d.%d", *sw, p1, p2 )

	l = &Link { 
		id: &id,
		sw1: sw,
		sw2: sw,
		Cost:	1,				// for now all links are equal
	}

	if bond == nil || bond[0] == nil {
		l.allotment = Mk_obligation( capacity )
	} else {
		l.allotment = bond[0].Get_allotment( )
	}

	return
}

/*
	Destroys a link. 
*/
func (l *Link) Nuke() {
	l.forward = nil;
	l.backward = nil;
	l.id = nil
	l.sw1 = nil
	l.sw2 = nil
	l.allotment = nil
}

/*
	Returns the allotment that is assigned to the link. 
*/
func (l *Link) Get_allotment( ) ( *Obligation ) {
	return l.allotment
}

/*
	Associates the allotment passed in with the link (used to share an allotment
	between multiple links. 
*/
func (l *Link) Set_allotment( ob *Obligation ) {
	l.allotment = ob
}

/*
	Allows the forward switch to be set.
*/
func (l *Link) Set_forward( sw *Switch ) {
	l.forward = sw; 
}

/*
	Allows the backward switch to be set
*/
func (l *Link) Set_backward( sw *Switch ) {
	l.backward = sw; 
}

/*
	Sets a port for either sw1 or sw2 to port.  Which indicates the switch to 
	set 1 or 2.
*/
func ( l *Link ) Set_port( which int, p int ) {
	if which == 1 {
		l.port1 = p
	} else {
		l.port2 = p
	}
}

/*
	Sets both port1 and port2 provided they are > 0. 
*/
func ( l *Link ) Set_ports( p1 int, p2 int ) {
	if p1 > 0 {
		l.port1 = p1
	}
	if p2 > 0  {
		l.port2 = p2
	}
}

/*
	Returns true if the forward path on the link ends at the switch passed in. 
*/
func (l *Link) Forwards_to( sw *Switch ) ( bool ) {
	return l.forward == sw
}

/*
	Returns true if the backward path on the link ends at the switch passed in. 
*/
func (l *Link) Comes_from( sw *Switch ) ( bool ) {
	return l.backward == sw
}

/*
	Returns the pointer to the switch in the forward direction.
*/
func (l *Link) Get_foward_sw( ) ( *Switch ) {
	return l.forward
}

/*
	Returns the pointer to the switch in the backward direction.
*/
func (l *Link) Get_backward_sw( ) ( *Switch ) {
	return l.forward
}

/*
	Returns the switch names (forward, backward).
*/
func (l *Link) Get_sw_names( ) ( *string, *string ) {
	return l.sw1, l.sw2
}

/*
	Returns the switch ports (forward, backward).
*/
func (l *Link) Get_sw_ports( ) ( int, int ) {
	return l.port1, l.port2
}

/*
	Returns true if the link connects to the swtich (in either direction).
*/
func (l *Link) Connects( sw *Switch ) ( bool ) {
	return l.backward == sw || l.forward == sw
}

/*
	Return true if this link can accept the indicated amount in addition to the current
	obligation for the time window indicated by commence and conclude.
*/
func (l *Link) Has_capacity( commence int64, conclude int64, amt int64 ) ( bool ) {
	return l.allotment.Has_capacity( commence, conclude, amt )
}

/*
	The new link capacity is set to the value passed in. 
	The capacity is the maximum bandwidth that the link can support. If the link's allotment is 
	shared (bound) with another link, then the new value imples the maximum
	bandwidth in either direction. 
*/
func (l *Link) Mod_capacity( new_cap int64 ) {
	l.allotment.Max_capacity = new_cap
}

/*
	Increases the current max capacity for the link by delta (+/-).
	The capacity is the maximum bandwidth that the link can support. If the link's allotment is 
	shared (bound) with another link, then the new value imples the maximum
	bandwidth in either direction. 
*/
func (l *Link) Inc_capacity( delta int64 ) {
	l.allotment.Max_capacity += delta
	if l.allotment.Max_capacity < 0 {				// must check as caller could have sent neg delta
		l.allotment.Max_capacity = 0
	}
}

/*
	Decreases the current max capacity by delta (not really needed, but it
	isn't difficult to supply and might make user's code easier). 
	The capacity is the maximum bandwidth that the link can support. If the link's allotment is 
	shared (bound) with another link, then the new value imples the maximum
	bandwidth in either direction. 
*/
func (l *Link) Dec_capacity( delta int64 ) {
	l.allotment.Max_capacity -= delta
	if l.allotment.Max_capacity < 0 {
		l.allotment.Max_capacity = 0
	}
}

/*
	Return the link's allotment for the given time.
*/
func (l *Link) Get_allocation( utime int64 ) ( int64 ) {
	return l.allotment.Get_allocation( utime )
}

/*
	Checks the current utilisation for the link to see if adding the amount to the 
	utilisation, for the time period indicated, will cause the utilisation to excede the 
	maximum capacity for the link. If adding amount does not exceed the maximum
	capacity, the amount is added to the link's utilisation and true is returned. 
	Otherwise, no change in the utilisation is made and false is returned. 
*/
func (l *Link) Inc_utilisation( commence int64, conclude int64, amt int64 ) ( r bool ) {
	r = l.allotment.Has_capacity( commence, conclude, amt )
	if r {
		l.allotment.Inc_utilisation( commence, conclude, amt )
	}

	return
}

/*
	Create a new queue in our obilgation that sets the queue/port in the queue based on 
	sw1 sending data in the forward direction (toward sw2 which is forward in the struct). 	 
	Qid is a string that is used to identify the queue -- useful for digging out queue/port
	information that are needed for reservations and thus is probably the reservation ID or 
	some derivation, but is up to the user of the link.
*/
func (l *Link) Set_forward_queue( qid *string, commence int64, conclude int64, amt int64 ) ( error ) {

	swdata := fmt.Sprintf( "%s/%d", *l.sw1, l.port1 )			// switch and port data that will be necessary to physically set the queue
	return l.allotment.Add_queue( qid, &swdata, amt, commence, conclude )
}

/*
	Create a new queue in our obilgation that sets the queue/port in the queue based on 
	sw2 sending data in a backwards direction (toward sw1 which is the backward switch).
	Qid is a string that is used to identify the queue -- useful for digging out queue/port
	information that are needed for reservations and thus is probably the reservation ID or 
	some derivation, but is up to the user of the link.
*/
func (l *Link) Set_backward_queue( qid *string, commence int64, conclude int64, amt int64 ) ( error ) {

	swdata := fmt.Sprintf( "%s/%d", *l.sw2, l.port2 )			// switch and port data that will be necessary to physically set the queue
	return l.allotment.Add_queue( qid, &swdata, amt, commence, conclude )
}

/*
	Add an amount to the indicated queue number. This is probably used just to increase the 
	generic priority queue for middle links, but who knows. 
*/
func (l *Link) Inc_queue( qid *string, commence int64, conclude int64, amt int64 ) {
	l.allotment.Inc_queue( qid, amt, commence, conclude )
}

/*
	Returns the information (switch/port/queue) that is needed for the switch (sw1) which sends
	data over the link in the forward direction at the time indicated by the timestamp and 
	for the queue with the queue-id passed in.
*/
func (l *Link) Get_forward_info( qid *string, tstamp int64 ) ( swid string, port int, queue int ) {
	
	if l == nil {
		return
	}

	swid = *l.sw1
	port = l.port1
	queue = l.allotment.Get_queue( qid, tstamp )

	return
}

/*
	Returns the information (switch/port/queue) that is needed for the switch (sw2) which sends
	data over the link in the backwards direction at the time indicated by the timestamp and 
	for the queue with the queue-id passed in.
*/
func (l *Link) Get_backward_info( qid *string, tstamp int64 ) ( swid string, port int, queue int ) {
	
	if l == nil {
		return
	}

	swid = *l.sw2
	port = l.port2
	queue = l.allotment.Get_queue( qid, tstamp )

	return
}

// -------- human and/or interface output generation -------------------------------------------------------------

/*
	Returns a list of queue information that an outside thing (human or programme) might need to actually 
	set the queues on a switch. The queue settings are for the point in time as indicated by the unix timestamp
	passed in. See the Queues2str() function in obligation.go for more information about the string that is 
	generated. 
*/
func (l *Link) Queues2str(  ts int64 ) ( s string ) {
	s = ""

	if l == nil || l.allotment == nil {
		return 
	}

	s = l.allotment.Queues2str( ts ) 
	return
}

/*
	Generate a string of the basic link infoormation.
	The output contains the following information in this order:
		eye-catcher (link:)
		switch 1
		switch 1's port 
		switch 2
		switch 2's port
		max capacity (bps)
*/
func (l *Link) To_str( ) ( s string ) {
	s = fmt.Sprintf( "link: %s %s/%d %s/%d %d", *l.id, *l.sw1, l.port1, *l.sw2, l.port2, l.allotment.Max_capacity )
	return
}

/*
	Generates a string of 'deep' json, including the allotment list.
*/
func (l *Link) To_json( ) ( s string ) {
	if l == nil {
		s = `{ "id": "null-link" } `
		return
	}

	s = fmt.Sprintf( `{ "id": %q, "sw1": %q, "sw1port": %d, "sw2": %q,  "sw2port": %d, "allotment": %s }`, *l.id, *l.sw1, l.port1, *l.sw2,  l.port2, l.allotment.To_json() )
	return
}
