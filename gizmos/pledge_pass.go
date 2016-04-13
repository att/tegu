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

	Mnemonic:	pledge_pass
	Abstract:	Passthrough pledge. Manages a reservation which allows user set DSCP markings
				to pass through the flow-mods that Tegu (agent) sets to mark non-reservation 
				traffic DSCP to 0.
	Date:		26 Jan 2016
	Author:		E. Scott Daniels

	Mods:		12 Apr 2016 : Changes to support duplicate refresh.
*/

package gizmos

import (
	"encoding/json"
	"fmt"

	"github.com/att/gopkgs/clike"
)

type Pledge_pass struct {
				Pledge_base	// common fields
	host		*string		// VM (endpoint) where the traffic originates from
	protocol	*string		// tcp/udp or "" if not defined
	tpport		*string		// transport port number or 0 if not defined
	vlan		*string		// vlan id to match with h1 match criteria

							// these aren't checkpointed -- they are discovered at restart if needed
	phost		*string		// physical host where the VM resides
}

/*
	A work struct used to decode a json string using Go's json package which requires things to
	be exported (boo). We need this to easily parse the json saved in the checkpoint file.
	We assume that host1/2 are saved _with_ trailing :port and thus we don't explicitly save/restore
	the tp port fields.  The conversion from checkpoint value to full struct will split them off.
*/
type Json_pledge_pass struct {
	Host		*string
	Protocol	*string
	Commence	int64
	Expiry		int64
	Usrkey		*string
	Id			*string
	Ptype		int
}

// ---- private -------------------------------------------------------------------

/*
	Returns {ID} if the vlan is defined and > 0. Returns an empty string if not 
	defined, or invalid value.
*/
func ( p *Pledge_pass ) vlan2string( ) (vlan string ) {
	vlan = ""
	if p.vlan != nil && clike.Atoi( *p.vlan ) > 0 {
		vlan = "{" + *p.vlan + "}"
	}

	return vlan
}

// ---- public -------------------------------------------------------------------

/*
	Constructor; creates a pledge.
	Creates a passthrough pledge allowing traffic from the indicated host to mark its own traffic with 
	the desired DSCP markings.

	A nil pointer is returned if the expiry time is in the past and the commence time is adjusted forward
	(to the current time) if it is less than the current time.
*/
func Mk_pass_pledge( host *string,  port *string, commence int64, expiry int64, id *string, usrkey *string ) ( p *Pledge_pass, err error ) {

	err = nil
	p = nil

	window, err := mk_pledge_window( commence, expiry )		// make the window and error if commence after expiry
	if err != nil {
		return
	}

	if id == nil {
		dummy := "unnamed-pt-pledge"
		id = &dummy
	}

	p = &Pledge_pass {
		Pledge_base:Pledge_base{
			id: id,
			window: window,
		},
		host: host,
		tpport: port,
		protocol:	&empty_str,
	}

	if usrkey != nil && *usrkey != "" {
		p.usrkey = usrkey
	} else {
		p.usrkey = &empty_str
	}

	return p, nil
}

/*
	Return whether the match on IPv6 flag is true
func (p *Pledge_pass) Get_matchv6() ( bool ) {
	return p.match_v6
}
*/


/*
	Returns pointers to host string (name is plural becaues that's defined in the pledge interface).
	The interface demands two values back so we send a dummy value to keep it happy.
	TODO: We should change the interface to return an array.
*/
func (p *Pledge_pass) Get_hosts( ) ( host *string, dummy *string ) {
	if p == nil {
		return &empty_str, &empty_str
	}

	return p.host, &empty_str
}

/*
	Returns the set of values that are needed to create a pledge in the network:
		pointer to host name,
		the host transport port number and mask or ""
		the commence time,
		the expiry time
*/
func (p *Pledge_pass) Get_values( ) ( host *string,  port *string, commence int64, expiry int64, proto *string ) {
	if p == nil {
		return &empty_str, &empty_str, 0, 0, &empty_str
	}

	c, e := p.window.get_values()
	return p.host,  p.tpport, c, e, p.protocol
}

