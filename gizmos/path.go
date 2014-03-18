// vi: sw=4 ts=4:

/*

	Mnemonic:	path
	Abstract:	Manages a path as a list of switches (needed to set a flow mod)
				and a list of links (needed to set obligations). There is no 
				attempt to maintain the path as a graph, though we'll save the 
				data in the order added, so if the caller adds things in path 
				order it represents the direction of flow.

				Some ascii art that might help
			
				path===>[sw1] ------------LINK1-------- [sw2]---------LINK2--------[sw3]
			    			^                          ^
			    			|   link1's                |   link1's
							--- forward queue           ---backward queue
			

				*sw1 is where h1 connects, sw3 is where h2 connects. 
				*the forward queue location is on the switch/port that sends data on the link in 
					the "path forward" direction (towards h2)
				*The backward queue location is the opposite; swtich/port sending data on the 
					link in the backwards direction (towards h1)
			
				The forward/backwards naming convention make sense, but are not obvious

	Date:		26 November 2013
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
	"os"
	//"strings"
	//"time"

	//"forge.research.att.com/gopkgs/clike"
)

type Path struct {
	links	[]*Link
	lidx	int
	switches []*Switch
	sidx	int
	h1		*Host
	h2		*Host
	is_reverse	bool		// set to indicate that the path was saved in reverse order
}

// ---------------------------------------------------------------------------------------

/*
	Creates an empty path representation between two hosts.
*/
func Mk_path( h1 *Host, h2 *Host ) ( p *Path ) {
	p = &Path {
		h1:		h1,
		h2:		h2,
		lidx:	0,
		sidx:	0,
		is_reverse: false,
	}
	
	p.links = make( []*Link, 32 )
	p.switches = make( []*Switch, 64 )

	return
}

/*
	Destruction.
*/
func (p *Path) Nuke() {
	for i := 0; i < p.lidx; i++ {
		p.links[i] = nil
	}
	p.links = nil
	for i := 0; i < p.sidx; i++ {
		p.switches[i] = nil
	}
	p.switches = nil
	p.h1 = nil
	p.h2 = nil
}

/*
	Causes the reverse indicator to be set.  This is necessary if
	the path has been constructed in reverse and affects the way
	queue information along the path is generated. 
*/
func (p *Path) Set_reverse( state bool ) {
	p.is_reverse = state
}

/*
	Adds the link passed in to the path. Links should be added in 
	order from the origin switch to the termination switch.  If
	the links are added in reverse, the reverse indicator should
	be set for the path (see Set_reverse() method).  Adding links
	out of order will cause interesting and likely undesired, results. 
*/
func (p *Path) Add_link( l *Link ) {
	var (
		new_links	[]*Link
	)

	if l == nil {
		return
	}

	if p.lidx >= len( p.links ) {
		new_links = make( []*Link, p.lidx + 32 )
		copy( new_links, p.links )
		p.links = new_links;	
	}

	p.links[p.lidx] = l
	p.lidx++
	
	return
}

/*
	Adds the switch passed in to the path.
	Switches should be added in order from the source to termination
	switch. If the order is from termination to source, then the 
	reverse indicator must be set.   Adding switches out of order
	will cause for interesting, and likely undesired, results. 
*/
func (p *Path) Add_switch( s *Switch ) {
	var (
		new_switches	[]*Switch
	)

	if p == nil {
		return
	}

	if p.sidx >= len( p.switches ) {
		new_switches = make( []*Switch, p.sidx + 64 )
		copy( new_switches, p.switches )
		p.switches = new_switches;	
	}

	p.switches[p.sidx] = s
	p.sidx++
}

/*
	Increases the utilisation of the path by adding delta to all links. This assumes that the
	link has already been tested and indicated it could accept the change. 
*/
func (p *Path) Inc_utilisation( commence, conclude, delta int64 ) ( r bool ){
	r = true

	for i := 0; i < p.lidx; i++ {
		if ! p.links[i].Inc_utilisation( commence, conclude, delta ) {
			r = false
		}
	}

	return
}


