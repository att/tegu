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

	Mnemonic:	network_path
	Abstract:	Functions that support the network manager with respect to path finding and
				similar tasks.

	Date:		09 June 2015 (broken out of main-line network.go)
	Author:		E. Scott Daniels

	Mods:
*/

package managers

import (
	"fmt"
	"strings"

	//"github.com/att/gopkgs/bleater"
	//"github.com/att/gopkgs/clike"
	//"github.com/att/gopkgs/ipc"
	"github.com/att/tegu"
	"github.com/att/tegu/gizmos"
)

// ------------ local structs -------------------------------------------------
type host_pair struct {
	usr	*string			// the user/tenant/project/squatter name/ID
	h1	*string
	h2	*string	
	fip	*string			// floating IP address needed for this segment
}


// ------------------------------------------------------------------------------------------------------------------

/*
	Search the list of endpoints looking for a router on the network given
*/
func (n *Network) find_router( netid *string ) ( epuuid *string ) {
	for u, ep := range n.endpts {
		if ep.Is_router() && *(ep.Get_netid()) == *netid {
			return &u
		}
	}

	return nil
}

/*
	Given two endpoint uuids (ep1 and ep2) determine if they are in the same project. If not, then construct
	an artifical endpoint such that the path is from host to router rather than host to host.  If both endpoints
	are 'insside' of the cloud, then we construct two pairs, otherwise we construct only one pair (from the 
	known endpoint to its router).

	Look at tid/h1 and tid/h2 and split them into two disjoint path endpoints, tid/gw,h1 and tid/gw,h2, if
	the project ids for the hosts differ.  This will allow for reservations between project VMs that are both
	known to Tegu.  If the endpoints are in different project, then we require each to have a floating point
	IP address that is known to us.

	If the project/project/user ID starts with a leading bang (!) then we assume it was NOT validated. If
	both are not validated we reject the attempt. If one is validated we build the path from the other
	endpoint to its gateway using the unvalidated endpoint as the external destination.  If one is not
	validated, but both IDs are the same, then we build the same path allowing user to use this as a
	shortcut and thus not needing to supply the same authorisation token twice on the request.

	We now allow a VM to make a reservation with an external address without a floating point IP
	since the VM's floating point IP is only needed on a reservation that would be made from
	the "other side".   If the VM doesn't have a floating IP, then it WILL be a problem if the reservation
	is being made between two tennants, but if for some odd reason a truely external IP address is
	(can be) associated with a VM, then we will not prohibit a reservation to an external IP address if
	the VM doesn't have a floating IP.
*/
func (n *Network) find_endpoints( epuuid1 *string, epuuid2 *string ) ( pair_list []host_pair, err error ) {
	ep1 := n.endpts[*epuuid1]
	ep2 := n.endpts[*epuuid2]

	if ep1 == nil && ep2 == nil {
		return nil, fmt.Errorf( "neither endpoint known to Tegu" )
	}

	nalloc := 2
	if ep1 == nil || ep2 == nil {			// only one is known, alloc just one
		nalloc = 1						
	} else {								// both known, see if they cross projects
		if *(ep1.Get_project()) == *(ep2.Get_project()) {				// same project; simple case hanle here
			pair_list = make( []host_pair, 1 )
			pair_list[0].h1 = epuuid1
			pair_list[0].h2 = epuuid2
			pair_list[0].usr = ep1.Get_project()
	net_sheep.Baa( 2, ">>>>> both eps are in same proejct, returning pair list: %d", len(pair_list) )
			return
		}
	}

	pair_list = make( []host_pair, nalloc )

// VERIFY -- do we need floating ip info in here any more?  I think not
	plidx := 0
	if ep1 != nil {											// find router for ep1 and set things up
		r1 := n.find_router( ep1.Get_netid() )				// find the ep uuid for the router
		if r1 == nil {
			return nil, fmt.Errorf( "unable to find a router for ep1 (%s) netid (%s)", *epuuid1, *ep1.Get_netid() )
		}

		pair_list[plidx].h1 = epuuid1
		pair_list[plidx].h2 = r1
		pair_list[plidx].usr = ep1.Get_project()

		plidx++
	}
	
	if ep2 != nil {											// find router for ep1 and set things up
		r2 := n.find_router( ep2.Get_netid() )				// find the ep uuid for the router
		if r2 == nil {
			return nil, fmt.Errorf( "unable to find a router for ep2 (%s) netid (%s)", epuuid1, ep1.Get_netid() )
		}

		pair_list[plidx].h1 = epuuid2
		pair_list[plidx].h2 = r2
		pair_list[plidx].usr = ep2.Get_project()
	}

	return
}


