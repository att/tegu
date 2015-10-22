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

	Mnemonic:	endpoint
	Abstract:	manages an endpoint in the network
				This is a revamp of the host (deprecated) to be more generic.

	Date:		10 September 2015
	Author:		E. Scott Daniels

	Mod:
*/

package gizmos

import (
	"fmt"
)


// --------------------------------------------------------------------------------------

/*
	Defines an endpoint in the network.  An endpoint is attached to a switch (conn_pt).
	The struct is really just a few things with the metadata maintained as a map so 
	that we can use more generic metadata keys down the road if we need them.  There 
	are some 'hard coded' things like uuid, project, phost, etc. which have explicit 
	get/set functions.
*/
type Endpt struct {
	meta		map[string]string	// generic metadata
	ip_addrs	[]*string			// addresses associated with this endpoint
	router		bool				// true if this is a router (gateway in openstack lingo)
	conn_pt		*Switch				// the switch that it is connected to
	port		int					// the port on the switch if known
}

/*
	Create the object setting defaults and adding user supplied IP address strings.
	The ip string is a space separated list of ip addresses or an array of string.
*/
func Mk_endpt( uuid string, phost string, project string, ip interface{}, mac string, sw *Switch, port int ) (ep *Endpt) {

	ep = &Endpt {
		conn_pt: 	sw,
		port:		port,
		router:		false,
	}

	ep.ip_addrs = make( []*string, 0, 10 )		// initially room for 10; add function extends if needed
	switch ipa := ip.( type ) {
		case string:
			ep.Add_addr( ipa )

		case *string:
			ep.Add_addr( *ipa )

		case []string:
			for _, v := range( ipa ) {
				ep.Add_addr( v )
			}

		case []*string:
			for _, v := range( ipa ) {
				ep.Add_addr( *v )
			}
	}

	ep.meta = make( map[string]string )
	ep.meta["uuid" ] = uuid
	ep.meta["phost" ]  = phost
	ep.meta["project" ]  = project
	ep.meta["mac" ]  = mac

	return
}

/*
	Destruction.
*/
func ( ep *Endpt ) Nuke() {

	if ep == nil {
		return
	}
}

/*
	Adds an address to the list, growing the slice if needed.
*/
func ( ep *Endpt ) Add_addr( ip string ) {
	/*
	n := len( ep.ip_addrs )
	if n == cap( ep.ip_addrs ) {
		ns := make( []*string, n, cap( ep.ip_addrs ) * 2 )
		copy( ns[:], ep.ip_addrs )
		ep.ip_addrs = ns
	}

	ep.ip_addrs = ep.ip_addrs[0:n+1]
	ep.ip_addrs[n] = &ip
	*/
	ep.ip_addrs = append( ep.ip_addrs, &ip )
}

/*
	Given a test value (string, pointer to string, int or int64), 
	and a meta key, return true if the value in the
	hash  for the key matches string. If there is no entry false is 
	returned.
*/
func ( ep *Endpt ) Equals_meta( key string, test interface{} ) ( bool ) {
	if v := ep.meta[key]; v != "" {
		switch t := test.(type) {
			case string: 	return v == t
			case *string:	return v == *t
			case int:		return v == string( t )
			case int64:		return v == string( t )
		}
	}

	return false
}

/*
	Return any meta value.
*/
func ( ep *Endpt ) Get_meta_value( key string ) ( *string ) {
	if ep == nil {
		return nil
	}

	s := ep.meta[key]
	return &s
}

/*
	Returns a copy of the meta map; adds port and router as 'strings'.
*/
func ( ep *Endpt ) Get_meta_copy( ) ( map[string]string ) {
	if ep == nil {
		return nil
	}

	meta_cpy := make( map[string]string )
	for k, v := range ep.meta {
		meta_cpy[k] = v
	}

	meta_cpy["port"] = string( ep.port )
	meta_cpy["router"] = fmt.Sprintf( "%v", ep.router )

	return meta_cpy
}

/*
	Returns the associated network ID.
*/
func ( ep *Endpt ) Get_netid( ) ( *string ) {
	if ep == nil {
		return nil
	}

	s := ep.meta["netid"]
	return &s
}

/*
	Return true if this endpoint has a connection point already defined.
*/
func ( ep *Endpt ) Has_connpt() ( bool ) {
	if ep == nil {
		return false
	}

	return ep.conn_pt != nil
}

/*
	Return true if this endpoint seems to be a router.
*/
func ( ep *Endpt ) Is_router() ( bool ) {
	if ep == nil {
		return false
	}

	return ep.router
}

/*
	Remove a specific address from the list, closing the hole.
*/
func ( ep *Endpt ) Rm_addr( ip string ) {
	if ep == nil {
		return
	}

	for n, v := range ep.ip_addrs {			// search for the string passed in
		if *v == ip {
			ns := make( []*string, len( ep.ip_addrs ) -1, cap( ep.ip_addrs ) )
			copy( ns, ep.ip_addrs[:n] )				// copy first n elements
			copy( ns[n:], ep.ip_addrs[n+1:] )		// copy starting past the match
			ep.ip_addrs = ns
			return
		}
	}
}