/*
	Add the necessary queues to the path that increase the utilisation of the links in the path.
	If is_reverse is set to true, the queue is added from last to first in the list. 

	The amt_in and amt_out values are the bandwidth outbound from the host1 and inbound to the host1 relative 
	to the direction of the path.  These values are used to properly set the queues for data traveling
	from host1 to host2 (out) and in the reverse direction (in).  To that end four queue types are 
	created on the links:
		1) priority-in the priority queue (1) for data returning to host1
		2) priority-out the priority queue (1) for date outbound toward host 2
		3) qid - the queue (n) set on the first link in the path for data flowing outbound
		4) Rqid - the queue (n) set on the last link in the path for the data flowing from host2 toward host1
*/
func (p *Path) Set_queue( qid *string, commence int64, conclude int64, amt_in int64, amt_out int64 ) (err error) {
	err = nil
	poutstr := "priority-out"		// names for priority queue in the proper direction
	pinstr := "priority-in"

	if p == nil {
		obj_sheep.Baa( 0, "set_queue: p is nil!" )
		err = fmt.Errorf( "p is nil" )
		return
	}

	if p.lidx == 0 {			// TODO: this is a special case which indicates h1-h2 is on the same switch and needs to be handeled differently
		obj_sheep.Baa( 0, "set_queue: no links in the path!" )
		err = fmt.Errorf( "path has no links" )
		return
	}

	if p.is_reverse {				// path was saved backwards, so we run it from last to first
		err = p.links[p.lidx-1].Set_forward_queue( qid, commence, conclude, amt_out )		// set first outbound queue from h1 on the ingress to a specific queue
		if err != nil { return }

		if p.lidx > 1 {																			// if this is only link, there'll not be a priority queue set toward h1
			err = p.links[p.lidx-1].Set_backward_queue( &pinstr, commence, conclude, amt_in )	// add inbound amount to the priority queue for this link in direction of h1
			if err != nil { return }
		}

		for i := p.lidx-2; i > 0; i-- {						// set priority queues for all interediate links; set in both directions
			err = p.links[i].Set_forward_queue( &poutstr, commence, conclude, amt_out )
			if err != nil { return }

			err = p.links[i].Set_backward_queue( &pinstr, commence, conclude, amt_in  )
			if err != nil { return }
		}

		rqid := "R" + *qid
		err = p.links[0].Set_backward_queue( &rqid, commence, conclude, amt_in )			// and the 'reverse' (outbound from h2) gets a specific queue num set to inbound h1 amt
		if err != nil { return }
		if p.lidx > 1 {																		// when only one link, there is no priority queue inbound to h2
			err = p.links[0].Set_forward_queue( &poutstr, commence, conclude, amt_out )		// for the last link set the last priority in direction of h2 to amt-out
		}

	} else {
		err = p.links[0].Set_forward_queue( qid, commence, conclude, amt_out )			// set the specific queue on the ingress switch side of the link
		if err != nil { return }

		if p.lidx > 1 {																	// when more than one link we need a priority queue on the far end of the link
			p.links[0].Set_backward_queue( &pinstr, commence, conclude, amt_in )		// set the inbound amount on the priority queue of the first link
		}

		for i := 1; i < p.lidx-1; i++ {
			err = p.links[i].Set_forward_queue( &poutstr, commence, conclude, amt_out )
			if err != nil { return }

			err = p.links[i].Set_backward_queue( &pinstr, commence, conclude, amt_in )
			if err != nil { return }
		}

		rqid := "R" + *qid
		err = p.links[p.lidx-1].Set_backward_queue( &rqid, commence, conclude, amt_in )			// for last link, inbound limit for h1 gates the outbound queue on last switch
		if err != nil { return }
		if p.lidx > 1 {																				// when just one link there is no priority queue into last switch
			err = p.links[p.lidx-1].Set_forward_queue( &poutstr, commence, conclude, amt_out )		// and priority for this is the limit out from h1
		}
	}

	return
}

/*
	Return the forward link information (switch/port/queue-num) associated with the first link of path.
	This is the port and queue number used on the first switch in the path to send data _out_ from h1.
	The data is based on queue ID and the timestamp given (queue numbers can vary over time).
*/
func (p *Path) Get_forward_ep_spq( qid *string, tstamp int64 ) ( spq *Spq ) {
	var (
		idx int = 0
	)
	
	spq = nil

	if p.is_reverse {			// if reverse we need to look at the last rather than the first
		idx = p.lidx-1		
	}
	
	if idx >= 0 {
		spq = Mk_spq( p.links[idx].Get_forward_info( qid, tstamp ) )
	}
		
	return
}

/*
	Return the backward link information (switch/port/queue-num) associated with the last link of path.
	This is the port and queue number on the last switch in the path that is used to send data _back_
	to h1 (inbound) from h2.
	The data is based on queue ID and the timestamp given (queue numbers can vary over time).
*/
func (p *Path) Get_backward_ep_spq( qid *string, tstamp int64 ) ( spq *Spq ) {
	var (
		idx int
	)
	
	spq = nil

	idx = p.lidx-1		
	if p.is_reverse {			// if reverse we need to look at the first link
		idx = 0
	}
	
	if idx >= 0 {
		spq = Mk_spq( p.links[idx].Get_backward_info( qid, tstamp ) )
	}
		
	return
}

/*
	Return a list of intermediate switch/port/queue-num tuples in a forward (h1->h2) direction.
	(The data is based on priority-out queues.) 
*/
func (p *Path) Get_forward_im_spq( tstamp int64 )  ( []*Spq ){
	var (
		pout string = "priority-out"
		ret_list []*Spq
		ridx	int = 0
	)

	ret_list = make( []*Spq, 128 )

	// TODO:  check bounds on ret_list
	if p.is_reverse {
		for i := p.lidx-2; i >= 0; i-- {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_forward_info( &pout, tstamp ) )
			ridx++
		}
	} else {
		for i := 1; i < p.lidx; i++ {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_forward_info( &pout, tstamp ) )
			ridx++
		}
	}

	return ret_list[:ridx]
}