func (n *Network) deprecated_find_endpoints( h1ip *string, h2ip *string ) ( pair_list []host_pair, err error ) {
	var (
		h1_auth	bool = true			// initially assume both hosts were validated and we can make a complete connection if in different project
		h2_auth bool = true
	)

	err = nil

	if strings.Index( *h1ip, "/" ) < 0 {					// no project id in the name we have to assume in the same realm
		pair_list = make( []host_pair, 1 )
		pair_list[0].h1 = h1ip
		pair_list[0].h2 = h2ip
		pair_list[0].usr = nil
net_sheep.Baa( 2, "early>>>>> returning pair list: %d", len(pair_list) )
		return
	}

	nalloc := 2												// number to allocate if both validated
	toks := strings.SplitN( *h1ip, "/", 2 )					// suss out project ids
	t1 := toks[0]
	f1 := &toks[1]											// if !//ip given, the IP is the external and won't be in the hash
	if t1[0:1] == "!" {										// project wasn't validated, we use as endpoint, but dont create an end to end path
		h1_auth = false
		t1 =  t1[1:]										// drop the not authorised indicator for fip lookup later
		ah1 :=  (*h1ip)[1:]									// must also adjust h1 string for fip translation
		h1ip = &ah1
		nalloc--											// need one less in return vector
	}

	toks = strings.SplitN( *h2ip, "/", 2 )
	t2 := toks[0]
	f2 := &toks[1]											// if !//ip given, the IP is the external and won't be in the hash
	if  t2[0:1] ==  "!" {									// project wasn't validated, we use as endpoint, but dont create an end to end path
		h2_auth = false
		t2 =  t2[1:]
		ah2 :=  (*h2ip)[1:]									// must also adjust h2 string for fip translation
		h2ip = &ah2
		nalloc--											// need one less in return vector
	}

net_sheep.Baa( 2, ">>>>> find endpoints allocating %d", nalloc )
	if nalloc <= 0 {
		net_sheep.Baa( 1, "neither endpoint was validated, refusing to build a path for %s-%s", *h1ip, *h2ip )
		return
	}

	if t1 == t2 {									// same project, just one pair to deal with and we don't care if one wasn't validated
		pair_list = make( []host_pair, 1 )
		pair_list[0].h1 = h1ip
		pair_list[0].h2 = h2ip
		pair_list[0].usr = &t1
		return
	}

	if !h1_auth && t1 == "" {								// external address as src
		h1_auth = false										// ensure this
		f2 = n.ip2fip[*h2ip]								// must assume h2 is the good address, and it must have a fip
	} else {												// external address as dest
		if  !h2_auth && t2 == "" {							// external address specified as !//ip-address; we use f2 as captured earlier
			h2_auth = false									// should be, but take no chances
			f1 = n.ip2fip[*h1ip]							// must assume f1 is the good address and it musht have a fip
		} else {											// both are VMs and should have mapped fips; alloc based on previous validitiy check
			f1 = n.ip2fip[*h1ip]							// dig the floating point IP address for each host (used as dest for flowmods on ingress rules)
			f2 = n.ip2fip[*h2ip]
		}
	}

	zip := "0.0.0.0"										// dummy which allows vm-name,!//ipaddress without requiring vm to have a floating point ip
	if f1 == nil {
		f1 = &zip											// possible VM-name without fip -> !//IPaddr
	}

	if f2 == nil {
		f2 = &zip											// possible !//IPaddr -> vm-name without fip
	}

	if f1 == f2 {											// one of the two must have had some kind of external address (floating IP or real IP)
		net_sheep.Baa( 1, "find_endpoints: neither host had an external or floating IP: %s %s", *h1ip, *h2ip )
		return
	}

	//g1 := n.gateway4tid( t1 )						// map project id to gateway which become the second endpoint
	//g2 := n.gateway4tid( t2 )
	g1 := n.vmip2gw[*h1ip]							// pick up the gateway for each of the VMs
	g2 := n.vmip2gw[*h2ip]

	h2i := nalloc - 1								// insertion point for h2 into pair list
	pair_list = make( []host_pair, nalloc )			// build the list based on number of validated

	if h1_auth {
		pair_list[0].h1 = h1ip
		pair_list[0].h2 = g1
		pair_list[0].usr = &t1
		pair_list[0].fip = f2							// destination fip for h1->h2 (aka fip of h2)
	} else {
		if g2 == nil {
			net_sheep.Baa( 1, "h1 was not validated, creating partial path reservation no-g2-router??? <-> %s", *h2ip )
			err = fmt.Errorf( "unable to create partial pair reservation to %s: no router", *h2ip )
		} else {
			net_sheep.Baa( 1, "h1 was not validated, creating partial path reservation %s <-> %s", *g2, *h2ip )
		}
	}

	if h2_auth {
		pair_list[h2i].h2 = h2ip
		pair_list[h2i].h1 = g2							// order is important to ensure bandwidth in/out limits if different
		pair_list[h2i].usr = &t2
		pair_list[h2i].fip = f1							// destination fip for h1<-h2	(aka fip of h1)
	} else {
		if g1 == nil {
			net_sheep.Baa( 1, "h2 was not validated (or external), creating partial path reservation no-g1-router???? <-> %s",  *h1ip )
			err = fmt.Errorf( "unable to create partial pair reservation to %s: no router", *h2ip )
		} else {
			net_sheep.Baa( 1, "h2 was not validated (or external), creating partial path reservation %s <-> %s", *g1, *h1ip )
		}
	}

net_sheep.Baa( 2, ">>>>> returning pair list: %d", len(pair_list) )
	return
}

