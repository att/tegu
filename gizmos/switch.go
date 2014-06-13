// vi: sw=4 ts=4:

/*

	Mnemonic:	switch
	Abstract:	Functions associated with the switch datastructure. This module also contains
				the functions that implement path-finding. Dijkstra's algorithm is implemented
				(see Path_to) to determine a path between two hosts which we assume are connected
				to one or two switches.  The path finding algorithm allows for disjoint networks
				which occurs when one or more switches are not managed by the controller(s) used 
				to create the network graph.
	Date:		24 November 2013
	Author:		E. Scott Daniels

	Mods:		10 Mar 2014 - We allow a target to be either a switch or host when looking for a path. 
				13 May 2014 - Corrected bug in debug string. 
				11 Jun 2014 - Changes to support finding all paths between two VMs rather than just 
					the shortest one.
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
	"forge.research.att.com/tegu"
)

// --------------------------------------------------------------------------------------

/*
	defines a switch.
*/
type Switch struct {
	id			*string				// reference id for the link	
	links		[]*Link
	lidx		int					// next open index in links
	hosts		map[string] bool
	hport		map[string] int		// the port that the host (string) attaches to

									// these are for path finding and are needed externally
	Prev		*Switch				// previous low cost switch
	Plink		int					// index of link on Prev used to reach this node
	Cost		int					// cost to reach this node through Prev/Plink
	Flags		int					// visited and maybe others
}

/*
	Constructor.  Generates a switch object with the given id.
*/
func Mk_switch( id *string ) ( s *Switch ) {
	s = &Switch { 
		id: id,
		lidx: 0,
	}

	if id == nil {
		dup_str := "no_id_given"
		id = &dup_str
	} 

	s.links = make( []*Link, 32 )
	s.hosts = make( map[string]bool, 64 )
	s.hport = make( map[string]int, 64 )
	return
}


/*
	Destruction
*/
func (s *Switch) Nuke() {
	for i := 0; i < s.lidx; i++ {
		s.links[i] = nil
	}
	s.links = nil
	s.hosts = nil
	s.hport = nil
}

/* 
	add a link to the switch 
*/
func (s *Switch) Add_link( link *Link ) {
	var (
		new_links	[]*Link
		i			int
	)

	if s.lidx >= len( s.links ) {
		new_links = make( []*Link, s.lidx + 32 )
		
		for i = 0; i < len( s.links ); i++ {
			new_links[i]  = s.links[i]
		}
		
		s.links = new_links
	} 

	s.links[s.lidx] = link
	s.lidx++
}

/*
	track an attached host (by name only)
*/
func (s *Switch) Add_host( host *string, port int ) {
	s.hosts[*host] = true
	s.hport[*host] = port
}

/*
	Returns true if the named host is attached to the switch.
*/
func (s *Switch) Has_host( host *string ) (bool) {
	r := s.hosts[*host]
	return r
}

/*
	Return the ID that has been associated with this switch. Likely this is the DPID.
*/
func (s *Switch) Get_id( ) ( *string ) {
	return s.id
}

/*
	Return the ith link in our index or nil if i is out of range.
*/
func (s *Switch) Get_link( i int ) ( l *Link ) {
	l = nil
	if i >= 0  &&  i < s.lidx {
		l = s.links[i]
	}

	return
}

// -------------- shortest, single, path finding -------------------------------------------------------------