/*
	Returns a list of intermediate switch/port/qnum tuples in a backwards (h2->h1) direction.
	(The queues are based on a priority-in queue name)
*/
func (p *Path) Get_backward_im_spq( tstamp int64 )  ( []*Spq ){
	var (
		pin string = "priority-in"
		ret_list []*Spq
		ridx	int = 0
	)

	ret_list = make( []*Spq, 128 )

	// TODO:  check bounds on ret_list
	if p.is_reverse {
		for i := p.lidx-1; i > 0; i-- {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_backward_info( &pin, tstamp ) )
			ridx++
		}
	} else {
		for i := 0; i < p.lidx - 1; i++ {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_backward_info( &pin, tstamp ) )
			ridx++
		}
	}

	return ret_list[:ridx]
}

/*
	Return a list of switch/port/queue-num tuples for all of the intermediate links in a path. Both
	the forward and backward tuples are returned in the list making the list a complete set of 
	switch/port/queue-nums that must be translated into flowmods along the path in order to 
	properly queue traffic for a reservation.
*/
func (p *Path) Get_intermed_spq( tstamp int64 )  ( []*Spq ){
	var (
		pin string = "priority-in"
		pout string = "priority-out"
		ret_list []*Spq
		ridx	int = 0
	)

	ret_list = make( []*Spq, 128 )

	// TODO:  check bounds on ret_list
	if p.is_reverse {
		for i := p.lidx-1; i > 0; i-- {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_backward_info( &pin, tstamp ) )
			ridx++
		}
		for i := p.lidx-2; i >= 0; i-- {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_forward_info( &pout, tstamp ) )
			ridx++
		}
	} else {
		for i := 0; i < p.lidx - 1; i++ {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_backward_info( &pin, tstamp ) )
			ridx++
		}
		for i := 1; i < p.lidx; i++ {
			ret_list[ridx] = Mk_spq(  p.links[i].Get_forward_info( &pout, tstamp ) )
			ridx++
		}

	}

	return ret_list[:ridx]
}

/*
	Creates a new path that is the inverse (reverse) of the path. The original 
	path is not damaged.
*/
func (p *Path) Invert( ) ( ip *Path ) {
	ip = Mk_path( p.h1, p.h2 )

	for i := p.lidx - 1; i >= 0; i-- {
		ip.Add_link( p.links[i] )
	}

	for i := p.sidx - 1; i >= 0; i-- {
		ip.Add_switch( p.switches[i] )
	}

	ip.is_reverse = !p.is_reverse
	return 
}

// ------------------------ string/json/human output functions ------------------------------------

/*
	Debugging and/or testing dump of the path. If reverse is true, then we assue that path
	is in reverse order.
*/
func (p *Path) Dump( reverse bool ) {
	var (
		sep string = ""
		sw1 *string
		sw2 *string
		swp1 int
		swp2 int
	)

	if reverse {
		for i := p.lidx-1; i >= 0; i-- {
			sw1, sw2 = p.links[i].Get_sw_names()
			swp1, swp2 = p.links[i].Get_sw_ports()
			ob := p.links[i].Get_allotment()			// get the obligation
			fmt.Fprintf( os.Stderr, "%ss(%s/%d) <== %.2fM", sep, *sw1, swp1, float64(ob.Get_max_allocation( ))/1000000.0 )
			sep = " ==> "
		}
	} else {
		for i := 0; i < p.lidx; i++ {
			sw1, sw2 = p.links[i].Get_sw_names()
			swp1, swp2 = p.links[i].Get_sw_ports()
			ob := p.links[i].Get_allotment()			// get the obligation
			fmt.Fprintf( os.Stderr, "%ss(%s/%d) <== %.2f", sep, *sw1, swp1, float64(ob.Get_max_allocation( ))/1000000.0 )
			sep = " ==> "
		}
	}

	fmt.Fprintf( os.Stderr, "%ss(%s/%d)\n", sep, *sw2, swp2 )
}


/*
	Generates a string representing the path.
*/
func (p *Path) To_str( ) ( s string ) {
	var (
		sep string = ""
	)

	s = ""

	for i := 0; i < p.sidx; i++ {
		s += fmt.Sprintf( "%s %s ", sep, *(p.switches[i].Get_id()) )
		sep = "->"
	}

	return
}

/*
	Generates a string of json which represents the path.
*/
func (p *Path) To_json( ) (json string) {
	var (
		sep string = ""
	)

	json = fmt.Sprintf( "{ %q: %q, %q: %q, %q: [ ", "h1", *p.h1, "h2", *p.h2, "links" )
	for i := 0; i < p.lidx; i++ {
		json += fmt.Sprintf( "%s%s ", sep, p.links[i].To_json() )
		sep = ","
	}

	sep = ""
	json += fmt.Sprintf( "], %q: [ ", "switches" )
	for i := 0; i < p.sidx; i++ {
		json += fmt.Sprintf( "%s%q ", sep, *(p.switches[i].Get_id()) )
		sep = ","
	}
	json += fmt.Sprintf( "] }" )
	return
}