/*
	This is a helper function for find_paths and is invoked when we are interested in just the shortest
	path between two switches. It will find the shortest path, and then build a path structure which
	represents it.  ssw is the starting switch and h2nm is the endpoint "name" (probably a mac that we
	are looking for.
	
	The usr_max value is the percentage (1-100) that indicates the maximum percentage of a link that the
	user may reserve.

	This function assumes that the switches have all been initialised with a reset of the visited flag,
	setting of inital cost, etc.
*/
//func (n *Network) find_shortest_path( ssw *gizmos.Switch, h1 *gizmos.Host, h2 *gizmos.Host, usr *string, commence int64, conclude int64, inc_cap int64, usr_max int64 ) ( path *gizmos.Path, cap_trip bool ) {
func (n *Network) find_shortest_path( ssw *gizmos.Switch, h1 *gizmos.Endpt, h2 *gizmos.Endpt, usr *string, commence int64, conclude int64, inc_cap int64, usr_max int64 ) ( path *gizmos.Path, cap_trip bool ) {
	//deprecated ---- h1nm := h1.Get_mac()				// path finding at the switch level is MAC based.
	//deprecated ---- h2nm := h2.Get_mac()
	h1nm := h1.Get_meta_value( "uuid" )				// path finding at the switch level is uuid based.
	h2nm := h2.Get_meta_value( "uuid" )
	_, port1 := h1.Get_switch_port()
	_, port2 := h2.Get_switch_port()
	path = nil

	if usr_max <= 0 {
		//deprecated --- i41, _ := h1.Get_addresses()
		//deprecated --- i42, _ := h2.Get_addresses()
		//deprecated --- /net_sheep.Baa( 1, "no path generated: user link capacity set to 0: attempt %s -> %s", *i41, *i42 )
		net_sheep.Baa( 1, "no path generated: user link capacity set to 0: attempt %s -> %s", *(h1.Get_meta_value("uuid")), *(h1.Get_meta_value("uuid")) )
		return
	}

	ssw.Cost = 0														// seed the cost in the source switch
	tsw, cap_trip := ssw.Path_to( h2nm, commence, conclude, inc_cap, usr, usr_max )		// discover the shortest path to terminating switch that has enough bandwidth
	if tsw != nil {												// must walk from the term switch backwards collecting the links to set the path
		path = gizmos.Mk_path( h1, h2 )
		path.Set_reverse( true )								// indicate that the path is saved in reverse order
		path.Set_bandwidth( inc_cap )
		net_sheep.Baa( 2,  "find_spath: found target on %s", tsw.To_str( ) )
				
		//deprecated --- lnk := n.find_vlink( *(tsw.Get_id()), h2.Get_port( tsw ), -1, nil, nil )		// add leafpoint -- a virtual link out from switch to h2
		lnk := n.find_vlink( *(tsw.Get_id()), port2, -1, nil, nil )		// add leafpoint -- a virtual link out from switch to h2
		lnk.Add_lbp( *h2nm )
		lnk.Set_forward( tsw )												// leafpoints have only a forward link
		path.Add_leafpoint( lnk )

		for ; tsw != nil ; {
			if tsw.Prev != nil {								// last node won't have a prev pointer so no link
				lnk = tsw.Prev.Get_link( tsw.Plink )
				path.Add_link( lnk )
			}	
			path.Add_switch( tsw )

			net_sheep.Baa( 3, "\t%s using link %d", tsw.Prev.To_str(), tsw.Plink )

			if tsw.Prev == nil {															// last switch in the path, add leafpoint
				//deprecated --- lnk = n.find_vlink( *(tsw.Get_id()), h1.Get_port( tsw ), -1, nil, nil )		// endpoint is a virt link from switch to h1
				lnk = n.find_vlink( *(tsw.Get_id()), port1, -1, nil, nil )					// leafpoint is a virt link from switch to h1
				lnk.Add_lbp( *h1nm )
				lnk.Set_forward( tsw )												// endpoints have only a forward link
				path.Add_leafpoint( lnk )
			}
			tsw = tsw.Prev
		}

		path.Flip_leafpoints()		// path expects them to be in h1,h2 order; we added them backwards so must flip
	}

if path != nil {
net_sheep.Baa( 2, ">>> returning path: %s", path )
}
	return
}

