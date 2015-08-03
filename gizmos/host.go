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

	Mnemonic:	host
	Abstract:	manages a host in the network
	Date:		25 November 2013
	Author:		E. Scott Daniels

	Note:		If the network is split (not all switches are being controlled by
				floodlight, then a host might show multiple connections: one on the
				switch that it is truly connectted to, and one for each switch that
				has an 'entry point' (likely a link to an uncontrolled switch) for
				the host.  At the moment, it does not appear that it is possible to
				map the IP address to the switch/port as the list of IPs and the list
				of attachment points seem not to be ordered.

	Mod:		29 Jun 2014 - Changes to support user link limits.
				26 Mar 2015 - Added Get_address() function to return one address with
					favourtism if host has both addresses defined.
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
	defines a host
*/
type Host struct {
	vmid	*string			// id given to host by virtulation manager (e.g. ostack)
	mac		string
	ip4		string
	ip6		string
	conns	[]*Switch		// the switches that it connects to (see note)
	ports	[]int			// ports match with Switch entries
	cidx	int
}

/*
	Create the object setting defaults and adding user supplied IP address strings.
*/
func Mk_host( mac string, ip4 string, ip6 string ) (h *Host) {

	h = &Host {
		mac:	mac,
		ip4:	ip4,
		ip6:	ip6,
		cidx:	0,
	}

	h.conns = make( []*Switch, 5 )
	h.ports = make( []int, 5 )

	return
}

/*
	Destruction
*/
func ( h *Host ) Nuke() {

	if h == nil {
		return
	}

	h.conns = nil
	h.ports = nil
}

/*
	Adds the vmid to the host (usually not known at mk time, so it's not a part of the mk process.
*/
func (h *Host) Add_vmid( vmid *string ) {
	h.vmid = vmid
}

/*
	allows more switches to be added
*/
func (h *Host) Add_switch( sw *Switch, port int ) {
	var (
		new_conns	[]*Switch
		new_ports	[]int
	)

	if h == nil {
		return
	}

	if h.cidx >= len( h.conns ) {						// out of room, extend and copy to new
		new_conns = make( []*Switch, h.cidx + 10 )
		new_ports = make( []int, h.cidx + 10 )
		for i := 0; i < h.cidx; i++ {
			new_conns[i] = h.conns[i]
			new_ports[i] = h.ports[i]
		}	
		h.conns = new_conns
		h.ports = new_ports
	}

	h.conns[h.cidx] = sw;
	h.ports[h.cidx] = port
	h.cidx++
}

/*
	Return the ith switch and associated port from the connections list
	Allows an owner of the object to iterate over the switches without
	having to have direct access to the lists.
*/
func (h *Host) Get_switch_port( i int ) ( s *Switch, p int ) {
	s = nil
	p = -1

	if h != nil  &&  i < len( h.conns ) {
		s = h.conns[i]
		p = h.ports[i]
	}

	return
}

/*
	Returns the port the host is 'attached to' for the given switch.
	In a disjoint network attached might not be true, but it's the
	port that the switch should write traffic on destined for the host.
*/
func (h *Host) Get_port( s *Switch ) ( int ) {
	var p int

	if h == nil {
		return -1
	}

	for p = 0; p < h.cidx; p++ {
		if h.conns[p] == s {
			return h.ports[p]
		}
	}

	p = -1
	return p
}

/*
	Drives the callback function for each switch/port combination that we have in our list.
	Data is the user data that is passed in that the callback function may need to process.
func (h *Host) Iterate_switch_port( data interface{}, cb func( *Switch, int, interface{} ) )  {
	for i := 0; i < h.cidx; i++ {
		cb( h.switch, h.port, data )
	}
}
*/

/*
	Return both IP address strings or nil
*/
func ( h *Host ) Get_addresses( ) ( ip4 *string, ip6 *string ) {
	if h == nil {
		return nil, nil
	}

	ip4 = &h.ip4
	ip6 = &h.ip6
	return
}

/*
	Return one of the IP addresses associated with the host. If both are defined the IPv6 addr
	is returned in favour of the IP v4 address if pref_v6 is true.
*/
func( h *Host ) Get_address( pref_v6 bool ) ( *string ) {
	if h == nil {
		return nil
	}

	if (h.ip6 != "" && pref_v6) || h.ip4 == "" {
		return &h.ip6
	}
	
	return &h.ip4
}

/*
	Return the number of connections.
*/
func ( h *Host ) Get_nconns( ) ( int ) {
	if h == nil {
		return 0
	}

	return h.cidx
}

/*
	Return a pointer to the string that has the mac address.
*/
func (h *Host) Get_mac( ) (s *string) {
	if h == nil {
		return nil
	}

	return &h.mac
}

/*
	Generate a string of the basic info
	Deprecated in favour of stringer interface method.
*/
func (h *Host) To_str( ) ( s string ) {
	return h.String()
}

/*
	Generate a string of the basic info
*/
func (h *Host) String( ) ( s string ) {
	if h == nil {
		return "--nil--"
	}

	s = fmt.Sprintf( "{ host: %s ",  h.mac )
	if h.ip4 != "" {
		s += fmt.Sprintf( "ip4: %s ",  h.ip4 )
	}
	if h.ip6 != "" {
		s += fmt.Sprintf( "ip6: %s ",  h.ip6 )
	}

	if h.cidx > 0 {
		s += fmt.Sprintf( " connections [ " )
		for i := 0; i < h.cidx; i++ {
			if h.conns[i] != nil {
				id := h.conns[i].Get_id()
				if id != nil {
					s += fmt.Sprintf( "%s ", *id )
				}
			} else {
				s += "==nil-connection== "
			}
		}
		s += "]"
	}

	return
}

/*
	Jsonise the whole object.
*/
func (h *Host) To_json( ) ( s string ) {
	var (
		sep string = ""
	)

	if h == nil {
		s = `{ "mac": "null-host" } `
		return
	}

	if h.vmid != nil {
		s = fmt.Sprintf( `{ "vmid": %q, "mac": %q`, *h.vmid, h.mac )
	} else {
		s = fmt.Sprintf( `{ "vmid": "missing", "mac": %q`, h.mac )
	}

	if h.ip4 != "" {
		s += fmt.Sprintf( `, "ip4": %q`,  h.ip4 )
	}
	if h.ip6 != "" {
		s += fmt.Sprintf( `, "ip6": %q`,  h.ip6 )
	}

	if h.cidx > 0 {
		s += fmt.Sprintf( `, "connections": [ ` )
		for i := 0; i < h.cidx; i++ {
			s += fmt.Sprintf( `%s%q`, sep, *(h.conns[i].Get_id()) )
			sep = ","
		}
		s += "] "
	}

	if h.cidx > 0 {
		sep = ""
		s += fmt.Sprintf( `, "ports": [ ` )
		for i := 0; i < h.cidx; i++ {
			s += fmt.Sprintf( "%s%d", sep, h.ports[i] )
			sep = ","
		}
		s += "] "
	}

	s += "}"

	return
}

/*
	generate json output that describes each swtitch/port combination that this host has.
*/
func (h *Host) Ports2json( ) ( s string ) {
	var (
		sep string = ""
	)
	if h == nil {
		return `{ "mac": "null-host" }`
	}

	s = fmt.Sprintf( `{ "host": { "ip4": %q, "mac": %q, "conns": [`, h.ip4, h.mac )
	for i := 0; i < h.cidx; i++ {
		if h.conns[i] != nil {
			sname := h.conns[i].Get_id()
			s += fmt.Sprintf( `%s { "switch": %q, "port": %d }`, sep, *sname, h.ports[i] )
			sep = ",";
		}
	}

	s += fmt.Sprintf( `] } }` )
	return
}