/*
	Probe all of the neighbours of the switch to see if they are attached to 
	the target host. If a neighbour has the target, we set the reverse path
	in the neighbour and return it indicating success.  If a neighbour does 
	not have the target, we update the neighbour's cost and reverse path _ONLY_
	if the cost through the current switch is lower than the cost recorded 
	at the neighbour. If no neighbour links to the target, we return null.

	We will not probe a neighbour if the link to it cannot accept the additional
	capacity. 

	The target may be the name of the host we're looking for, or the ID of the
	endpoint switch to support finding a path to a "gateway".
*/
func (s *Switch) probe_neighbours( target *string, commence, conclude, inc_cap int64 ) (found *Switch) {
	var (
		fsw	*Switch			// next neighbour switch (through link)
	)

	found = nil

	//fmt.Printf( "\n\nsearching neighbours of (%s) for %s\n", s.To_str(), *target )
	for i := 0; i < s.lidx; i++ {
		if s != fsw  &&  s.links[i].Has_capacity( commence, conclude, inc_cap ) {
			fsw = s.links[i].forward				// at the switch on the other side of the link
			if (fsw.Flags & tegu.SWFL_VISITED) == 0 {
				obj_sheep.Baa( 2, "switch:probe_neigbour: following link %d -- has capacity to (%s) and NOT visited", i, fsw.To_str() )
				if s.Cost + s.links[i].Cost < fsw.Cost {
					//fmt.Printf( "\tsetting cost: %d\n", s.Cost + s.links[i].Cost )
					fsw.Cost = s.Cost + s.links[i].Cost
					fsw.Prev = s								// shortest path to this node is through s
					fsw.Plink = i								// using its ith link
				}

				obj_sheep.Baa( 2, "compare: (%s) (%s)", *target, *(fsw.Get_id()) )
				if fsw.Has_host( target ) || *(fsw.Get_id()) == *target {			// target is attahced to this switch, or the target is a swtich that is the forward switch
					fsw.Prev = s
					fsw.Plink = i
					found = fsw
					return
				}

			}
		} 
	}

	return
}

/*
	Implements Dijkstra's algorithm for finding the shortest path in the network
	starting from the switch given and stoping when it finds a switch that has 
	the target host attached.  At the moment, link costs are all the same, so 
	there is no ordering of queued nodes such that the lowest cost is always
	searched next.  A path may exist, but not be available if the usage on a 
	link cannot support the additional capacity that is requested via inc_cap.
		
*/
func (s *Switch) Path_to( target *string, commence, conclude, inc_cap int64 ) (found *Switch) {
	var (
		sw		*Switch
		fifo 	[]*Switch
		push 	int = 0
		pop 	int = 0
		pidx 	int = 0
	)

	found = nil
	fifo = make( []*Switch, 4096 )

	obj_sheep.Baa( 2, "switch:Path_to: looking for path to %s", *target )
	s.Prev = nil
	fifo[push] = s
	push++

	for ; push != pop; {		// if we run out of things in the fifo we're done and found no path
		sw = fifo[pop]
		pop++
		if pop > len( fifo ) { 
			pop = 0; 
		}

		found = sw.probe_neighbours( target, commence, conclude, inc_cap )
		if found != nil {
			return
		}
		
		if sw.Flags & tegu.SWFL_VISITED == 0 {				// possible that it was pushed multiple times and already had it's neighbours queued
			for i := 0; i < sw.lidx; i++ {
				if sw.links[i].Has_capacity( commence, conclude, inc_cap ) {
					if sw.links[i].forward.Flags & tegu.SWFL_VISITED == 0 {
						fifo[push] = sw.links[i].forward
						push++
						if push > len( fifo ) { 
							push = 0; 
						}
					}
				}
			}
		}

		sw.Flags |= tegu.SWFL_VISITED
		if pidx > 1 {
			pidx--
		}
	}

	return
}
// -------------------- find all paths ------------------------------------------------

/*
	A list of links each of which represents a unique path between two switches.
*/
type trail_list struct {
	links [][]*Link
	lidx	int				// next entry to populate
	ep		*Switch			// far end switch
}


/*
	Examine all neighbours of the switch 's' for possible connectivity to target host. If s 
	houses the target host, then we push the current path to this host into the trail list
	and return. 
*/
func (s *Switch) ap_search_neighbours( target *string, clinks []*Link, clidx int, tl *trail_list ) {
	if s.Has_host( target ) {
		tl.ep = s							// mark the end switch
		obj_sheep.Baa( 2, "search_neighbours: target found on switch: %s\n", *s.id )
		c := make( []*Link, clidx )
		copy( c, clinks[0:clidx+1]	)	// copy and push into the trail list 
		tl.links[tl.lidx] = c
		tl.lidx++
	} else {							// not the end, keep searching forward
		// TODO: check to see that we aren't beyond limit
		s.Flags |= tegu.SWFL_VISITED 
		obj_sheep.Baa( 2, "search_neighbours: testing switch: %s  has %d links", *s.id, s.lidx )

		for i := 0; i < s.lidx; i++ {				// for each link to a neighbour
			sn := s.links[i].Get_forward_sw() 
			if (sn.Flags & tegu.SWFL_VISITED) == 0  {
				obj_sheep.Baa( 2, "search_neighbours: advancing over link %d switch: %s", i, *sn.id )
				clinks[clidx] = s.links[i]			// push the link onto the trail and check out the switch at the other end
				sn.ap_search_neighbours( target, clinks, clidx+1,  tl )
				obj_sheep.Baa( 2, "search_neighbours: back to  switch: %s",  *s.id )
			}
		}
	}

	s.Flags &= ^tegu.SWFL_VISITED				// as we back out we allow paths to come back through
}