/*
	This is a helper function for find_paths(). It is used to find all possible paths between h1 and h2 starting at ssw.
	The resulting path is a "scramble" meaning that the set of links is a unique set of links that are traversed by
	one or more paths. The list of links can be traversed without the need to dup check which is beneficial for
	increasing/decreasing the utilisaition on the link.  From a scramble, only end point queues can be set as the
	middle switches are NOT maintained.

	usr is the name of the user that the reservation is being processed for (project in openstack). The usr_max value
	is a percentage (1-100)  that defines the maximum of any link that the user may have reservations against or a hard
	limit if larger than 100.

*/
//func (n *Network) find_all_paths( ssw *gizmos.Switch, h1 *gizmos.Host, h2 *gizmos.Host, usr *string, commence int64, conclude int64, inc_cap int64, usr_max int64 ) ( path *gizmos.Path, err error ) {
func (n *Network) find_all_paths( ssw *gizmos.Switch, h1 *gizmos.Endpt, h2 *gizmos.Endpt, usr *string, commence int64, conclude int64, inc_cap int64, usr_max int64 ) ( path *gizmos.Path, err error ) {

	net_sheep.Baa( 1, "find_all: searching for all paths between %s  and  %s", *(h1.Get_mac()), *(h2.Get_mac()) )

	links, epsw, err := ssw.All_paths_to( h2.Get_mac(), commence, conclude, inc_cap, usr, usr_max )
	if err != nil {
		return
	}

	path = gizmos.Mk_path( h1, h2 )
	path.Set_scramble( true )
	path.Set_bandwidth( inc_cap )
	path.Add_switch( ssw )
	path.Add_switch( epsw )

	_, port1 := h1.Get_switch_port()
	_, port2 := h2.Get_switch_port()

	//deprecated--- lnk := n.find_vlink( *(ssw.Get_id()), h1.Get_port( ssw ), -1, nil, nil )			// add endpoint -- a virtual link out from switch to h1
	lnk := n.find_vlink( *(ssw.Get_id()), port1, -1, nil, nil )					// add leafpoint -- a virtual link out from switch to h1
	lnk.Add_lbp( *(h1.Get_mac()) )
	lnk.Set_forward( ssw )
	path.Add_leafpoint( lnk )

	//deprecated ---lnk = n.find_vlink( *(epsw.Get_id()), h2.Get_port( epsw ), -1, nil, nil )		// add endpoint -- a virtual link out from switch to h2
	lnk = n.find_vlink( *(epsw.Get_id()), port2, -1, nil, nil )					// add leafpoint -- a virtual link out from switch to h2
	lnk.Add_lbp( *(h2.Get_mac()) )
	lnk.Set_forward( epsw )
	path.Add_leafpoint( lnk )

	for i := range links {
		path.Add_link( links[i] )
	}

	return
}