/*
	Set the vlan IDs associated with the hosts (for matching)
*/
func (p *Pledge_pass) Set_vlan( vlanid *string ) {
	if p == nil {
		return
	}

	p.vlan = vlanid
}

/*
	Returns the matching vlan IDs.
*/
func (p *Pledge_pass) Get_vlan( ) ( vlanid *string ) {
	if p == nil {
		return
	}

	return p.vlan
}

/*
	Create a clone of the pledge. Strings are immutable, so 
	just copying the pointers is fine.
*/
func (p *Pledge_pass) Clone( name string ) ( *Pledge_pass ) {
	if p == nil {
		return nil 
	}

	newp := &Pledge_pass {
		Pledge_base:Pledge_base {
			id:			&name,
			usrkey:		p.usrkey,
			pushed:		p.pushed,
			paused:		p.paused,
		},
		host:		p.host,
		tpport: 	p.tpport,
		protocol: 	p.protocol,
	}

	newp.window = p.window.clone()
	return newp
}

/*
	Accepts another pledge (op) and compares the two returning true if the following values are
	the same:
		hosts, protocol, transport ports, vlan match value, window

	The test for window involves whether the reservation overlaps. If there is any
	overlap they are considered equal windows, otherwise not.

	It gets messy.... if p1.h1 == p2.h2 (hosts reversed), then we must match the
	reverse of port since port and host must align.
*/
func (p *Pledge_pass) Equals( op *Pledge ) ( state bool ) {

	if p == nil {
		return false
	}

	opt, ok := (*op).( *Pledge_pass )			// convert from generic type to specific
	if ok {
		if ! Strings_equal( p.protocol, opt.protocol ) { return false }
		if ! Strings_equal( p.host, opt.host ) { return false }
		if ! Strings_equal( p.tpport, opt.tpport ) { return false }
		if ! Strings_equal( p.vlan, opt.vlan ) { return false }

		if ! p.window.overlaps( opt.window ) { return false; }

		return true							// get here, all things are the same
	}

	return false
}

/*
	Set the protocol string. This string is 'free form' as is needed by the user 
	though it is likely one of the following forms:
		proto:port
		proto::port
		proto:address:port
*/
func (p *Pledge_pass) Set_proto( proto *string ) {
	if p != nil {
		p.protocol = proto
	}
}

/*
	Accept two physical host names and return true if the path 
	associated with the pledge seems to be anchored by the pair. 
	For passthrough only a1 matters, but the interface supports all
	bandwidht types so two must be accepted.
*/
func( p *Pledge_pass ) Same_anchors( a1 *string, a2 *string ) ( bool ) {
	if p == nil || a1 == nil {
		return false
	}

	if p.phost != nil {
		return *a1 == *p.phost
	}

	return false
}

// --------------- interface functions (required) ------------------------------------------------------
/*
	Destruction
*/
func (p *Pledge_pass) Nuke( ) {
	return
	p.host = nil
	p.id = nil
	p.usrkey = nil
}

/*
	Given a json string unpack it and put it into a pledge struct.
	We assume that the host names are name:port and split them apart
	as would be expected.
*/
func (p *Pledge_pass) From_json( jstr *string ) ( err error ){
	if p == nil {
		err = fmt.Errorf( "no passthrough pledge to convert json into" )
		return
	}

	jp := new( Json_pledge_pass )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}

	if jp.Ptype != PT_PASSTHRU {
		err = fmt.Errorf( "json was not a passthrough pledge type type=%d", jp.Ptype )
		return
	}

	p.host, p.tpport, p.vlan  = Split_hpv( jp.Host )		// suss apart host and port
	p.protocol = jp.Protocol
	p.window, _ = mk_pledge_window( jp.Commence, jp.Expiry )
	p.id = jp.Id
	p.usrkey = jp.Usrkey
	p.protocol = jp.Protocol
	if p.protocol == nil {					// we don't tolerate nil ptrs
		p.protocol = &empty_str
	}

	return
}