/*
	Starting at switch s, this function finds all possible paths to the switch that houses the target
	host, and then returns the list of unique links that are traversed by one or more paths provided 
	that each link can support the increased amount of capacity (inc_amt). The endpoint switch is also 
	returned.  If any of the links cannot support the capacity, the list will be nil or empty; this is
	also the case if no paths are found.  The error message will indicate the exact reason if that is 
	important to the caller. 
*/
func (s *Switch) All_paths_to( target *string, commence int64, conclude int64, inc_amt int64 ) ( links []*Link, ep *Switch, err error ) {
	var (
		ulinks	map[string]*Link			// unique list of links involved in all trails
	)

	links = nil
	ep = nil
	err = nil

	tl := &trail_list{ lidx: 0 }
	tl.links = make( [][]*Link, 4096 )

	clinks := make( []*Link, 4096 )		// working set of links
	
	s.ap_search_neighbours(  target, clinks, 0, tl )

	if tl.lidx > 0 {								// found at least one trail
		ulinks = make( map[string]*Link )
		ep = tl.ep

		obj_sheep.Baa( 2, "switch/all-paths: %d trails found to target", tl.lidx )
		for i := 0; i < tl.lidx; i++ {				// for each trail between the two endpoints
			obj_sheep.Baa( 3, "Trail %d follows:", i )
			for j := range tl.links[i] {
				lid := tl.links[i][j].Get_id()				// add if not already found in another trail
				if ulinks[*lid] == nil  {
					ulinks[*lid] = tl.links[i][j]
				}
				obj_sheep.Baa( 3, "link %d: %s", j, tl.links[i][j].To_str( ) )
			}
		}

		obj_sheep.Baa( 2, "found %d unique links across %d trails", len( ulinks ), tl.lidx )
		links = make( []*Link, len( ulinks ) )
		i := 0
		for _, v := range ulinks {
			// TODO:  Add tenant based check
			if ! v.Has_capacity( commence, conclude, inc_amt ) {
				err = fmt.Errorf( "no capacity found bwtween switch (%s) and target (%s)", *s.id, *target )
				links = nil
				break
			}

			// TODO:  Add warning if the capacity for the link is above threshold (here, or when the usage is actually bumpped up?)
			links[i] = v
			i++
		}
	} else {
		err = fmt.Errorf( "no paths found bwtween switch (%s) and target (%s)", *s.id, *target )
	}

	return
}

// -------------------- formatting ----------------------------------------------------

/*
	generate some useable representation for debugging
*/
func (s *Switch) To_str( ) ( string ) {
	if s != nil {
		return fmt.Sprintf( "%s %d links cost=%d fl=0x%02x", *s.id, s.lidx, s.Cost, s.Flags )
	}

	return "null-switch"
}

func (s *Switch) To_json( ) ( jstr string ) {
	var sep = ""

	if s == nil {
		jstr = `{ id: "null_switch" }`
		return
	}

	if s.lidx > 0 {
		jstr = fmt.Sprintf( `{ "id": %q, "links": [ `, *s.id )

		for i := 0; i < s.lidx; i++ {
			jstr += fmt.Sprintf( "%s%s", sep, s.links[i].To_json() )
			sep = ","
		}
		jstr += " ]"
	} else {
		jstr = fmt.Sprintf( `{ "id": %q }`, *s.id )
	}


	if len( s.hosts ) > 0 {
		jstr += fmt.Sprintf( `, "conn_hosts": [ ` )
		sep = ""
		for k := range s.hosts {
			if s.hosts[k] == true {
				//jstr += fmt.Sprintf( `%s"%s"`, sep, k )
				jstr += fmt.Sprintf( `%s { "host": %q, "port": %d }`, sep, k, s.hport[k]  )
				sep = ","
			}
		}
		jstr += " ]"
	}

	jstr += " }"
	return
}
