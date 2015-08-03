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
				29 Jun 2014 - Changes to support user link limits.
				29 Jul 2014 : Mlag support
				23 Oct 2014 - Find path functions return an indication that no path might have been
					caused by a capacity issue rather than no path.
				17 Jun 2015 - Added checking for nil pointer on some functions. General cleanup of
					comments and switch to stringer interface instead of To_str(). Added support for
					oneway bandwidth reserations with a function that checks outbound capacity
					on all switch links.
*/

package gizmos

import (
	"fmt"
	"strings"

	"github.com/att/att/tegu"
)


/*
	Defines a switch.
*/
type Switch struct {
	id			*string				// reference id for the switch
	links		[]*Link				// links to other switches
	lidx		int					// next open index in links
	hosts		map[string] bool	// hosts that are attched to this switch
	hvmid		map[string]*string	// vmids of attached hosts
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
	tokens := strings.SplitN( *id, "@", 2 )			// in q-lite world we get host@interface and we need only host portion
	id = &tokens[0]

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
	s.hvmid = make( map[string]*string, 64 )
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
	Add a link to the switch.
*/
func (s *Switch) Add_link( link *Link ) {
	var (
		new_links	[]*Link
		i			int
	)

	if s == nil {
		return
	}

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
	Track an attached host (by name only)
*/
func (s *Switch) Add_host( host *string, vmid *string, port int ) {
	if s == nil {
		return
	}

	s.hosts[*host] = true
	s.hport[*host] = port
	s.hvmid[*host] = vmid
}

/*
	Returns true if the named host is attached to the switch.
*/
func (s *Switch) Has_host( host *string ) (bool) {
	if s == nil {
		return false
	}

	r := s.hosts[*host]
	return r
}

/*
	Return the ID that has been associated with this switch. Likely this is the DPID.
*/
func (s *Switch) Get_id( ) ( *string ) {
	if s == nil {
		return nil
	}

	return s.id
}

/*
	Return the ith link in our index or nil if i is out of range.
	Allows the user programme to loop through the list if needed. Yes,
	this _could_ have been implemented to drive a callback for each
	list element, but that just makes the user code more complicated
	requiring an extra function or closure and IMHO adds uneeded
	maintence and/or learning curve issues.
*/
func (s *Switch) Get_link( i int ) ( l *Link ) {
	if s == nil {
		return nil
	}

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

	The usr max value is a percentage which defines the max percentage of
	a link that the user (tenant in openstack terms) is allowed to reserve
	on any given link.

	We will not probe a neighbour if the link to it cannot accept the additional
	capacity.

	The target may be the name of the host we're looking for, or the ID of the
	endpoint switch to support finding a path to a "gateway".
*/
func (s *Switch) probe_neighbours( target *string, commence, conclude, inc_cap int64, usr *string, usr_max int64 ) ( found *Switch, cap_trip bool ) {
	var (
		fsw	*Switch			// next neighbour switch (through link)
	)

	found = nil
	cap_trip = false

	//fmt.Printf( "\n\nsearching neighbours of (%s) for %s\n", s.To_str(), *target )
	for i := 0; i < s.lidx; i++ {
		if s != fsw  {
  			has_room, err := s.links[i].Has_capacity( commence, conclude, inc_cap, usr, usr_max )
			if has_room {
				fsw = s.links[i].forward				// at the switch on the other side of the link
				if (fsw.Flags & tegu.SWFL_VISITED) == 0 {
					obj_sheep.Baa( 3, "switch:probe_neigbour: following link %d -- has capacity to (%s) and NOT visited", i, fsw.To_str() )
					if s.Cost + s.links[i].Cost < fsw.Cost {
						//fmt.Printf( "\tsetting cost: %d\n", s.Cost + s.links[i].Cost )
						fsw.Cost = s.Cost + s.links[i].Cost
						fsw.Prev = s								// shortest path to this node is through s
						fsw.Plink = i								// using its ith link
					}
	
					obj_sheep.Baa( 3, "compare: (%s) (%s)", *target, *(fsw.Get_id()) )
					if fsw.Has_host( target ) || *(fsw.Get_id()) == *target {			// target is attahced to this switch, or the target is a swtich that is the forward switch
						fsw.Prev = s
						fsw.Plink = i
						found = fsw
						return
					}
	
				}
			}  else {
				obj_sheep.Baa( 2, "no capacity on link: %s", err )
				cap_trip = true
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

	The usr_max vlaue is a percentage (1-100) which indicaes the max percentage
	of a link that the user may reserve.

	The cap_trip return value is set to true if one or more links could not be
	followed because of capacity. If return switch is nil, and cap-trip is true
	then the most likely cause of failure is capacity, though it _is_ possible that
	there really is no path between the swtich and the target, but we stunbled onto
	a link at capacity before discovering that there is no real path.  The only way
	to know for sure is to run two searches, first with inc_cap of 0, but that seems
	silly.
		
*/
func (s *Switch) Path_to( target *string, commence, conclude, inc_cap int64, usr *string, usr_max int64 ) ( found *Switch, cap_trip bool ) {
	var (
		sw		*Switch
		fifo 	[]*Switch
		push 	int = 0
		pop 	int = 0
		pidx 	int = 0
		lcap_trip	bool = false		// local detection of capacity exceeded on one or more links
	)

	if s == nil {
		return
	}

	cap_trip = false
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

		found, cap_trip = sw.probe_neighbours( target, commence, conclude, inc_cap, usr, usr_max )
		if found != nil {
			return
		}
		if cap_trip {
			lcap_trip = true			// must preserve this
		}
		
		if sw.Flags & tegu.SWFL_VISITED == 0 {				// possible that it was pushed multiple times and already had it's neighbours queued
			for i := 0; i < sw.lidx; i++ {
				has_room, err := sw.links[i].Has_capacity( commence, conclude, inc_cap, usr, usr_max )
				if has_room {
					if sw.links[i].forward.Flags & tegu.SWFL_VISITED == 0 {
						fifo[push] = sw.links[i].forward
						push++
						if push > len( fifo ) {
							push = 0;
						}
					}
				} else {
					obj_sheep.Baa( 2, "no capacity on link: %s", err )
					lcap_trip = true
				}
			}
		}

		sw.Flags |= tegu.SWFL_VISITED
		if pidx > 1 {
			pidx--
		}
	}

	cap_trip = lcap_trip		// indication that we tripped on capacity at least once if lcap was set
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
		obj_sheep.Baa( 3, "search_neighbours: target found on switch: %s\n", *s.id )
		c := make( []*Link, clidx )
		copy( c, clinks[0:clidx+1]	)	// copy and push into the trail list
		tl.links[tl.lidx] = c
		tl.lidx++
	} else {							// not the end, keep searching forward
		// TODO: check to see that we aren't beyond limit
		s.Flags |= tegu.SWFL_VISITED
		obj_sheep.Baa( 3, "search_neighbours: testing switch: %s  has %d links", *s.id, s.lidx )

		for i := 0; i < s.lidx; i++ {				// for each link to a neighbour
			sn := s.links[i].Get_forward_sw()
			if (sn.Flags & tegu.SWFL_VISITED) == 0  {
				obj_sheep.Baa( 3, "search_neighbours: advancing over link %d switch: %s", i, *sn.id )
				clinks[clidx] = s.links[i]			// push the link onto the trail and check out the switch at the other end
				sn.ap_search_neighbours( target, clinks, clidx+1,  tl )
				obj_sheep.Baa( 3, "search_neighbours: back to  switch: %s",  *s.id )
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

	Usr_max is a perctage value (1-100) that defines the maximum percentage of any link that the user
	may reserve.
*/
func (s *Switch) All_paths_to( target *string, commence int64, conclude int64, inc_amt int64, usr *string, usr_max int64 ) ( links []*Link, ep *Switch, err error ) {
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
			_, err := v.Has_capacity( commence, conclude, inc_amt, usr, usr_max )
			if err != nil {
				err = fmt.Errorf( "no capacity found between switch (%s) and target (%s)", *s.id, *target )
				obj_sheep.Baa( 2, "all_paths: no capacity on link: %s", err )
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


/*
	Checks all links to determine if they _all_ have the capacity to support
	additional outbound traffic (inc_cap).  Used to check for gating when a
	path isn't built, but rate limiting at ingress is needed.
*/
func (s *Switch) Has_capacity_out( commence, conclude, inc_cap int64, usr *string, usr_max int64 ) ( bool ) {
	if s == nil {
		return	false
	}

	for i := 0; i < s.lidx; i++ {
		has_room, err := s.links[i].Has_capacity( commence, conclude, inc_cap, usr, usr_max )
		if ! has_room {
			obj_sheep.Baa( 2, "switch/cap_out: no capacity on link from %s: %s", s.id, err )
			return false
		}
	}

	obj_sheep.Baa( 2, "switch/cap_out: %s has capacity", s.id )
	return true
}

// -------------------- formatting ----------------------------------------------------

/*
	Generate some useable representation for debugging
	Deprectated -- use Stringer interface (String())
*/
func (s *Switch) To_str( ) ( string ) {
	return s.String()
}

/*
	Generate some useable representation for debugging
*/
func (s *Switch) String( ) ( string ) {
	if s != nil {
		return fmt.Sprintf( "%s %d links cost=%d fl=0x%02x", *s.id, s.lidx, s.Cost, s.Flags )
	}

	return "null-switch"
}

/*
	Generate a string containing json represntation of the switch.
*/
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
				vmid := "unknown"
				if s.hvmid[k] != nil {
					vmid = *s.hvmid[k]
				}
				jstr += fmt.Sprintf( `%s { "host": %q, "port": %d, "vmid": %q }`, sep, k, s.hport[k], vmid  )
				sep = ","
			}
		}
		jstr += " ]"
	}

	jstr += " }"
	return
}