/*
	Reset the ip addresses to just the ip address passed in.
*/
func ( ep *Endpt ) Reset_addrs( ip string ) {
	
	ep.ip_addrs = make( []*string, 1, 10 )
	ep.ip_addrs[0] = &ip
}

/*
	Set any meta value.
*/
func ( ep *Endpt ) Set_meta_value( key string, val string ) {
	if ep == nil {
		return
	}

	ep.meta[key] = val
}

/*
	Allows it to be changed later; sometimes it's not known at creation time.
*/
func (ep *Endpt) Set_uuid( uuid *string ) {
	if ep == nil {
		return
	}

	ep.meta["uuid"] = *uuid
}

/*
	Changes the switch that this ep is attached to. Port must be either the 
	special -128 port for late binding, or an integer > 0.
*/
func (ep *Endpt) Set_switch( sw *Switch, port int ) ( error ) {
	if ep == nil {
		return fmt.Errorf( "pointer to struct was nil" )
	}

	if port > 0 || port == -128 {
		ep.conn_pt = sw
		ep.port = port
	} else {
		return fmt.Errorf( "invalid port value: %d", port )
	}

	return nil
}

/*
	Allows router flag to be set.
*/
func (ep *Endpt) Set_router( val bool ) {
	if ep == nil {
		return
	}
	ep.router = val
}

/*
	Allows the port to be set
*/
func (ep *Endpt) Set_port( val int ) {
	if ep == nil {
		return
	}

	ep.port = val
}

/*
	Return the project id.
*/
func ( ep *Endpt) Get_project( ) ( *string ) {
	if ep == nil {
		return nil
	}

	s := ep.meta["project"]
	return &s
}

/*
	Return the swtich/port the endpoint is attached to. On error, returned port
	will be -1.
*/
func (ep *Endpt) Get_switch_port( ) ( s *Switch, p int ) {
	s = nil
	p = -128

	if ep != nil {
		s = ep.conn_pt
		p = ep.port
	}

	return
}

/*
	Returns the physical host name.
*/
func ( ep *Endpt ) Get_phost() ( *string ) {
	if ep == nil {
		return nil
	}

	s := ep.meta["phost"]
	return &s
}

/*
	Returns the IP and the MAC address. The IP address really cannot be trusted
	as it is the first from the list and if the endpoint has multiple IP addresses
	this could change from execution to execution of Tegu, or even if the endpoint
	data is updated while running.
*/
func ( ep *Endpt ) Get_addresses( ) ( ip *string, mac *string ) {
	if ep == nil {
		return nil, nil
	}

	ips := ""
	if len( ep.ip_addrs ) > 0 {
		ips = *ep.ip_addrs[0]
	}

	macs := ep.meta["mac"]
	return &ips, &macs
}
/*
	Returns the mac.
*/
func ( ep *Endpt ) Get_mac( ) ( mac *string ) {
	if ep == nil {
		return nil
	}

	macs := ep.meta["mac"]				// force copy
	return &macs
}

/*
	Generate a string of the basic info.
*/
func (ep *Endpt) String( ) ( s string ) {
	if ep == nil {
		return "--nil--"
	}

	s = ""
	sep := ""
	for k, v := range ep.meta {
		s += fmt.Sprintf( "%s%s=%s", sep, k, v )
		sep = " "
	}

	s += " ip=[ "
	sep = ""
	for _, v := range ep.ip_addrs {
		s += fmt.Sprintf( "%s%s", sep, *v )
		sep = ", "
	}
	s += " ]"

	if ep.conn_pt != nil {
		s += fmt.Sprintf( " sw=%s port=%d",  *(ep.conn_pt.Get_id()), ep.port )
	} else {
		s += " sw=none"
	}

	s += fmt.Sprintf( " rtr=%v", ep.router )

	return
}

/*
	Jsonise the endpt struct.
*/
func (ep *Endpt) To_json( ) ( s string ) {
	if ep == nil {
		return "--nil--"
	}

	s = "{ "
	sep := ""
	for k, v := range ep.meta {
		s += fmt.Sprintf( `%s"%s": %q`, sep, k, v )
		sep = " "
	}

	s += " ip=[ "
	sep = ""
	for _, v := range ep.ip_addrs {
		s += fmt.Sprintf( `%s%q`, sep, *v )
		sep = ", "
	}
	s += " ], "

	if ep.conn_pt != nil {
		s += fmt.Sprintf( ` "sw"=%q, "port"=%d`,  *(ep.conn_pt.Get_id()), ep.port )
	} else {
		s += fmt.Sprintf( ` "sw"=%q, "port"=%d`,  "", -1 )
	}

	s += fmt.Sprintf( ` "rtr": %v`, ep.router )
	s += " }"

	return
}
