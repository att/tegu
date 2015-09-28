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
*/
type Endpt struct {
	uuid	string			// id given to host by creator (openstack, etc.)
	project	string			// a project id if required by virtualiasation manager
	phost	string			// the physical host this endpoint resides on
	ip		string			// this either an IPv6 OR an IPv4 address
	mac		string			// mac address
	router	bool			// true if this is a router (gateway in openstack lingo)
	conn_pt	*Switch			// the switch that it is connected to
	port	int				// the port on the switch if known
}

/*
	Create the object setting defaults and adding user supplied IP address strings.
*/
func Mk_endpt( uuid string, phost string, project string, ip string, mac string, sw *Switch, port int ) (ep *Endpt) {

	ep = &Endpt {
		uuid: uuid,
		phost: phost,
		project: project,
		ip:		ip,
		mac:	mac,
		conn_pt: 	sw,
		port:	port,
		router:	false,
	}

	return
}

/*
	Given a map of openstack endpoints, generate a map of our flavour of endpoint.
	Connection point and port are set by network when it knows the swtitch object to point to.
func Epmap_from_ostack( omap map[string]*ostack.End_pt ) (epmap map[string]*Endpt) {
	
	if( omap == nil ) {
		return nil
	}

	epmap = make( map[string]*Endpt, len( omap ) )
	for k, v := range omap {

		epmap[k] = &Endpt {
			uuid:	k,
			phost:	*(v.Get_phost()),
			project:	*(v.Get_project()),
			ip:	*(v.Get_ip()),
			mac:	*(v.Get_mac()),
			router:	v.Is_router(),
		}
	}
		
	return
}
*/

/*
	Destruction.
*/
func ( ep *Endpt ) Nuke() {

	if ep == nil {
		return
	}
}

/*
	Allows it to be changed later; sometimes it's not known at creation time.
*/
func (ep *Endpt) Set_uuid( uuid *string ) {
	ep.uuid = *uuid
}

/*
	Changes the switch that this ep is attached to. Port must be either the 
	special -128 port for late binding, or an integer > 0.
*/
func (ep *Endpt) Set_switch( sw *Switch, port int ) ( error ) {
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
	ep.router = val
}

/*
	Allows the port to be set
*/
func (ep *Endpt) Set_port( val int ) {
	ep.port = val
}

/*
	Return the swwtich/port the endpoint is attached to. On error, returned port
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

	return &ep.phost
}

/*
	Returns the MAC and IP addresses
*/
func ( ep *Endpt ) Get_addresses( ) ( ip *string, mac *string ) {
	if ep == nil {
		return nil, nil
	}

	return &ep.ip, &ep.mac
}

/*
	Generate a string of the basic info.
*/
func (ep *Endpt) String( ) ( s string ) {
	if ep == nil {
		return "--nil--"
	}

	s = fmt.Sprintf( "id=%s ",  ep.uuid )
	if ep.ip != "" {
		s += fmt.Sprintf( "ip=%s mac=%s",  ep.ip, ep.mac )
	}
	if ep.conn_pt != nil {
		s += fmt.Sprintf( "sw=%s port=%d",  ep.conn_pt.Get_id(), ep.port )
	} else {
		s += fmt.Sprintf( "sw=none " )
	}

	s += fmt.Sprintf( " rtr=%v", ep.router )

	return
}

/*
	Jsonise the endpt struct.
*/
func (ep *Endpt) To_json( ) ( s string ) {
	if ep == nil {
		s = `{ }`
		return
	}

	s = fmt.Sprintf( `{ "uuid:" %q `,  ep.uuid )
	if ep.ip != "" {
		s += fmt.Sprintf( `, "ip: %q `,  ep.ip )
	}
	if ep.conn_pt != nil {
		s += fmt.Sprintf( `, "sw:" %q, "port:" %q `,  ep.conn_pt.Get_id(), ep.port )
	}

	s += fmt.Sprintf( `, "router": %v`, ep.router )

	s += " }"

	return
}