/*
	A helper function for find_paths() that is used when running in 'relaxed' mode. In relaxed mode we don't
	actually find a path between the endpoints as we aren't doign admission control, but need to simulate
	a path in order to set up the flow-mods on the endpoints correctly.
*/
//func (n *Network) find_relaxed_path( sw1 *gizmos.Switch, h1 *gizmos.Host, sw2 *gizmos.Switch, h2 *gizmos.Host ) ( path *gizmos.Path, err error ) {
func (n *Network) find_relaxed_path( sw1 *gizmos.Switch, h1 *gizmos.Endpt, sw2 *gizmos.Switch, h2 *gizmos.Endpt ) ( path *gizmos.Path, err error ) {

	net_sheep.Baa( 1, "find_lax: creating relaxed path between %s and %s", *(h1.Get_mac()), *(h2.Get_mac()) )

	path = gizmos.Mk_path( h1, h2 )
	path.Set_bandwidth( 0 )
	path.Add_switch( sw1 )
	path.Add_switch( sw2 )


	_, port1 := h1.Get_switch_port()
	_, port2 := h2.Get_switch_port()
	//deprecated ---- lnk := n.find_vlink( *(sw1.Get_id()), h1.Get_port( sw1 ), -1, nil, nil )	// add endpoint -- a virtual from sw1 out to the host h1
	lnk := n.find_vlink( *(sw1.Get_id()), port1, -1, nil, nil )					// add leafpoint -- a virtual from sw1 out to the host h1
	lnk.Add_lbp( *(h1.Get_mac()) )
	lnk.Set_forward( sw1 )
	path.Add_leafpoint( lnk )

	lnk = n.find_swvlink( *(sw1.Get_id()), *(sw2.Get_id()) )					// suss out or create a virtual link between the two
	lnk.Set_forward( sw2 )
	lnk.Set_backward( sw1 )
	path.Add_link( lnk )

	//deprecated --- lnk = n.find_vlink( *(sw2.Get_id()), h2.Get_port( sw2 ), -1, nil, nil )		// add endpoint -- a virtual link on sw2 out to the host h2
	lnk = n.find_vlink( *(sw2.Get_id()), port2, -1, nil, nil )					// add leafpoint -- a virtual link on sw2 out to the host h2
	lnk.Add_lbp( *(h2.Get_mac()) )
	lnk.Set_forward( sw2 )
	path.Add_leafpoint( lnk )

	return
}

