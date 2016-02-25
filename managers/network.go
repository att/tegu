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

	Mnemonic:	network
	Abstract:	Manages everything associated with a network. This module contains a
				goroutine which should be invoked from the tegu main and is responsible
				for managing the network graph and responding to requests for information about
				the network graph. As a part of the collection of functions here, there is also
				a tickler which causes the network graph to be rebuilt on a regular basis.

				The network manager goroutine listens to a channel for requests such as finding
				and reserving a path between two hosts, and generating a json representation
				of the network graph for outside consumption.

				TODO: need to look at purging links/vlinks so they don't bloat if the network changes

	Date:		24 November 2013
	Author:		E. Scott Daniels

	Mods:		19 Jan 2014 - Added support for host-any reservations.
				11 Feb 2014 - Support for queues on links rather than just blanket obligations per link.
				21 Mar 2014 - Added noop support to allow main to hold off driving checkpoint
							loading until after the driver here has been entered and thus we've built
							the first graph.
				03 Apr 2014 - Support for endpoints on the path.
				05 May 2014 - Added support for merging gateways into the host list when not using floodlight.
				18 May 2014 - Changes to allow cross tenant reservations.
				30 May 2014 - Corrected typo in error message
				11 Jun 2014 - Added overall link-headroom support
				25 Jun 2014 - Added user level link capacity limits.
				26 Jun 2014 - Support for putting vmid into graph and hostlist output.
				29 Jun 2014 - Changes to support user link limits.
				07 Jul 2014 - Added support for reservation refresh.
				15 Jul 2014 - Added partial path allocation if one endpoint is in a different user space and
							is not validated.
				16 Jul 2014 - Changed unvalidated indicator to bang (!) to avoid issues when
					vm names have a dash (gak).
				29 Jul 2014 - Added mlag support.
				31 Jul 2014 - Corrected a bug that prevented using a VM ID when the project name/id was given.
				11 Aug 2014 - Corrected bleat message.
				01 Oct 2014 - Corrected bleat message during network build.
				09 Oct 2014 - Turned 'down' two more bleat messages to level 2.
				10 Oct 2014 - Correct bi-directional link bug (228)
				30 Oct 2014 - Added support for !//ex-ip address syntax, corrected problem with properly
					setting the -S or -D flag for an external IP (#243).
				12 Nov 2014 - Change to strip suffix on phost map.
				17 Nov 2014 - Changes for lazy mapping.
				24 Nov 2014 - Changes to drop the requirement for both VMs to have a floating IP address
					if a cross tenant reservation is being made. Also drops the requirement that the VM
					have a floating IP if the reservation is being made with a host using an external
					IP address.
				11 Mar 2015 - Corrected bleat messages in find_endpoints() that was causing core dump if the
					g1/g2 information was missing. Corrected bug that was preventing uuid from being used
					as the endpoint 'name'.
				20 Mar 2014 - Added REQ_GET_PHOST_FROM_MAC code
				25 Mar 2015 - IPv6 changes.
				30 Mar 2014 - Added ability to accept an array of Net_vm blocks.
				18 May 2015 - Added discount support.
				26 May 2015 - Conversion to support pledge as an interface.
				08 Jun 2015 - Added support for dummy star topo.
					Added support for 'one way' reservations (non-virtual router so no real endpoint)
				16 Jun 2015 - Corrected possible core dump in host_info() -- not checking for nil name.
				18 Jun 2015 - Added oneway rate limiting and delete support.
 				02 Jul 2015 - Extended the physical host refresh rate.
				03 Sep 2015 - Correct nil pointer core dump cause.
				16 Dec 2015 - Strip domain name when we create the vm to phost map since openstack sometimes
					gives us fqdns and sometimes not, but we only ever get hostname from the topo side.
				17 Dec 2015 - Correct nil pointer crash trying to fill in the vm map.
				09 Jan 2016 - Fixed some "go vet" problems.
				10 Jan 2016 - Corrected typo in printf statement.
				27 Jan 2016 - Added ability to query user cap value.
				25 Feb 2016 - Corrected missing nil pointer check in find_vlink()
*/

package managers

import (
	"fmt"
	"math/rand"
	"os"
	"strings"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/clike"
	"github.com/att/gopkgs/ipc"
	//"github.com/att/tegu"
	"github.com/att/tegu/gizmos"
)

// --------------------------------------------------------------------------------------

// this probably should be network rather than Network as it's used only internally

/*
	Defines everything we need to know about a network.
*/
type Network struct {
	switches	map[string]*gizmos.Switch	// symtable of switches
	hosts		map[string]*gizmos.Host		// references to host by either mac, ipv4 or ipv6 'names'
	links		map[string]*gizmos.Link		// table of links allows for update without resetting allotments
	vlinks		map[string]*gizmos.Link		// table of virtual links (links between ports on the same switch)
	vm2ip		map[string]*string			// maps vm names and vm IDs to IP addresses (generated by ostack and sent on channel)
	ip2vm		map[string]*string			// reverse -- makes generating complete host listings faster
	ip2mac		map[string]*string			// IP to mac	Tegu-lite
	ip2vmid		map[string]*string			// ip to vm-id translation	Tegu-lite
	vmid2phost	map[string]*string			// vmid to physical host name	Tegu-lite
	vmip2gw		map[string]*string			// vmid to it's gateway
	vmid2ip		map[string]*string			// vmid to ip address	Tegu-lite
	mac2phost	map[string]*string			// mac to phost map generated from OVS agent data (needed to include gateways in graph)
	gwmap		map[string]*string			// mac to ip map for the gateways	(needed to include gateways in graph)
	ip2fip		map[string]*string			// projects/ip to floating ip address translation
	fip2ip		map[string]*string			// floating ip address to projects/ip translation
	limits		map[string]*gizmos.Fence	// user boundary defaults for per link caps
	mlags		map[string]*gizmos.Mlag		// reference to each mlag link group by name
	hupdate		bool						// set to true only if hosts is updated after gwmap has size (chkpt reload timing)
	relaxed		bool						// if true, we're in relaxed mode which means we don't path find or do admission control.
}


// ------------ private -------------------------------------------------------------------------------------

/*
	constructor
*/
func mk_network( mk_links bool ) ( n *Network ) {
	n = &Network { }
	n.switches = make( map[string]*gizmos.Switch, 20 )		// initial sizes aren't limits, but might help save space
	n.hosts = make( map[string]*gizmos.Host, 2048 )

	if mk_links {
		n.links = make( map[string]*gizmos.Link, 2048 )		// must maintain a list of links so when we rebuild we preserve obligations
		n.vlinks = make( map[string]*gizmos.Link, 2048 )
		n.mlags = make( map[string]*gizmos.Mlag, 2048 )
	}

	return
}

// -------------- map management (mostly tegu-lite) -----------------------------------------------------------
/*
	Set the relaxed mode in the network.
*/
func (n *Network) Set_relaxed( state bool ) {
	if n != nil {
		n.relaxed = state
	}
}

func (n *Network) Is_relaxed( ) ( bool ) {
	return n.relaxed
}

/*
	Using the various vm2 and ip2 maps, build the host array as though it came from floodlight.
*/
func (n *Network) build_hlist( ) ( hlist []gizmos.FL_host_json ) {

	i := 0
	if n != nil && n.ip2mac != nil {								// first time round we might not have any data
		gw_count := 0
		if n.gwmap != nil {
			gw_count = len( n.gwmap )
		}
		hlist = make( []gizmos.FL_host_json, len( n.ip2mac ) + gw_count )

		for ip, mac := range n.ip2mac {							// add in regular VMs
			vmid := n.ip2vmid[ip]
			if vmid != nil && mac != nil {						// skip if we don't find a vmid
				if n.vmid2phost[*vmid] != nil {
					net_sheep.Baa( 3, "adding host: [%d] mac=%s ip=%s phost=%s", i, *mac, ip, *(n.vmid2phost[*vmid]) )
					hlist[i] = gizmos.FL_mk_host( ip, "", *mac, *(n.vmid2phost[*vmid]), -128 ) 				// use phys host as switch name and -128 as port
					i++
				} else {
					net_sheep.Baa( 1, "did NOT add host: mac=%s ip=%s phost=NIL", *mac, ip )
				}
			} else {
			}
		}

		if n.gwmap != nil {										// add in the gateways which are not reported by openstack
			if n.mac2phost != nil && len( n.mac2phost ) > 0 {
				for mac, ip := range n.gwmap {
					if n.mac2phost[mac] == nil {
						net_sheep.Baa( 1, "WRN:  build_hlist: unable to find gw mac in mac2phost list: mac=%s  ip=%s  [TGUNET000]", mac, *ip )
					} else {
						if ip != nil {
							net_sheep.Baa( 3, "adding gateway: [%d] mac=%s ip=%s phost=%s", i, mac, *ip, *(n.mac2phost[mac]) )
							hlist[i] = gizmos.FL_mk_host( *ip, "", mac, *(n.mac2phost[mac]), -128 ) 		// use phys host collected from OVS as switch
							i++
						} else {
							net_sheep.Baa( 1, "WRN:  build_hlist: ip was nil for mac: %s  [TGUNET001]", mac )
						}
					}
				}
			} else {
				net_sheep.Baa( 1, "WRN: no phost2mac map -- agent likely not returned sp2uuid list  [TGUNET002]" )
			}
		} else {
			net_sheep.Baa( 1, "WRN: no gateway map  [TGUNET003]" )
		}

		hlist = hlist[0:i]
	} else {
		hlist = make( []gizmos.FL_host_json,  1 )			// must have empty list to return if net is nil
	}

	return
}

/*
	Using a net_vm struct update the various maps. Allows for lazy discovery of
	VM information rather than needing to request everything all at the same time.
*/
func (net *Network) insert_vm( vm *Net_vm ) {
	vname, vid, vip4, _, vphost, gw, vmac, vfip := vm.Get_values( )
	if vname == nil || *vname == "" || *vname == "unknown" {								// shouldn't happen, but be safe
		//return

	}

	if net.vm2ip == nil {							// ensure everything exists
		net.vm2ip = make( map[string]*string )
	}
	if net.ip2vm == nil {
		net.ip2vm = make( map[string]*string )
	}

	if net.vmid2ip == nil {
		net.vmid2ip = make( map[string]*string )
	}
	if net.ip2vmid == nil {
		net.ip2vmid = make( map[string]*string )
	}

	if net.vmid2phost == nil {
		net.vmid2phost = make( map[string]*string )
	}
	if net.mac2phost == nil {
		net.mac2phost = make( map[string]*string )
	}

	if net.ip2mac == nil {
		net.ip2mac = make( map[string]*string )
	}

	if net.fip2ip == nil {
		net.fip2ip = make( map[string]*string )
	}
	if net.ip2fip == nil {
		net.ip2fip = make( map[string]*string )
	}

	if net.gwmap == nil {
		net.gwmap = make( map[string]*string )
	}

	if net.vmip2gw == nil {
		net.vmip2gw = make( map[string]*string )
	}

	if vname != nil {
		net.vm2ip[*vname] = vip4
	}

	if vid != nil {
		net.vmid2ip[*vid] = vip4
		if vphost != nil {
			htoks := strings.Split( *vphost, "." )		// strip domain name
			//net.vmid2phost[*vid] = vphost
			net_sheep.Baa( 2, "vm2phost saving %s (%s) for %s", htoks[0], *vphost, *vid )
			net.vmid2phost[*vid] = &htoks[0]
		} else {
			net_sheep.Baa( 2, "vm2phost phys host is nil for %s", *vid )
		}
	}

	if vip4 != nil {
		net.ip2vmid[*vip4] = vid
		net.ip2vm[*vip4] = vname
		net.ip2mac[*vip4] = vmac
		net.ip2fip[*vip4] = vfip
		net.vmip2gw[*vip4] = gw
	}

	if vfip != nil {
		net.fip2ip[*vfip] = vip4
	}

	vgwmap := vm.Get_gwmap()					// don't assume that all gateways are present in every map
	if vgwmap != nil {							// as it may be just related to the VM and not every gateway
		for k, v := range vgwmap {
			net.gwmap[k] = v
		}
	}
}


/*
	Given a user name find a fence in the table, or copy the defaults and
	return those.
*/
func (n *Network) get_fence( usr *string ) ( *gizmos.Fence ) {
	var (
		fence *gizmos.Fence
	)

	fence = nil
	if usr != nil {
		fence = n.limits[*usr] 								// get the default fence settings for the user or the overall defaults if none supplied for the user
	} else {
		u := "nobody"
		usr = &u
	}

	if fence == nil {
		if n.limits["default"] != nil {
			fence = n.limits["default"].Copy( usr )			// copy the default to pick up the user name and pass that down
			n.limits[*usr] = fence
		} else {
			nm := "default"
			fence = gizmos.Mk_fence( &nm, 100, 0, 0 )		// create a generic default with no limits (100% max)
			n.limits["default"] = fence
			fence = fence.Copy( usr )
		}
	}

	return fence
}

/*	Given a user name find a fence in the table and return the maximum value.
	If there is no fence, 0 is returned.
*/
func (n *Network) get_fence_max( usr *string ) ( int64 ) {

	if usr == nil {
		return 0
	}

	fence := n.limits[*usr] 								// get the default fence settings for the user or the overall defaults if none supplied for the user
	if fence != nil {
		return fence.Get_limit_max()
	}

	return 0
}

/*
	Takes a set of strings of the form <hostname><space><mac> and adds them to the mac2phost table
	This is needed to map gateway hosts to physical hosts since openstack does not return the gateways
	with the same info as it does VMs
*/
func (n *Network) update_mac2phost( list []string, phost_suffix *string ) {
	if n.mac2phost == nil {
		n.mac2phost = make( map[string]*string )
	}

	for i := range list {
		toks := strings.Split( list[i], " " )
		dup_str := toks[0]
		if phost_suffix != nil {								// if we added a suffix to the host, we must strip it away
			stoks := strings.Split( toks[0], *phost_suffix )
			dup_str = stoks[0]
		}
		n.mac2phost[toks[1]] = &dup_str
	}

	net_sheep.Baa( 2, "mac2phost map updated; has %d elements (list had %d elements)", len( n.mac2phost ), len( list ) )
}

/*
	Build the ip2vm map from the vm2ip map which is a map of IP addresses to what we hope is the VM
	name.  The vm2ip map contains both VM IDs and the names the user assigned to the VM. We'll guess
	at getting the name from the map.

	TODO: we need to change vm2ip to be struct with two maps rather than putting IDs and names into the same map
*/
func (n *Network) build_ip2vm( ) ( i2v map[string]*string ) {

	i2v = make( map[string]*string )

	for k, v := range n.vm2ip {
		if len( k ) != 36 || strings.Index( k, "." ) > 0  || i2v[*v] == nil {		// uuids are 36, so if it's not it's in, or if it has a dot as IPv4 addrs do, or if we have nothing yet
			dup_str := k							// 'dup' the string so we don't reference the string associated with the other map
			i2v[*v] = &dup_str
			net_sheep.Baa( 3, "build_ip2vm [%s] --> %s", *v, *i2v[*v] )
		} else {
			net_sheep.Baa( 3, "build_ip2vm skipped index on: %s;  seems not to be IP addr len=%d", k, len( k ) )
		}
	}

	net_sheep.Baa( 2, "built ip2vm map: %d entries", len( i2v ) )
	return
}


/*
	Accepts a list (string) of queue data information segments (swid/port,res-id,queue,min,max,pri), splits
	the list based on spaces and adds each information segment to the queue map.  If ep_only is true,
	then we drop all middle link queues (priority-in priority-out).

	(Supports gen_queue_map and probably not useful for anything else)
*/
func qlist2map( qmap map[string]int, qlist *string, ep_only bool ) {
	qdata := strings.Split( *qlist, " " )					// tokenise (if multiple swid/port,res-id,queue,min,max.pri)

	if ep_only {
		for i := range qdata  {
			if qdata[i] != ""  &&  strings.Index( qdata[i], "priority-" ) < 0 {
				qmap[qdata[i]] = 1;
			}
		}
	} else {
		for i := range qdata {
			if qdata[i] != "" {
				qmap[qdata[i]] = 1;
			}
		}
	}
}

/*
	Traverses all known links and generates a switch queue map based on the queues set for
	the time indicated by the timestamp passed in (ts).

	If ep_only is set to true, then queues only for endpoints are generated.

	TODO:  this needs to return the map, not an array (fqmgr needs to change to accept the map)
*/
func (n *Network) gen_queue_map( ts int64, ep_only bool ) ( qmap []string, err error ) {
	err = nil									// at the moment we are always successful
	seen := make( map[string]int, 100 )			// prevent dups which occur because of double links

	for _, link := range n.links {				// for each link in the graph
		s := link.Queues2str( ts )
		qlist2map( seen, &s, ep_only )	// add these to the map
	}

	for _, link := range n.vlinks {				// and do the same for vlinks
		s := link.Queues2str( ts )
		qlist2map( seen, &s, ep_only )			// add these to the map
	}

	qmap = make( []string, len( seen ) )
	i := 0
	for data := range seen {
		net_sheep.Baa( 2, "gen_queue_map[%d] = %s", i, data )
		qmap[i] = data
		i++
	}
	net_sheep.Baa( 1, "gen_queue_map: added %d queue tokens to the list (len=%d)", i, len( qmap ) )

	return
}

/*
	Returns the ip address associated with the name. The name may indeed be
	an IP address which we'll look up in the hosts table to verify first.
	If it's not an ip, then we'll search the vm2ip table for it.

	If the name passed in has a leading bang (!) meaning that it was not
	validated, we'll strip it and do the lookup, but will return the resulting
	IP address with a leading bang (!) to propagate the invalidness of the address.

	The special case !/ip-address is used to designate an external address. It won't
	exist in our map, and we return it as is.
*/
func (n *Network) name2ip( hname *string ) (ip *string, err error) {
	ip = nil
	err = nil
	lname := *hname								// lookup name - we may have to strip leading !

	if *hname == "" {
		net_sheep.Baa( 1, "bad name passed to name2ip: empty" )
		err = fmt.Errorf( "host unknown: empty name passed to network manager" )
		return
	}

	if (*hname)[0:2] == "!/" {					// special external name (no project string following !)
		ip = hname
		return
	}

	if  (*hname)[0:1] == "!" {					// ignore leading bang which indicate unvalidated IDs
		lname = (*hname)[1:]
	}

	if n.hosts[lname] != nil {					// we have a host by 'name', then 'name' must be an ip address
		ip = hname
	} else {
		ip = n.vm2ip[lname]						// it's not an ip, try to translate it as either a VM name or VM ID
		if ip == nil {							// maybe it's just an ID, try without
			tokens := strings.Split( lname, "/" )				// could be project/uuid or just uuid
			lname = tokens[len( tokens ) -1]	// local name is the last token
			ip = n.vmid2ip[lname]				// see if it maps to an ip
		}
		if ip != nil {							// the name translates, see if it's in the known net
			if n.hosts[*ip] == nil {			// ip isn't in the network scope as a host, return nil
				err = fmt.Errorf( "host unknown: %s maps to an IP, but IP not known to SDNC: %s", *hname, *ip )
				ip = nil
			} else {
				if (*hname)[0:1] == "!" {					// ensure that we return the ip with the leading bang
					lname = "!" + *ip
					ip = &lname
				}
			}
		} else {
			err = fmt.Errorf( "host unknown: %s could not be mapped to an IP address", *hname )
		}
	}

	return
}

/*
	Given two switch names see if we can find an existing link in the src->dest direction
	if lnk is passed in, that is passed through to Mk_link() to cause lnk's obligation to be
	'bound' to the link that is created here.

	If the link between src-sw and dst-sw is not there, one is created and added to the map.

	Mlag is a pointer to the string which is the name of the mlag group that this link belongs to.

	We use this to reference the links from the previously created graph so as to preserve obligations.
	(TODO: it would make sense to vet the obligations to ensure that they can still be met should
	a switch depart from the network.)
*/
func (n *Network) find_link( ssw string, dsw string, capacity int64, link_alarm_thresh int, mlag  *string, lnk ...*gizmos.Link ) (l *gizmos.Link) {

	id := fmt.Sprintf( "%s-%s", ssw, dsw )
	l = n.links[id]
	if l != nil {
		if lnk != nil {										// dont assume that the links shared the same allotment previously though they probably do
			l.Set_allotment( lnk[0].Get_allotment( ) )
		}
		return
	}

	net_sheep.Baa( 3, "making link: %s", id )
	if lnk == nil {
		l = gizmos.Mk_link( &ssw, &dsw, capacity, link_alarm_thresh, mlag );
	} else {
		l = gizmos.Mk_link( &ssw, &dsw, capacity, link_alarm_thresh, mlag, lnk[0] );
	}

	if mlag != nil {
		ml := n.mlags[*mlag]
		if ml == nil {
			n.mlags[*mlag] = gizmos.Mk_mlag( mlag, l.Get_allotment() )		// creates the mlag group and adds the link
		} else {
			n.mlags[*mlag].Add_link( l.Get_allotment() ) 					// group existed, just add the link
		}
	}

	n.links[id] = l
	return
}

/*
	Looks for a virtual link on the switch given between ports 1 and 2.
	Returns the existing link, or makes a new one if this is the first.
	New vlinks are stashed into the vlink hash.

	We also create a virtual link on the endpoint between the switch and
	the host. In this situation there is only one port (p2 expected to be
	negative) and the id is thus just sw.port.

	m1 and m2 are the mac addresses of the hosts; used to build different
	links since their ports will be -128 when not known in advance.
*/
func (n Network) find_vlink( sw string, p1 int, p2 int, m1 *string, m2 *string ) ( l *gizmos.Link ) {
	var(
		id string
	)

	if m1 == nil {					// no mac id known (more often than not), generate something.
		rn := fmt.Sprintf( "%d", rand.Intn( 32765 ) )
		m1 = &rn
	}

	if m2 == nil {					// no mac id known (more often than not), generate something.
		rn := fmt.Sprintf( "%d", rand.Intn( 32765 ) )
		m2 = &rn
	}

	if p2 < 0 {
		if p2 == p1 {
			id = fmt.Sprintf( "%s.%s.%s", sw, *m1, *m2 )		// late binding, we don't know port, so use mac for ID
			net_sheep.Baa( 2, "late binding id: %s", id )
		} else {
			id = fmt.Sprintf( "%s.%d", sw, p1 )					// endpoint -- only a forward link to p1
		}
	} else {
		id = fmt.Sprintf( "%s.%d.%d", sw, p1, p2 )
	}

	l = n.vlinks[id]
	if l == nil {
		l = gizmos.Mk_vlink( &sw, p1, p2, int64( 10 * ONE_GIG ) )
		l.Set_ports( p1, p2 )
		n.vlinks[id] = l
	}

	return
}

/*
	Find a virtual link between two switches -- used when in relaxed mode and no real path
	between endpoints is found, but we still need to pretend there is a path. If we don't
	have a link in the virtual table we'll create one and return that.
*/
func (n Network) find_swvlink( sw1 string, sw2 string  ) ( l *gizmos.Link ) {

	id := fmt.Sprintf( "%s.%s", sw1, sw2 )

	l = n.vlinks[id]
	if l == nil {
		l = gizmos.Mk_link( &sw1, &sw2, int64( 10 * ONE_GIG ), 99, nil )		// create the link and add to virtual table
		l.Set_ports( 1024, 1024 )
		n.vlinks[id] = l
	}

	return
}

/*
	Build a new graph of the network.
	Host is the name/ip:port of the host where floodlight is running.
	Old-net is the reference net that we'll attempt to find existing links in.
	Max_capacity is the generic (default) max capacity for each link.

	Tegu-lite:  sdnhost might be a file which contains a static graph, in json form,
	describing the physical network. The string is assumed to be a filename if it
	does _not_ contain a ':'.

*/
func build( old_net *Network, flhost *string, max_capacity int64, link_headroom int, link_alarm_thresh int, host_list *string ) (n *Network) {
	var (
		ssw		*gizmos.Switch
		dsw		*gizmos.Switch
		lnk		*gizmos.Link
		ip4		string
		ip6		string
		links	[]gizmos.FL_link_json			// list of links from floodlight or simulated floodlight source
		hlist	[]gizmos.FL_host_json			// list of hosts from floodlight or built from vm maps if not using fl
		err		error
		hr_factor	int64 = 1
		mlag_name	*string = nil
	)

	n = nil

	if link_headroom > 0 && link_headroom < 100 {
		hr_factor = 100 - int64( link_headroom )
	}


	// REVAMP:   eliminate the floodlight call openstack interface should be sending us a list of endpoints to use; use them directly
	if strings.Index( *flhost, ":" ) >= 0  {
		links = gizmos.FL_links( flhost )					// request the current set of links from floodlight
		hlist = gizmos.FL_hosts( flhost )					// get a current host list from floodlight
	} else {
		hlist = old_net.build_hlist()						// simulate output from floodlight by building the host list from openstack maps
		links, err = gizmos.Read_json_links( *flhost )		// build links from the topo file; if empty/missing, we'll generate a dummy next
		if err != nil || len( links ) <= 0 {
			if host_list != nil {
				net_sheep.Baa_some( "star", 500, 1, "generating a dummy star topology: json file empty, or non-existent: %s", *flhost )
				links = gizmos.Gen_star_topo( *host_list )				// generate a dummy topo based on the host list
			} else {
				net_sheep.Baa( 0, "ERR: unable to read static links from %s: %s  [TGUNET004]", *flhost, err )
				links = nil										// kicks us out later, but must at least create an empty network first
			}
		}
	}

	n = mk_network( old_net == nil )			// new network, need links and mlags only if it's the first network
	if old_net == nil {
		old_net = n								// prevents an if around every try to find an existing link.
	} else {
		n.links = old_net.links					// might it be wiser to copy this rather than reference and update the 'live' copy?
		n.vlinks = old_net.vlinks
		n.mlags = old_net.mlags
		n.relaxed = old_net.relaxed
	}

	if links == nil {
		return
	}
	if hlist == nil {
		return
	}

	for i := range links {								// parse all links returned from the controller (build our graph of switches and links)
		if links[i].Capacity <= 0 {
			links[i].Capacity = max_capacity			// default if it didn't come from the source
		}

		tokens := strings.SplitN( links[i].Src_switch, "@", 2 )	// if the 'id' is host@interface we need to drop interface so all are added to same switch
		sswid := tokens[0]
		tokens = strings.SplitN( links[i].Dst_switch, "@", 2 )
		dswid := tokens[0]

		ssw = n.switches[sswid]
		if ssw == nil {
			ssw = gizmos.Mk_switch( &sswid )
			n.switches[sswid] = ssw
		}

		dsw = n.switches[dswid]
		if dsw == nil {
			dsw = gizmos.Mk_switch( &dswid )
			n.switches[dswid] = dsw
		}

		// omitting the link (last parm) causes reuse of the link if it existed so that obligations are kept; links _are_ created with the interface name
		lnk = old_net.find_link( links[i].Src_switch, links[i].Dst_switch, (links[i].Capacity * hr_factor)/100, link_alarm_thresh, links[i].Mlag )
		lnk.Set_forward( dsw )
		lnk.Set_backward( ssw )
		lnk.Set_port( 1, links[i].Src_port )		// port on src to dest
		lnk.Set_port( 2, links[i].Dst_port )		// port on dest to src
		ssw.Add_link( lnk )

		if links[i].Direction == "bidirectional" { 			// add the backpath link
			mlag_name = nil
			if links[i].Mlag != nil {
				mln := *links[i].Mlag + ".REV"				// differentiate the reverse links so we can adjust them with amount_in more easily
				mlag_name = &mln
			}
			lnk = old_net.find_link( links[i].Dst_switch, links[i].Src_switch, (links[i].Capacity * hr_factor)/100, link_alarm_thresh, mlag_name )
			lnk.Set_forward( ssw )
			lnk.Set_backward( dsw )
			lnk.Set_port( 1, links[i].Dst_port )		// port on dest to src
			lnk.Set_port( 2, links[i].Src_port )		// port on src to dest
			dsw.Add_link( lnk )
			net_sheep.Baa( 3, "build: addlink: src [%d] %s %s", i, links[i].Src_switch, n.switches[sswid].To_json() )
			net_sheep.Baa( 3, "build: addlink: dst [%d] %s %s", i, links[i].Dst_switch, n.switches[dswid].To_json() )
		}
	}

	if len( old_net.gwmap ) > 0 {			// if we build after gateway map has size, then gateways are in host table and checkpoints can be processed
		n.hupdate = true
	} else {
		n.hupdate = false
	}

	// REVAMP:  expect hlist to be a list of endpoints which we just need to map to switches and add.
	for i := range hlist {			// parse the unpacked json (host list); structs are very dependent on the floodlight output; TODO: change FL_host to return a generic map
		if len( hlist[i].Mac )  > 0  && len( hlist[i].AttachmentPoint ) > 0 {		// switches come back in the list; if there are no attachment points we assume it's a switch & drop
			ip6 = ""
			ip4 = ""

			if len( hlist[i].Ipv4 ) > 0 { 							// floodlight returns them all in a list; openstack produces them one at a time, so for now this doesn't snarf everything
				if strings.Index( hlist[i].Ipv4[0], ":" ) >= 0 {	// if emulating floodlight, we get both kinds of IP in this field, one per hlist entry (ostack yields unique mac for each ip)
					ip6 = hlist[i].Ipv4[0];
				} else {
					ip4 = hlist[i].Ipv4[0];
				}
			}
			if len( hlist[i].Ipv6 ) > 0 &&  hlist[i].Ipv6[0] != "" {			// if getting from floodlight, then it will be here, and we may have to IPs on the same NIC
				ip6 = hlist[i].Ipv6[0];
			}

			h := gizmos.Mk_host( hlist[i].Mac[0], ip4, ip6 )
			vmid := &empty_str
			if old_net.ip2vmid != nil {
				key := ip4
				if key == "" {
					key = ip6
				}
				
				if old_net.ip2vmid[key] != nil {
					vmid = old_net.ip2vmid[key]
					h.Add_vmid( vmid )
				}
			}

			for j := range hlist[i].AttachmentPoint {
				h.Add_switch( n.switches[hlist[i].AttachmentPoint[j].SwitchDPID], hlist[i].AttachmentPoint[j].Port )
				ssw = n.switches[hlist[i].AttachmentPoint[j].SwitchDPID]
				if ssw != nil {																	// it should always be known, but no chances
					ssw.Add_host( &hlist[i].Mac[0], vmid, hlist[i].AttachmentPoint[j].Port )	// allows switch to provide has_host() method
					net_sheep.Baa( 4, "saving host %s in switch : %s port: %d", hlist[i].Mac[0], hlist[i].AttachmentPoint[j].SwitchDPID, hlist[i].AttachmentPoint[j].Port )
				}
			}

			n.hosts[hlist[i].Mac[0]] = h			// reference by mac and IP addresses (when there)
			net_sheep.Baa( 3, "build: saving host ip4=(%s)  ip6=(%s) as mac: %s", ip4, ip6, hlist[i].Mac[0] )
			if ip4 != "" {
				n.hosts[ip4] = h
			}
			if ip6 != "" {
				n.hosts[ip6] = h
			}
		} else {
			net_sheep.Baa( 2, "skipping host in list (i=%d) attachment points=%d", i, len( hlist[i].Mac ) )
		}
	}

	return
}

/*
	DEPRECATED
	Given a project id, find the associated gateway.  Returns the whole project/ip string.

	TODO: return list if multiple gateways
	TODO: improve performance by maintaining a tid->gw map
*/
func (n *Network) gateway4tid( tid string ) ( *string ) {
	for _, ip := range n.gwmap {				// mac to ip, so we have to look at each value
		toks := strings.SplitN( *ip, "/", 2 )
		if toks[0] == tid {
			return ip
		}
	}

	return nil
}

/*
	Given a host name, generate various bits of information like mac address, switch and switch port.
	Error is set if we cannot find the box.
*/
func (n *Network) host_info( name *string ) ( ip *string, mac *string, swid *string, swport int, err error ) {
	var (
		h	*gizmos.Host
		ok 	bool
	)

	mac = nil

	if name == nil {
		err = fmt.Errorf( "cannot translate nil name" )
		return
	}

	if ip, ok = n.vm2ip[*name]; !ok {		// assume that IP was given instead of name (gateway)
		//err = fmt.Errorf( "cannot translate vm to an IP address: %s", *name )
		//return
		ip = name
	}

	mac = n.ip2mac[*ip]
	if mac != nil {
		if h, ok = n.hosts[*mac]; !ok {
			err = fmt.Errorf( "cannot find host (representation) for host %s based on IP and MAC: %s %s", *name, *ip, *mac )
			return
		}
	} else {
		err = fmt.Errorf( "cannot translate IP to MAC: %s", *ip )
		return
	}

	sw, swport := h.Get_switch_port( 0 )			// we'll blindly assume it's not a split network
	if sw != nil {
		swid = sw.Get_id()
	} else {
		err = fmt.Errorf( "cannot generate switch/port for %s", *name )
		return
	}

	return
}

// --------------------  info exchange/debugging  -----------------------------------------------------------------------------------------

/*
	Generate a json list of hosts which includes ip, name, switch(es) and port(s).
*/
func (n *Network) host_list( ) ( jstr string ) {
	var(
		sep 	string = ""
		hname	string = ""
		seen	map[string]bool
	)

	seen = make( map[string]bool )
	jstr = ` [ `						// an array of objects

	if n != nil && n.hosts != nil {
		for _, h := range n.hosts {
			ip4, ip6 := h.Get_addresses()
			mac :=  h.Get_mac()						// track on this as we will always see this

			if seen[*mac] == false {
				seen[*mac] = true;					// we track hosts by both mac and ip so only show once

				if n.ip2vm[*ip4] != nil {
					hname = *n.ip2vm[*ip4]
				} else {
					hname = "unknown"
				}
				vmid := "unknown"
				if n.ip2vmid[*ip4] != nil {
					vmid = *n.ip2vmid[*ip4]
				}
				jstr += fmt.Sprintf( `%s { "name": %q, "vmid": %q, "mac": %q, "ip4": %q, "ip6": %q `, sep, hname, vmid, *(h.Get_mac()), *ip4, *ip6 )
				if nconns := h.Get_nconns(); nconns > 0 {
					jstr += `, "conns": [`
					sep = ""
					for i := 0; i < nconns; i++ {
						sw, port := h.Get_switch_port( i )
						if sw == nil {
							break
						}

						jstr += fmt.Sprintf( `%s { "switch": %q, "port": %d }`, sep, *(sw.Get_id( )), port )
						sep = ","
					}

					jstr += ` ]`
				}

				jstr += ` }`						// end of this host

				sep = ","
			}
		}
	} else {
		net_sheep.Baa( 0, "ERR: host_list: n is nil (%v) or n.hosts is nil  [TGUNET007]", n == nil )
	}

	jstr += ` ]`			// end of hosts array

	return
}

/*
	Generate a json list of fences
*/
func (n *Network) fence_list( ) ( jstr string ) {
	var(
		sep 	string = ""
	)

	jstr = ` [ `						// an array of objects

	if n != nil && n.limits != nil {
		for _, f := range n.limits {
			jstr += fmt.Sprintf( "%s%s", sep, f.To_json() )
			sep = ", "
		}
	} else {
		net_sheep.Baa( 0, "limit list is nil, no list generated" )
	}

	jstr += ` ]`			// end of the array

	return
}


/*
	Generate a json representation of the network graph.
*/
func (n *Network) to_json( ) ( jstr string ) {
	var	sep string = ""

	jstr = `{ "netele": [ `

	for k := range n.switches {
		jstr += fmt.Sprintf( "%s%s", sep, n.switches[k].To_json( ) )
		sep = ","
	}

	jstr += "] }"

	return
}

/*
	Transfer maps from an old network graph to this one
*/
func (net *Network) xfer_maps( old_net *Network ) {
	net.vm2ip = old_net.vm2ip
	net.ip2vm = old_net.ip2vm
	net.vmid2ip = old_net.vmid2ip
	net.ip2vmid = old_net.ip2vmid
	net.vmid2phost = old_net.vmid2phost	
	net.vmip2gw = old_net.vmip2gw
	net.ip2mac = old_net.ip2mac
	net.mac2phost = old_net.mac2phost
	net.gwmap = old_net.gwmap
	net.fip2ip = old_net.fip2ip
	net.ip2fip = old_net.ip2fip
	net.limits = old_net.limits
}


// --------- public -------------------------------------------------------------------------------------------

/*
	to be executed as a go routine.
	nch is the channel we are expected to listen on for api requests etc.
	sdn_host is the host name and port number where the sdn controller is running.
	(for now we assume the sdn-host is a floodlight host and invoke FL_ to build our graph)
*/
func Network_mgr( nch chan *ipc.Chmsg, sdn_host *string ) {
	var (
		act_net *Network
		req				*ipc.Chmsg
		max_link_cap	int64 = 0
		refresh			int = 30
		find_all_paths	bool = false		// if set, then we will find all paths for a reservation not just shortest
		mlag_paths 		bool = true			// can be set to false in config (mlag_paths); overrides find_all_paths
		link_headroom 	int = 0				// percentage that each link capacity is reduced by
		link_alarm_thresh int = 0			// percentage of total capacity that when reached for a timeslice will trigger an alarm
		limits map[string]*gizmos.Fence		// user link capacity boundaries
		phost_suffix 	*string = nil
		discount 		int64 = 0					// bandwidth discount value (pct if between 1 and 100 inclusive; hard value otherwise
		relaxed			bool = false				// set with relaxed = true in config
		hlist			*string = &empty_str		// host list we'll give to build should we need to build a dummy star topo

	)

	if *sdn_host  == "" {
		sdn_host = cfg_data["default"]["sdn_host"]
		if sdn_host == nil {
			sdn_host = cfg_data["default"]["static_phys_graph"]
			if sdn_host == nil {
				sdn_host = &default_sdn;
				net_sheep.Baa( 1, "WRN: using default openflow host: %s  [TGUNET008]", sdn_host )
			} else {
				net_sheep.Baa( 1, "WRN: using static map of physical network and openstack VM lists to build the network graph  [TGUNET009]" )
			}
		}
	}

	net_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	net_sheep.Set_prefix( "netmgr" )
	tegu_sheep.Add_child( net_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	limits = make( map[string]*gizmos.Fence )
	if cfg_data["fqmgr"] != nil {								// we need to know if fqmgr is adding a suffix to physical host names so we can strip
		if p := cfg_data["fqmgr"]["phost_suffix"]; p != nil {
			phost_suffix = p
			net_sheep.Baa( 1, "will strip suffix from mac2phost map: %s", *phost_suffix )
		}
	}

														// suss out config settings from our section
	if cfg_data["network"] != nil {
		if p := cfg_data["network"]["discount"]; p != nil {
			d := clike.Atoll( *p );
			if d < 0 {
				discount = 0
			} else {
				discount = d
			}
		}

		if p := cfg_data["network"]["relaxed"]; p != nil {
			relaxed = *p ==  "true" || *p ==  "True" || *p == "TRUE"
		}
		if p := cfg_data["network"]["refresh"]; p != nil {
			refresh = clike.Atoi( *p );
		}
		if p := cfg_data["network"]["link_max_cap"]; p != nil {
			max_link_cap = clike.Atoi64( *p )
		}
		if p := cfg_data["network"]["verbose"]; p != nil {
			net_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
		}

		if p := cfg_data["network"]["all_paths"]; p != nil {
			find_all_paths = false
			net_sheep.Baa( 0, "config file key find_all_paths is deprecated: use find_paths = {all|mlag|shortest}" )
		}

		if p := cfg_data["network"]["find_paths"]; p != nil {
			switch( *p ) {
				case "all":
					find_all_paths = true
					mlag_paths = false

				case "mlag":
					find_all_paths = false
					mlag_paths = true

				case "shortest":
					find_all_paths = false
					mlag_paths = false

				default:
					net_sheep.Baa( 0, "WRN: invalid setting in config: network:find_paths %s is not valid; must be: all, mlag, or shortest; assuming mlag  [TGUNET010]" )
					find_all_paths = false
					mlag_paths = true
			}
		}

		if p := cfg_data["network"]["link_headroom"]; p != nil {
			link_headroom = clike.Atoi( *p )							// percentage that we should take all link capacities down by
		}

		if p := cfg_data["network"]["link_alarm"]; p != nil {
			link_alarm_thresh = clike.Atoi( *p )						// percentage of total capacity when an alarm is generated
		}

		if p := cfg_data["network"]["user_link_cap"]; p != nil {
			s := "default"
			f := gizmos.Mk_fence( &s, clike.Atoi64( *p ), 0, 0 )			// the default capacity value used if specific user hasn't been added to the hash
			limits["default"] = f
			v, _ := f.Get_limits()
			net_sheep.Baa( 1, "link capacity limits set to: %d%%", v )
		}
	}

														// enforce some sanity on config file settings
	if refresh < 15 {
		net_sheep.Baa( 0, "refresh rate in config file (%ds) was too small; set to 15s", refresh )
		refresh = 15
	}
	if max_link_cap <= 0 {
		max_link_cap = 1024 * 1024 * 1024 * 10							// if not in config file use 10Gbps
	}

	net_sheep.Baa( 1,  "network_mgr thread started: sdn_hpst=%s max_link_cap=%d refresh=%d", *sdn_host, max_link_cap, refresh )

	act_net = build( nil, sdn_host, max_link_cap, link_headroom, link_alarm_thresh, &empty_str )
	if act_net == nil {
		net_sheep.Baa( 0, "ERR: initial build of network failed -- core dump likely to follow!  [TGUNET011]" )		// this is bad and WILL cause a core dump
	} else {
		net_sheep.Baa( 1, "initial network graph has been built" )
		act_net.limits = limits
		act_net.Set_relaxed( relaxed )
	}

	tklr.Add_spot( 2, nch, REQ_CHOSTLIST, nil, 1 ) 		 						// tickle once, very soon after starting, to get a host list
	tklr.Add_spot( int64( refresh * 2 ), nch, REQ_CHOSTLIST, nil, ipc.FOREVER )  	// get a host list from openstack now and again
	tklr.Add_spot( int64( refresh ), nch, REQ_NETUPDATE, nil, ipc.FOREVER )		// add tickle spot to drive rebuild of network

	for {
		select {					// assume we might have multiple channels in future
			case req = <- nch:
				req.State = nil				// nil state is OK, no error

				net_sheep.Baa( 3, "processing request %d", req.Msg_type )			// we seem to wedge in network, this will be chatty, but may help
				switch req.Msg_type {
					case REQ_NOOP:			// just ignore -- acts like a ping if there is a return channel

					case REQ_STATE:			// return state with respect to whether we have enough data to allow reservation requests
						state := 0			// value reflects ability 2 == have all we need; 1 == have partial, but must block, 0 == have nothing
						mlen := 0
						if act_net.mac2phost != nil  && len( act_net.mac2phost ) > 0 {	// in lazy update world, we need only the agent supplied data
							mlen =  len( act_net.mac2phost )
							state = 2													// once we have it we are golden
						}
						net_sheep.Baa( 1, "net-state: m2pho=%v/%d state=%d", act_net.mac2phost == nil, mlen, state )

						req.Response_data = state

					case REQ_HASCAP:						// verify that there is capacity, and return the path, but don't allocate the path
						p, ok := req.Req_data.( *gizmos.Pledge_bw )
						if ok {
							h1, h2, _, _, commence, expiry, bandw_in, bandw_out := p.Get_values( )
							net_sheep.Baa( 1,  "has-capacity request received on channel  %s -> %s", h1, h2 )
							pcount_in, path_list_out, o_cap_trip := act_net.build_paths( h1, h2, commence, expiry,  bandw_out, find_all_paths, false );
							pcount_out, path_list_in, i_cap_trip := act_net.build_paths( h2, h1, commence, expiry, bandw_in, find_all_paths, true ); 	// reverse path

							if pcount_out > 0  && pcount_in > 0  {
								path_list := make( []*gizmos.Path, pcount_out + pcount_in )		// combine the lists
								pcount := 0
								for j := 0; j < pcount_out; j++ {
									path_list[pcount] = path_list_out[j]
									pcount++
								}
								for j := 0; j < pcount_in; j++ {
									path_list[pcount] = path_list_in[j]
								}

								req.Response_data = path_list
								req.State = nil
							} else {
								req.Response_data = nil
								if i_cap_trip {
									req.State = fmt.Errorf( "unable to generate a path: no capacity (h1<-h2)" )		// tedious, but we'll break out direction
								} else {
									if o_cap_trip {
										req.State = fmt.Errorf( "unable to generate a path: no capacity (h1->h2)" )
									} else {
										req.State = fmt.Errorf( "unable to generate a path:  no path" )
									}
								}
							}
						} else {
							net_sheep.Baa( 1, "internal mishap: pledge passed to has capacity wasn't a bw pledge: %s", p )
							req.State = fmt.Errorf( "unable to create reservation in network, internal data corruption." )
						}

					case REQ_BWOW_RESERVE:								// one way bandwidth reservation, nothing really to vet, return a gate block
						// host names are expected to have been vetted (if needed) and translated to project-id/IPaddr if IDs are enabled
						var ipd *string
						var dh  *gizmos.Host

						req.Response_data = nil
						p, ok := req.Req_data.( *gizmos.Pledge_bwow )
						if ok {
							src, dest := p.Get_hosts( )									// we assume project/host-name

							if src != nil && dest != nil {

								net_sheep.Baa( 1,  "network: bwow reservation request received: %s -> %s", *src, *dest )

								usr := "nobody"											// default dummy user if not project/host
								toks := strings.SplitN( *src, "/", 2 )					// suss out project name
								if len( toks ) > 1 {
									usr = toks[0]										// the 'user' for queue setting
								}

								ips, err := act_net.name2ip( src )
								if err == nil {
									ipd, _ = act_net.name2ip( dest )				// for an external dest, this can be nil which is not an error
								} 
								if ips != nil {
									sh := act_net.hosts[*ips]
									if ipd != nil {
										dh = act_net.hosts[*ipd]						// this will be nil for an external IP
									}
									ssw, _ := sh.Get_switch_port( 0 )
									gate := gizmos.Mk_gate( sh, dh, ssw, p.Get_bandwidth(), usr )
									if (*dest)[0:1] == "!" || dh == nil {			// indicate that dest IP cannot be converted to a MAC address
										gate.Set_extip( dest )
									}

									c, e := p.Get_window( )														// commence/expiry times
									fence := act_net.get_fence( &usr )
									max := int64( -1 )
									if fence != nil {
										max = fence.Get_limit_max()
									}
									if gate.Has_capacity( c, e, p.Get_bandwidth(), &usr, max ) {		// verify that there is room
										qid := p.Get_id()												// for now, the queue id is just the reservation id, so fetch
										p.Set_qid( qid ) 												// and add the queue id to the pledge

										if gate.Add_queue( c, e, p.Get_bandwidth(), qid, fence ) {		// create queue AND inc utilisation on the link
											req.Response_data = gate									// finally safe to set gate as the return data
											req.State = nil												// and nil state to indicate OK
										} else {
											net_sheep.Baa( 1, "owreserve: internal mishap: unable to set queue for gate: %s", gate )
											req.State = fmt.Errorf( "unable to create oneway reservation: unable to setup queue" )
										}
									} else {
										net_sheep.Baa( 1, "owreserve: switch does not have enough capacity for a oneway reservation of %s", p.Get_bandwidth() )
										req.State = fmt.Errorf( "unable to create oneway reservation for %d: no capacity on (v)switch: %s", p.Get_bandwidth(), gate.Get_sw_name() )
									}
								} else {
									net_sheep.Baa( 1, "cant map %s to ip: %s", src )
									req.State = fmt.Errorf( "unable to create oneway reservation in network cannot map src (%s) to an IP address", src )
								}
							} else {
								net_sheep.Baa( 1, "owreserve: one/both host names were invalid" )
								req.State = fmt.Errorf( "unable to create oneway reservation in network one or both host names invalid" )
							}
						} else {									// pledge wasn't a bw pledge
							net_sheep.Baa( 1, "internal mishap: pledge passed to owreserve wasn't a bwow pledge: %s", p )
							req.State = fmt.Errorf( "unable to create oneway reservation in network, internal data corruption." )
						}

					case REQ_BW_RESERVE:
						var ip2		*string = nil					// tmp pointer for this block

						// host names are expected to have been vetted (if needed) and translated to project-id/name if IDs are enabled
						p, ok := req.Req_data.( *gizmos.Pledge_bw )
						if ok {
							h1, h2, _, _, commence, expiry, bandw_in, bandw_out := p.Get_values( )		// ports can be ignored
							net_sheep.Baa( 1,  "network: bw reservation request received: %s -> %s  from %d to %d", *h1, *h2, commence, expiry )

							suffix := "bps"
							if discount > 0 {
								if discount < 101 {
									bandw_in -=  ((bandw_in * discount)/100)
									bandw_out -=  ((bandw_out * discount)/100)
									suffix = "%"
								} else {
									bandw_in -= discount
									bandw_out -= discount
								}

								if bandw_out < 10 {			// add some sanity, and keep it from going too low
									bandw_out = 10
								}
								if bandw_in < 10 {
									bandw_in = 10
								}
								net_sheep.Baa( 1, "bandwidth was reduced by a discount of %d%s: in=%d out=%d", discount, suffix, bandw_in, bandw_out )
							}

							ip1, err := act_net.name2ip( h1 )
							if err == nil {
								ip2, err = act_net.name2ip( h2 )
							}

							if err == nil {
								net_sheep.Baa( 2,  "network: attempt to find path between  %s -> %s", *ip1, *ip2 )
								pcount_out, path_list_out, o_cap_trip := act_net.build_paths( ip1, ip2, commence, expiry, bandw_out, find_all_paths, false ); 	// outbound path
								pcount_in, path_list_in, i_cap_trip := act_net.build_paths( ip2, ip1, commence, expiry, bandw_in, find_all_paths, true ); 		// inbound path

								if pcount_out > 0  &&  pcount_in > 0  {
									net_sheep.Baa( 1,  "network: %d acceptable path(s) found icap=%v ocap=%v", pcount_out + pcount_in, i_cap_trip, o_cap_trip )

									path_list := make( []*gizmos.Path, pcount_out + pcount_in )		// combine the lists
									pcount := 0
									for j := 0; j < pcount_out; j++ {
										path_list[pcount] = path_list_out[j]
										pcount++
									}
									for j := 0; j < pcount_in; j++ {	
										path_list[pcount] = path_list_in[j]
										pcount++
									}

									qid := p.Get_id()											// for now, the queue id is just the reservation id, so fetch
									p.Set_qid( qid )											// and add the queue id to the pledge

									for i := 0; i < pcount; i++ {								// set the queues for each path in the list (multiple paths if network is disjoint)
										fence := act_net.get_fence( path_list[i].Get_usr() )
										net_sheep.Baa( 2,  "\tpath_list[%d]: %s -> %s  (%s)", i, *h1, *h2, path_list[i].To_str( ) )
										path_list[i].Set_queue( qid, commence, expiry, path_list[i].Get_bandwidth(), fence )		// create queue AND inc utilisation on the link
										if mlag_paths {
											net_sheep.Baa( 1, "increasing usage for mlag members" )
											path_list[i].Inc_mlag( commence, expiry, path_list[i].Get_bandwidth(), fence, act_net.mlags )
										}
									}

									req.Response_data = path_list
									req.State = nil
								} else {
									req.Response_data = nil
									if i_cap_trip {
										req.State = fmt.Errorf( "unable to generate a path: no capacity (h1<-h2)" )		// tedious, but we'll break out direction
									} else {
										if o_cap_trip {
											req.State = fmt.Errorf( "unable to generate a path: no capacity (h1->h2)" )
										} else {
											req.State = fmt.Errorf( "unable to generate a path:  no path" )
										}
									}
									net_sheep.Baa( 0,  "no paths in list: %s  cap=%v/%v", req.State, i_cap_trip, o_cap_trip )
								}
							} else {
								net_sheep.Baa( 0,  "network: unable to map to an IP address: %s",  err )
								req.State = fmt.Errorf( "unable to map host name to a known IP address: %s", err )
							}
						} else {									// pledge wasn't a bw pledge
							net_sheep.Baa( 1, "internal mishap: pledge passed to reserve wasn't a bw pledge: %s", p )
							req.State = fmt.Errorf( "unable to create reservation in network, internal data corruption." )
						}

					case REQ_PT_RESERVE:						// passthru reservations are allowed only in relaxed mode and only if user has link capacity set
						req.Response_data = false				// assume bad
						if req.Req_data != nil {
							if act_net.Is_relaxed() {
								u := req.Req_data.( *string )
								if act_net.get_fence_max( u ) > 0 {
									req.Response_data = true
								} else {
									req.State = fmt.Errorf( "user link capacity <= 0" )
								}
							} else {
								req.State = fmt.Errorf( "passthru reservations not allowed in full path mode (relaxed is false)" )
							}
			
						} else {
							req.State = fmt.Errorf( "no data passed on request channel" )
						}
					
					case REQ_DEL:									// delete the utilisation for the given reservation
						switch p := req.Req_data.( type ) {
							case *gizmos.Pledge_bw:
								net_sheep.Baa( 1,  "network: deleting bandwidth reservation: %s", *p.Get_id() )
								commence, expiry := p.Get_window( )
								path_list := p.Get_path_list( )
		
								qid := p.Get_qid()							// get the queue ID associated with the pledge
								for i := range path_list {
									fence := act_net.get_fence( path_list[i].Get_usr() )
									net_sheep.Baa( 1,  "network: deleting path %d associated with usr=%s", i, *fence.Name )
									path_list[i].Set_queue( qid, commence, expiry, -path_list[i].Get_bandwidth(), fence )		// reduce queues on the path as needed
								}

							case *gizmos.Pledge_bwow:
								net_sheep.Baa( 1,  "network: deleting oneway reservation: %s", *p.Get_id() )
								commence, expiry := p.Get_window( )
								gate := p.Get_gate()
								fence := act_net.get_fence( gate.Get_usr() )
								gate.Set_queue( p.Get_qid(), commence, expiry, -p.Get_bandwidth(), fence )				// reduce queues

							default:
								net_sheep.Baa( 1, "internal mishap: req_del wasn't passed a bandwidth or oneway pledge; nothing done by network" )
							
						}

					case REQ_ADD:							// insert new information into the various vm maps
						if req.Req_data != nil {
							switch req.Req_data.( type ) {
								case *Net_vm:
									vm := req.Req_data.( *Net_vm )
									act_net.insert_vm( vm )

								case []*Net_vm:
									vlist := req.Req_data.( []*Net_vm )
									for i := range vlist {
										act_net.insert_vm( vlist[i] )
									}
							}

							new_net := build( act_net, sdn_host, max_link_cap, link_headroom, link_alarm_thresh, hlist )
							if new_net != nil {
								new_net.xfer_maps( act_net )				// copy maps from old net to the new graph
								act_net = new_net							// and finally use it
							}
						}
						
					case REQ_VM2IP:								// a new vm name/vm ID to ip address map
						if req.Req_data != nil {
							act_net.vm2ip = req.Req_data.( map[string]*string )
							act_net.ip2vm = act_net.build_ip2vm( )
							net_sheep.Baa( 2, "vm2ip and ip2vm maps were updated, has %d entries", len( act_net.vm2ip ) )
						} else {
							net_sheep.Baa( 1, "vm2ip map was nil; not changed" )
						}


					case REQ_VMID2IP:									// Tegu-lite
						if req.Req_data != nil {
							act_net.vmid2ip = req.Req_data.( map[string]*string )
						} else {
							net_sheep.Baa( 1, "vmid2ip map was nil; not changed" )
						}

					case REQ_IP2VMID:									// Tegu-lite
						if req.Req_data != nil {
							act_net.ip2vmid = req.Req_data.( map[string]*string )
						} else {
							net_sheep.Baa( 1, "ip2vmid map was nil; not changed" )
						}

					case REQ_VMID2PHOST:									// Tegu-lite
						if req.Req_data != nil {
							act_net.vmid2phost = req.Req_data.( map[string]*string )
						} else {
							net_sheep.Baa( 1, "vmid2phost map was nil; not changed" )
						}

					case REQ_IP2MAC:									// Tegu-lite
						if req.Req_data != nil {
							act_net.ip2mac = req.Req_data.( map[string]*string )
							if net_sheep.Would_baa( 3 ) {
								for k, v := range act_net.ip2mac {
									net_sheep.Baa( 3, "ip2mac: %s --> %s", k, *v )
								}
							}
						} else {
							net_sheep.Baa( 1, "ip2mac map was nil; not changed" )
						}

					case REQ_GWMAP:									// Tegu-lite
						if req.Req_data != nil {
							act_net.gwmap = req.Req_data.( map[string]*string )
							if net_sheep.Would_baa( 3 ) {
								for k, v := range act_net.gwmap {
									net_sheep.Baa( 3, "gwmap: %s --> %s", k, *v )
								}
							}
						} else {
							net_sheep.Baa( 1, "gw map was nil; not changed" )
						}

					case REQ_IP2FIP:									// Tegu-lite
						if req.Req_data != nil {
							act_net.ip2fip = req.Req_data.( map[string]*string )
						} else {
							net_sheep.Baa( 1, "ip2fip map was nil; not changed" )
						}

					case REQ_FIP2IP:
						if req.Req_data != nil {
							act_net.fip2ip = req.Req_data.( map[string]*string )
							if net_sheep.Would_baa( 3 ) {
								for k, v := range act_net.fip2ip {
									net_sheep.Baa( 3, "fip2ip: %s --> %s", k, *v )
								}
							}
						} else {
							net_sheep.Baa( 1, "fip2ip map was nil; not changed" )
						}

					case REQ_GEN_QMAP:							// generate a new queue setting map
						ts := req.Req_data.( int64 )			// time stamp for generation
						req.Response_data, req.State = act_net.gen_queue_map( ts, false )

					case REQ_GEN_EPQMAP:						// generate a new queue setting map but only for endpoints
						ts := req.Req_data.( int64 )			// time stamp for generation
						req.Response_data, req.State = act_net.gen_queue_map( ts, true )
						
					case REQ_GETGW:								// given a project ID (projects ID) map it to the gateway
						if req.Req_data != nil {
							tname := req.Req_data.( *string )
							req.Response_data = act_net.gateway4tid( *tname )
						} else {
							req.Response_data = nil
						}

					case REQ_GETPHOST:							// given a name or IP address, return the physical host
						if req.Req_data != nil {
							var ip *string

							s := req.Req_data.( *string )
							ip, req.State = act_net.name2ip( s )
							if req.State == nil {
								req.Response_data = act_net.mac2phost[*act_net.ip2mac[*ip]]
								if req.Response_data == nil {
									req.State = fmt.Errorf( "cannot translate IP to physical host: %s", *ip )
								}	
							}
						} else {
							req.State = fmt.Errorf( "no data passed on request channel" )
						}
						
					case REQ_GETIP:								// given a VM name or ID return the IP if we know it.
						if req.Req_data != nil {
							s := req.Req_data.( *string )
							req.Response_data, req.State = act_net.name2ip( s )		// returns ip or nil
						} else {
							req.State = fmt.Errorf( "no data passed on request channel" )
						}
					case REQ_HOSTINFO:							// generate a string with mac, ip, switch-id and switch port for the given host
						if req.Req_data != nil {
							ip, mac, swid, port, err := act_net.host_info(  req.Req_data.( *string ) )
							if err != nil {
								req.State = err
								req.Response_data = nil
							} else {
								req.Response_data = fmt.Sprintf( "%s,%s,%s,%d", *ip, *mac, *swid, port )
							}
						} else {
							req.State = fmt.Errorf( "no data passed on request channel" )
						}

					case REQ_GETLMAX:							// DEPRECATED!  request for the max link allocation
						req.Response_data = nil;
						req.State = nil;

					case REQ_NETUPDATE:											// build a new network graph
						net_sheep.Baa( 2, "rebuilding network graph" )			// less chatty with lazy changes
						new_net := build( act_net, sdn_host, max_link_cap, link_headroom, link_alarm_thresh, hlist )
						if new_net != nil {
							new_net.xfer_maps( act_net )						// copy maps from old net to the new graph
							act_net = new_net

							net_sheep.Baa( 2, "network graph rebuild completed" )		// timing during debugging
						} else {
							net_sheep.Baa( 1, "unable to update network graph -- SDNC down?" )
						}


					case REQ_CHOSTLIST:								// this is tricky as it comes from tickler as a request, and from osifmgr as a response, be careful!
																	// this is similar, yet different, than the code in fq_mgr (we don't need phost suffix here)
						req.Response_ch = nil;						// regardless of source, we should not reply to this request

						if req.State != nil || req.Response_data != nil {				// response from ostack if with list or error
							if  req.Response_data.( *string ) != nil {
								hls := strings.TrimLeft( *(req.Response_data.( *string )), " \t" )		// ditch leading whitespace
								hl := &hls
								if *hl != ""  {
									hlist = hl										// ok to use it
									net_sheep.Baa( 2, "host list received from osif: %s", *hlist )
								} else {
									net_sheep.Baa( 1, "empty host list received from osif was discarded" )
								}
							} else {
								net_sheep.Baa( 0, "WRN: no  data from openstack; expected host list string  [TGUFQM009]" )
							}
						} else {
							req_hosts( nch, net_sheep )					// send requests to osif for data
						}
								
					//	------------------ user api things ---------------------------------------------------------
					case REQ_SETULCAP:							// user link capacity; expect array of two string pointers
						data := req.Req_data.( []*string )
						val := clike.Atoi64( *data[1] )	
						if val < 0 {							// drop the user fence
							delete( act_net.limits, *data[0] )
							net_sheep.Baa( 1, "user link capacity deleted: %s", *data[0] )
						} else {
							f := gizmos.Mk_fence( data[0], val, 0, 0 )			// get the default frame
							act_net.limits[*data[0]] = f
							net_sheep.Baa( 1, "user link capacity set: %s now %d%%", *data[0], f.Get_limit_max() )
						}
						
					case REQ_NETGRAPH:							// dump the current network graph
						req.Response_data = act_net.to_json()

					case REQ_LISTHOSTS:							// spew out a json list of hosts with name, ip, switch id and port
						req.Response_data = act_net.host_list( )

					case REQ_LISTULCAP:							// user link capacity list
						req.Response_data = act_net.fence_list( )

					case REQ_LISTCONNS:							// for a given host spit out the switch(es) and port(s)
						hname := req.Req_data.( *string )
						host := act_net.hosts[*hname]
						if host != nil {
							req.Response_data = host.Ports2json( );
						} else {
							req.Response_data = nil			// assume failure
							req.State = fmt.Errorf( "did not find host: %s", *hname )

							net_sheep.Baa( 2, "looking up name for listconns: %s", *hname )
							hname = act_net.vm2ip[*hname]		// maybe they sent a vm ID or name
							if hname == nil || *hname == "" {
								net_sheep.Baa( 2, "unable to find name in vm2ip table" )

								if net_sheep.Would_baa( 3 ) {
									for k, v := range act_net.vm2ip {
										net_sheep.Baa( 3, "vm2ip[%s] = %s", k, *v );
									}
								}
							} else {
								net_sheep.Baa( 2, "name found in vm2ip table translated to: %s, looking up  host", *hname )
								host = act_net.hosts[*hname]
								if host != nil {
									req.Response_data = host.Ports2json( );
									req.State = nil
								} else {
									net_sheep.Baa( 2, "unable to find host entry for: %s", *hname )
								}
							}
						}

					case REQ_GET_PHOST_FROM_MAC:			// try to map a MAC to a phost -- used for mirroring
						mac := req.Req_data.( *string )
						for k, v := range act_net.mac2phost {
							if *mac == k {
								req.Response_data = v
							}
						}

					case REQ_SETDISC:
						req.State = nil;	
						req.Response_data = "";			// we shouldn't send anything back, but if caller gave a channel, be successful
						if req.Req_data != nil {
							d := clike.Atoll( *(req.Req_data.( *string )) )
							if d < 0 {
								discount = 0
							} else {
								discount = d
							}
						}

					// --------------------- agent things -------------------------------------------------------------
					case REQ_MAC2PHOST:
						req.Response_ch = nil			// we don't respond to these
						act_net.update_mac2phost( req.Req_data.( []string ), phost_suffix )

					default:
						net_sheep.Baa( 1,  "unknown request received on channel: %d", req.Msg_type )
				}

				net_sheep.Baa( 3, "processing request complete %d", req.Msg_type )
				if req.Response_ch != nil {				// if response needed; send the request (updated) back
					req.Response_ch <- req
				}

		}
	}
}