// --- functions that extend the interface -- pass-only functions ---------
/*
	Add a protocol reference to the pledge (e.g. tcp:80 or udp:4444)
*/
func (p *Pledge_pass) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.protocol = proto
}

/*
	Return the protocol associated with the pledge.
*/
func (p *Pledge_pass) Get_proto( ) ( *string ) {
	if p != nil {
		return p.protocol
	}

	return nil
}

/*
	Set the physical host.
*/
func( p *Pledge_pass) Set_phost( phost *string ) {
	if p != nil {
		p.phost = phost;
	}
}

func( p *Pledge_pass) Get_phost( ) ( *string ) {
	if p != nil {
		return p.phost
	}

	return nil
}

// --- functions required by the interface ------------------------------
/*
	Set match v6 flag based on user input.
	Specific use of v4 vs v6 is deprecated.  It will be sussed out by the 
	agent if the protocol is given. Must keep this for interface compatability.
*/
func (p *Pledge_pass) Set_matchv6( state bool ) {
	return
}


/*
	Accepts a host name and returns true if it matches either of the names associated with
	this pledge.
*/
func (p *Pledge_pass) Has_host( hname *string ) ( bool ) {
	if p != nil {
		return *p.host == *hname
	}

	return false
}


// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
	(Deprecated in favour of Stringer interface)
*/
func (p *Pledge_pass) To_str( ) ( s string ) {
	return p.String()		// handles nil check so we don't need to
}

/*
	Stringer interface so that fmt.Printf( "%s\n", p ) will just work.
*/
func (p *Pledge_pass) String( ) ( s string ) {

	if p == nil {
		return "--nil-pt-pledge--"
	}

	state, caption, diff := p.window.state_str()
	commence, expiry := p.window.get_values( )
	v := p.vlan2string( )

	//NEVER put the usrkey into the string!
	s = fmt.Sprintf( "%s: togo=%ds %s h=%s:%s%s id=%s st=%d ex=%d push=%v ptype=passthrough", state, diff, caption, *p.host, *p.tpport, v, *p.id, commence, expiry, p.pushed )
	return
}

/*
	Generate a json representation of the pledge. This is different than the checkpoint
	string as it is safe to use this in a reservation list that will be presented to
	some user -- no cookie or other 'private' information should be exposed in the
	json generated here.
	We do NOT use the json package because we don't put the object directly in; we render
	useful information, which excludes some of the raw data, and we don't want to have to
	expose the fields publicly that do go into the json output.
*/
func (p *Pledge_pass) To_json( ) ( json string ) {
	if p == nil {
		return "{ }"
	}

	state, _, diff := p.window.state_str()		// get state as a string
	v := p.vlan2string( )

	json = fmt.Sprintf( `{ "state": %q, "time": %d, "host": "%s:%s%s", "id": %q, "ptype": %d }`, state, diff, *p.host, *p.tpport, v, *p.id,  PT_PASSTHRU )

	return
}

/*
	Build a checkpoint string -- probably json, but it will contain everything including the user key.
	We still won't use the json package because that means making all of the fields available to outside
	users.

	If the pledge is expired, the string "expired" is returned which seems a bit better than just returning
	an empty string, or "{ }" which is meaningless.

	The kind value is a constant that allows the user to know what kind of pledge this is for easy reload
	without having to blindly unbundle the json into all possible pledge types to discover the type. The
	type _is_ put into the json for error checking internally.
*/
func (p *Pledge_pass) To_chkpt( ) ( chkpt string ) {

	if p.Is_expired( ) {			// will show expired if p is nil, so safe without check
		chkpt = "expired"
		return
	}

	commence, expiry := p.window.get_values()
	v := p.vlan2string( )

	chkpt = fmt.Sprintf( `{ "host": "%s:%s%s", "commence": %d, "expiry": %d, "id": %q, "usrkey": %q, "ptype": %d }`, *p.host, *p.tpport, v, commence, expiry, *p.id, *p.usrkey, PT_PASSTHRU )

	return
}