/*
	Find a set of connected switches that can be used as a path beteeen
	hosts 1 and 2 (given by name; mac or ip).  Further, all links between from and the final switch must be able to
	support the additional capacity indicated by inc_cap during the time window between
	commence and conclude (unix timestamps).

	If the network is 'split' a host may appear to be attached to multiple switches; one with a real connection and
	the others are edge switches were we see an 'entry' point for the host from the portion of the network that we
	cannot visualise.  We must attempt to find a path between h1 using all of it's attached switches, and thus the
	return is an array of paths rather than a single path.


	h1nm and h2nm are likely going to be ip addresses as the main function translates any names that would have
	come in from the requestor.

	Extip is an external IP address that will need to be associated with the flow-mods and thus needs to be
	added to any path we generate.

	If mlag_paths is true, then we will find shortest path but add usage to all related mlag links in the path.

	If find_all is set, and mlog_paths is false, then we will suss out all possible paths between h1 and h2 and not
	just the shortest path.
*/
func (n *Network) find_paths( h1nm *string, h2nm *string, usr *string, commence int64, conclude int64, inc_cap int64, extip *string, ext_flag *string, find_all bool ) ( pcount int, path_list []*gizmos.Path, cap_trip bool ) {
	var (
		path	*gizmos.Path
		ssw 	*gizmos.Switch		// starting switch
		//h1		*gizmos.Host
		//h2		*gizmos.Host
		h1		*gizmos.Endpt
		h2		*gizmos.Endpt
		lnk		*gizmos.Link
		plidx	int = 0
		swidx	int = 0				// index into host's switch list
		err		error
		lcap_trip bool = false		// local capacity trip flag; indicates one or more paths blocked by capacity limits
	)

	if h1nm == nil || h2nm == nil {
		net_sheep.Baa( 1, "IER:	find_paths: one/both names is/are nil  h1 nil=%v  h2 nil=%v", h1nm == nil, h2nm == nil )
		return 0, nil, false
	}

	//h1 = n.hosts[*h1nm]
	h1 = n.endpts[*h1nm]
	if h1 == nil {
		path_list = nil
		net_sheep.Baa( 1,  "find-path: cannot find host(1) in network -- not reported by SDNC? %s", *h1nm )
		return
	}
	//h1nm = h1.Get_mac()			// must have the host's mac as our flowmods are at that level
	//h1mac := h1.Get_mac()

	//h2 = n.hosts[*h2nm]					// do the same for the second host
	h2 = n.endpts[*h2nm]
	if h2 == nil {
		path_list = nil
		net_sheep.Baa( 1,  "find-path: cannot find host(2) in network -- not reported by the SDNC? %s", *h2nm )
		return
	}
	//h2nm = h2.Get_mac()
	//h2mac := h2.Get_mac()			// must have the host's mac as our flowmods are at that level

/* deprecated -- we use endpoint names passed in now and not mac addresses 
	if h1nm == nil || h2nm == nil {			// this has never happened, but be parinoid
		pcount = 0
		path_list = nil
		net_sheep.Baa( 0, "CRI: find-path: internal error: either h1nm or h2nm was nil after get mac  [TGUNET005]" )
		return
	}
--- */
	net_sheep.Baa( 1,  ">>>find-path: both hosts found in network: %s  %s", *h1nm, *h2nm )

	path_list = make( []*gizmos.Path, len( n.links ) )		// we cannot have more in our path than the number of links (needs to be changed as this isn't good in the long run)
	pcount = 0
	net_sheep.Baa( 1,  ">>>find-path: both hosts found in network: %s  %s  (iniital path_list size is %d)", *h1nm, *h2nm, len( path_list) )

	ssw, p1 := h1.Get_switch_port()						// get the source switch and the port the VM is attached to
	// REVAMP -- in the world of floodlight a host might appear attached to multiple switches. we had to find all paths
	//deprecated -- for {													// we'll break after we've looked at all of the connection points for h1
		if plidx >= len( path_list ) {
			net_sheep.Baa( 0,  "CRI: find-path: internal error -- path size > num of links.  [TGUNET006]" )
			return
		}

		//deprecated -- 	ssw, _ = h1.Get_switch_port( swidx )				// get next switch that lists h1 as attached; we'll work 'out' from it toward h2
		if ssw == nil {										// no more source switches which h1 thinks it's attached to
			pcount = plidx
			if pcount <= 0 || swidx == 0 {
				net_sheep.Baa( 1, "find-path: early exit? no switch/port returned for h1 (%s) at aptrip=%v", h1, lcap_trip )
			}
			path_list = path_list[0:pcount]					// slice it down to size
			cap_trip = lcap_trip							// set with overall state
			return	plidx, path_list[0:plidx], cap_trip
		}

		fence := n.get_fence( usr )
		if ssw.Has_host( h1nm )  &&  ssw.Has_host( h2nm ) {			// if both hosts are on the same switch, there's no path if they both have the same port (both external to our view)
net_sheep.Baa( 1, ">>>> both endpoints on same switch" )
			//p1 := h1.Get_port( ssw )
			//p2 := h2.Get_port( ssw )

			_, p2 := h2.Get_switch_port( )							// need the port for the second endpoint so we can test to see if they dup or are on same switch
			if p1 < 0 || p1 != p2 {									// when ports differ we'll create/find the vlink between them	(in Tegu-lite port == -128 is legit and will dup)
				//m1 := h1.Get_mac( )
				//m2 := h2.Get_mac( )

				//lnk = n.find_vlink( *(ssw.Get_id()), p1, p2, m1, m2 )
				//lnk = n.find_vlink( *(ssw.Get_id()), p1, p2, h1mac, h2mac )
				lnk = n.find_vlink( *(ssw.Get_id()), p1, p2, h1nm, h2nm )			// use endpoint names
				has_room := true									// always room if relaxed mode, so start this way
				if ! n.relaxed {
					has_room, err = lnk.Has_capacity( commence, conclude, inc_cap, fence.Name, fence.Get_limit_max() ) 	// admission control if not in relaxed mode
				}
				if has_room {										// room for the reservation
					lnk.Add_lbp( *h1nm )		// REVAMP:  this is switching to uuid; was mac; make sure it doesn't break.
					//net_sheep.Baa( 1, "path[%d]: found target on same switch, different ports: %s  %d, %d", plidx, ssw.To_str( ), h1.Get_port( ssw ), h2.Get_port( ssw ) )
					net_sheep.Baa( 1, "path[%d]: found target on same switch, different ports: %s  %d, %d", plidx, ssw.To_str( ), p1, p2 )
					path = gizmos.Mk_path( h1, h2 )							// empty path
					path.Set_bandwidth( inc_cap )
					path.Set_extip( extip, ext_flag )
					path.Add_switch( ssw )
					path.Add_link( lnk )
	
					path_list[plidx] = path
					plidx++
				} else {
					lcap_trip = true
					if err != nil {
						net_sheep.Baa( 1, "path[%d]: hosts on same switch, virtual link cannot support bandwidth increase of %d: %s", plidx, inc_cap, err )
					} else {
						net_sheep.Baa( 1, "path[%d]: hosts on same switch, virtual link cannot support bandwidth increase of %d", plidx, inc_cap )
					}
				}
			}  else {					// debugging only
				net_sheep.Baa( 2,  "find-path: path[%d]: found target (%s) on same switch with same port: %s  %d, %d", plidx, *h2nm, ssw.To_str( ), p1, p2 )
				net_sheep.Baa( 2,  "find-path: host1-json= %s", h1.To_json( ) )
				net_sheep.Baa( 2,  "find-path: host2-json= %s", h2.To_json( ) )
			}
		} else {						// usual case, two named hosts and hosts are on different switches
			net_sheep.Baa( 1, "path[%d]: searching for path starting from switch: %s", plidx, ssw.To_str( ) )

			for sname := range n.switches {					// initialise the network for the walk
				n.switches[sname].Cost = 2147483647			// this should be large enough and allows cost to be int32
				n.switches[sname].Prev = nil
				n.switches[sname].Flags &= ^tegu.SWFL_VISITED
			}

			
			if n.relaxed {				
				//dsw, _ := h2.Get_switch_port( swidx )					// need the switch associated with the second host (dest switch)
				dsw, _ := h2.Get_switch_port( )							// need the switch associated with the second host (dest switch)
				path, err = n.find_relaxed_path( ssw, h1, dsw, h2 )		// no admissions control we fake a link between the two
				if err != nil {
					net_sheep.Baa( 1, "find_paths: find_relaxed failed: %s", err )
				}
			} else {
				if find_all {																		// find all possible paths not just shortest
					path, err = n.find_all_paths( ssw, h1, h2, usr, commence, conclude, inc_cap, fence.Get_limit_max() )		// find a 'scramble' path
					if err != nil {
						net_sheep.Baa( 1, "find_paths: find_all failed: %s", err )
					}
				} else {
					path, cap_trip = n.find_shortest_path( ssw, h1, h2, usr, commence, conclude, inc_cap, fence.Get_limit_max() )
					if cap_trip {
						lcap_trip = true
					}
				}
			}

			if path != nil {
				path.Set_extip( extip, ext_flag )
				path_list[plidx] = path
				plidx++
			}
		}

		//swidx++
	//}

	//cap_trip = lcap_trip
	net_sheep.Baa( 2, ">>>>>> returning: %d things in path list", plidx )
	return	plidx, path_list[0:plidx], lcap_trip		// slice it down to just what we actually used
}

/*
	Find all paths that are associated with the reservation.  This splits the h1->h2 request into
	two paths if h1 and h2 are in different projects.  The resulting paths in this case are between h1 and
	the gateway, and from the gateway to h2 (to preserve the h1->h2 directional signficance which is
	needed if inbound and outbound rates differ.  In order to build a good set of flow-mods for the split
	reservation, both VMs MUST have an associated floating point address which is then generated as a
	match point in the flow-mod.

	If find_all is true we will find all paths between each host, not just the shortest.  This should not
	be confused with finding all paths when the network is split as that _always_ happens and by default
	we find just the shortest path in each split network.

	Cap_trip indicates that one or more paths could not be found because of capacity issues. If this is
	set there is still a possibility that the path was not found because it doesn't exist, but a
	capacity limit was encountered before 'no path' was discovered.  The state of the flag is only
	valid if the pathcount returend is 0.

	rpath is true if this function is called to build the reverse path.  It is necessary in order to
	properly set the external ip address flag (src/dest).
*/
func (n *Network) build_paths( h1nm *string, h2nm *string, commence int64, conclude int64, inc_cap int64, find_all bool, rpath bool ) ( pcount int, path_list []*gizmos.Path, cap_trip bool ) {
	var (
		num int = 0				// must declare num as := assignment doesnt work when ipath[n] is in the list
		src_flag string = "-S"	// flags that indicate which direction the external address is
		dst_flag string = "-D"
		lcap_trip bool = false	// overall capacity caused failure indicator
		ext_flag *string		// src/dest flag associated with the external ip address of the path component
	)

	path_list = nil
	if n == nil { return }

	pair_list, err := n.find_endpoints( h1nm, h2nm )					// determine endpoints based on names that might have different projects (vm-vm, or vm-rtr vm-rtr)
	if err != nil {
		net_sheep.Baa( 1, "unable to build path: %s", err )
		return
	}
	if pair_list == nil {										// likely no fip for one or the other VMs
		net_sheep.Baa( 1, "internal mishap: pair list in build_path was nil" )
		return
	}

	net_sheep.Baa( 2, "path building: pair list has %d elements", len( pair_list ) )
	total_paths := 0
	ok_count := 0
	ipaths := make( [][]*gizmos.Path, len( pair_list ) )			// temp holder of each path list resulting from pair_list exploration

	if rpath {
		ext_flag = &src_flag
	} else {
		ext_flag = &dst_flag
	}
	for i := range pair_list {
		net_sheep.Baa( 3, "path building: process pair list %d", i )
		num, ipaths[i], cap_trip = n.find_paths( pair_list[i].h1, pair_list[i].h2, pair_list[i].usr, commence, conclude, inc_cap, pair_list[i].fip, ext_flag, find_all )	
net_sheep.Baa( 1, ">>>>> ipath count is %d/%d", num, len( ipaths ) )
		if num > 0 {
			total_paths += num
			ok_count++

			net_sheep.Baa( 2, "path building: looping over %d ipaths", len( ipaths[i] ) )
			for j := range ipaths[i] {
				if ipaths[i][j] != nil {
					ipaths[i][j].Set_usr( pair_list[i].usr )			// associate this user with the path; needed in order to delete user based utilisation
				} else {
			net_sheep.Baa( 1, ">>>>> j is nil: %d", j )
				}
			}
		} else {
			if pair_list[i].h1 != nil && pair_list[i].h2 != nil {											 // pair might be nil if no gateway; don't stack dump
				net_sheep.Baa( 1, "path not found between: %s and %s ctrip=%v", *pair_list[i].h1, *pair_list[i].h2, cap_trip )	
			} else {
				net_sheep.Baa( 1, "path not found between: %s and %s ctrip=%v", h1nm, h2nm,  cap_trip )
			}

			if cap_trip {
				lcap_trip = true
			}
		}

		if rpath {													// flip the src/dest flag for the second side of the path if two components
			ext_flag = &dst_flag
		} else {
			ext_flag = &src_flag
		}
	}

	if ok_count < len( pair_list ) {								// didn't find a good path for each pair
		net_sheep.Baa( 1, "did not find a good path for each pair; expected %d, found %d cap_trip=%v", len( pair_list ), ok_count, lcap_trip )
		pcount = 0
		cap_trip = lcap_trip
		return
	}

	if len( ipaths ) == 1 {
		path_list = ipaths[0]
		pcount = len( ipaths[0] )
	} else {
		path_list = make( []*gizmos.Path,  total_paths )
		pcount = 0
		for i := range ipaths {
			for j := range ipaths[i] {
				path_list[pcount] = ipaths[i][j]
				pcount++
			}
		}
	}

	return
}
