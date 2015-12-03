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

	Mnemonic:	pledge_bwow
	Abstract:	Oneway bandwidth pledge -- provides pledge interface.
				Similar to a bandwidth pledge but only sets up 'outbound'
				related things (marking and maybe rate limiting). It's used
				when the reservation is with a cross platform or external
				endpoint and a nova router isn't used to NAT.

				We can make use of some of the bandwidth pledge functions
				like conversion of vlan to string.
	Date:		08 June 2015
	Author:		E. Scott Daniels

	Mods:		18 Jun 2015 : Added set_qid() function.
				29 Jun 2015 : Corrected bug in Equals().
				16 Aug 2015 : Move common code into Pledge_base
				03 Dec 2015 : Added datacache support (sadly means exporting most fields)
*/

package gizmos

import (
	"encoding/json"
	"fmt"

	"github.com/att/gopkgs/clike"
)

type Pledge_bwow struct {
				Pledge_base					// common fields (datacache marked as needed)
	Src			*string		`dcache:"_"`	// hosts; gate applied for traffic from source to dest
	Dest		*string		`dcache:"_"`	// could be an external IP  (e.g. !/IP-address)
	Protocol	*string		`dcache:"_"`	// tcp/udp:port
	Src_tpport	*string		`dcache:"_"`	// transport port number or 0 if not defined
	Dest_tpport	*string		`dcache:"_"`	// thee match h1/h2 respectively
	Src_vlan	*string		`dcache:"_"`	// vlan id to match with src match criteria
	Bandw_out	int64		`dcache:"_"`	// bandwidth to reserve outbound from src
	Dscp		int			`dcache:"_"`	// dscp value that should be propagated
	Qid			*string		`dcache:"_"`	// name that we'll assign to the queue which allows us to look up the pledge's queues
	Phost		*string		`dcache:"_"`	// the physical host
	Match_v6	bool		`dcache:"_"`	// true if we should force flow-mods to match on IPv6
	epoint		*Gate						// endpoint where the gate is applied
}

/*
	A work struct used to decode a json string using Go's json package which requires things to
	be exported (boo). We need this to easily parse the json saved in the checkpoint file.
	We assume that src.dest are saved _with_ trailing :port and thus we don't explicitly save/restore
	the tp port fields.  The conversion from checkpoint value to full struct will split them off.
*/
type Json_pledge_bwow struct {
	Src			*string					// of the form name[:port]{vlan}
	Dest		*string					// of the form name[:port]
	Protocol	*string
	Commence	int64
	Expiry		int64
	Bandwout	int64
	Dscp		int
	Dscp_koe	bool
	Id			*string
	Qid			*string
	Usrkey		*string
	Match_v6	bool
	Ptype		int
}

// ---- private -------------------------------------------------------------------

/*
	Formats vlan in the {n} format for adding to a host representation which is
	now   token/project/vm:port{vlan}. If vlan is < 0 then the empty string is returned.
*/
func ( p *Pledge_bwow ) vlan2string( ) ( v string ) {
	if p != nil {
	v = ""
		if p.Src_vlan != nil && clike.Atoi( *p.Src_vlan ) > 0 {
			v = "{" + *p.Src_vlan + "}"
		}
	}

	return ""
}

// ---- public -------------------------------------------------------------------

/*
	Constructor; creates a pledge.
	Creates a pledge of bandwidth between two hosts, allowing dest to be nil which indicates that the
	pledge exists between src and any other host. If commence is 0, then the current time (now) is used.

	A nil pointer is returned if the expiry time is in the past and the commence time is adjusted forward
	(to the current time) if it is less than the current time.
*/
func Mk_bwow_pledge(	src *string, dest *string, p1 *string, p2 *string, commence int64, expiry int64, bandw_out int64, id *string, usrkey *string, dscp int ) ( p *Pledge_bwow, err error ) {

	err = nil
	p = nil

	window, err := mk_pledge_window( commence, expiry )		// make the window and error if commence after expiry
	if err != nil {
		return
	}

	if *dest == "" || *dest == "any" {			// no longer allowed
		p = nil;
		err = fmt.Errorf( "bad dest name submitted: %s", *dest )
		obj_sheep.Baa( 1, "pledge: %s", err )
		return
	}

	p = &Pledge_bwow {
		Pledge_base:Pledge_base{
			Id: id,
			Window: window,
		},
		Src: src,
		Dest: dest,
		Src_tpport: p1,
		Dest_tpport: p2,
		Bandw_out:	bandw_out,
		Qid: &empty_str,
		Dscp: dscp,
		Protocol:	&empty_str,
		Match_v6: false,
	}

	if *usrkey != "" {
		p.Usrkey = usrkey
	} else {
		p.Usrkey = &empty_str
	}

	return
}

/*
	Return whether the match on IPv6 flag is true
*/
func (p *Pledge_bwow) Get_matchv6() ( bool ) {
	return p.Match_v6
}

/*
	Returns a pointer to the queue ID
*/
func (p *Pledge_bwow) Get_qid( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.Qid
}

/*
	Returns the current total amount of bandwidth that has been assigned to the pledge.
*/
func (p *Pledge_bwow) Get_bandwidth( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.Bandw_out
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge_bwow) Get_hosts( ) ( *string, *string ) {
	if p == nil {
		return &empty_str, &empty_str
	}

	return p.Src, p.Dest
}

/*
	Returns the set of values that are needed to create a pledge in the network:
		pointer to src name,
		pointer to dest name,
		tcp/udp port for src,
		tcp/udp port for dest
		commence time
		expiry time
*/
func (p *Pledge_bwow) Get_values( ) ( src *string, dest *string, p1 *string, p2 *string, c int64, e int64 ) {
	if p == nil {
		return &empty_str, &empty_str, &zero_str, &zero_str, 0, 0
	}

	c, e = p.Window.get_values()
	return p.Src, p.Dest, p.Src_tpport, p.Dest_tpport, c, e
}

/*
	Return the dscp that was submitted with the reservation, and the state of the keep on
	exit flag.
*/
func (p *Pledge_bwow) Get_dscp( ) ( int ) {
	if p == nil {
		return 0
	}

	return p.Dscp
}

/*
	Set the vlan IDs associated with the hosts (for matching)
*/
func (p *Pledge_bwow) Set_vlan( v1 *string ) {
	if p == nil {
		return
	}

	p.Src_vlan = v1
}

/*
	Set the queue ID associated with the pledge.
*/
func (p *Pledge_bwow) Set_qid( id *string ) {
	if( p == nil ) {
		return
	}

	p.Qid = id
}

/*
	Returns the matching vlan IDs.
*/
func (p *Pledge_bwow) Get_vlan( ) ( v1 *string ) {
	if p == nil {
		return
	}

	return p.Src_vlan
}

/*
	Create a clone of the pledge.
*/
func (p *Pledge_bwow) Clone( name string ) ( *Pledge_bwow ) {
	newp := &Pledge_bwow {
		Pledge_base:Pledge_base {
			Id:			&name,
			Usrkey:		p.Usrkey,
			Pushed:		p.Pushed,
			Paused:		p.Paused,
		},
		Src:		p.Src,
		Dest:		p.Dest,
		Src_tpport: 	p.Src_tpport,
		Dest_tpport: 	p.Dest_tpport,
		Bandw_out:	p.Bandw_out,
		Dscp:		p.Dscp,
		Qid:		p.Qid,
	}

	ep := *p.epoint		// make copy
	newp.epoint = &ep

	newp.Window = p.Window.clone()
	return newp
}

/*
	Accepts another pledge (op) and compares the two returning true if the following values are
	the same:
		hosts, protocol, transport ports, vlan match value, window

	The test for window involves whether the reservation overlaps. If there is any
	overlap they are considered equal windows, otherwise not.

	For one way reservations the reverse ordering of the hosts is NOT a dup.
*/
func (p *Pledge_bwow) Equals( op *Pledge ) ( state bool ) {

	if p == nil {
		return false
	}

	obw, ok := (*op).( *Pledge_bwow )			// convert from generic type to specific
	if ok {
		if ! Strings_equal( p.Protocol, obw.Protocol ) { return false } // simple tests that don't swap if hosts are reversed

		if !Strings_equal( p.Src, obw.Src ) { return false }
		if !Strings_equal( p.Dest, obw.Dest ) { return false }

		if !Strings_equal( p.Src_tpport, obw.Src_tpport ) { return false }		// hosts can match if ports are different
		if !Strings_equal( p.Dest_tpport, obw.Dest_tpport ) { return false }
		if !Strings_equal( p.Src_vlan, obw.Src_vlan ) { return false }

		if !p.Window.overlaps( obw.Window ) {
			return false;
		}

		return true							// get here, all things are the same
	}

	return false
}

// --------------- interface functions (required) ------------------------------------------------------
/*
	Destruction
*/
func (p *Pledge_bwow) Nuke( ) {
	p.Src = nil
	p.Dest = nil
	p.Id = nil
	p.Qid = nil
	p.Usrkey = nil
}

/*
	Given a json string unpack it and put it into a pledge struct.
	We assume that the host names are name:port and split them apart
	as would be expected.
*/
func (p *Pledge_bwow) From_json( jstr *string ) ( err error ){
	jp := new( Json_pledge_bwow )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}

	if jp.Ptype != PT_OWBANDWIDTH {
		err = fmt.Errorf( "json was not a bandwidth pledge type" )
		return
	}

	p.Src, p.Src_tpport, p.Src_vlan  = Split_hpv( jp.Src )		// suss apart host and port
	p.Dest, p.Dest_tpport, _  = Split_hpv( jp.Dest )

	p.Protocol = jp.Protocol
	p.Window, _ = mk_pledge_window( jp.Commence, jp.Expiry )
	p.Id = jp.Id
	p.Dscp = jp.Dscp
	p.Usrkey = jp.Usrkey
	p.Qid = jp.Qid
	p.Bandw_out = jp.Bandwout

	p.Protocol = jp.Protocol
	if p.Protocol == nil {					// we don't tolerate nil ptrs
		p.Protocol = &empty_str
	}

	return
}

// --- functions that extend the interface -- bw-only functions ---------

/*
	Add a protocol reference to the pledge (e.g. tcp:80 or udp:4444)
*/
func (p *Pledge_bwow) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.Protocol = proto
}

/*
	Return the protocol associated with the pledge.
*/
func (p *Pledge_bwow) Get_proto( ) ( *string ) {
	if p== nil {
		return nil
	}

	return p.Protocol
}

func (p *Pledge_bwow ) Set_phost( phost *string ) {
	if p== nil {
		return
	}

	p.Phost = phost

}

/*
	Associate a gate with the pledge
*/
func (p *Pledge_bwow) Set_gate( g *Gate ) {
	if p != nil {
		p.epoint = g
	}
}

/*
	Return the associated gate
*/
func (p *Pledge_bwow) Get_gate( ) ( *Gate ) {
	if p != nil {
		return p.epoint
	}

	return nil
}


// --- functions required by the interface ------------------------------
/*
	Set match v6 flag based on user input.
*/
func (p *Pledge_bwow) Set_matchv6( state bool ) {
	p.Match_v6 = state
}

/*
	Accepts a host name and returns true if it matches either of the names associated with
	this pledge.
*/
func (p *Pledge_bwow) Has_host( hname *string ) ( bool ) {
	return *p.Src == *hname || *p.Dest == *hname
}


// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
*/
func (p *Pledge_bwow) To_str( ) ( s string ) {
	return p.String()
}

/*
	Stringer interface so that fmt.Printf( "%s\n", p ) will just work.
*/
func (p *Pledge_bwow) String( ) ( s string ) {

	if p == nil {
		return ""
	}

	state, caption, diff := p.Window.state_str()
	commence, expiry := p.Window.get_values( )
	v1 := p.vlan2string( )

	//NEVER put the usrkey into the string!
	s = fmt.Sprintf( "%s: togo=%ds %s h1=%s:%s%s h2=%s:%s id=%s qid=%s st=%d ex=%d bwo=%d push=%v dscp=%d ptype=bw_oneway", state, diff, caption,
		*p.Src, *p.Dest_tpport, v1, *p.Dest, *p.Dest_tpport,  *p.Id, *p.Qid, commence, expiry, p.Bandw_out, p.Pushed, p.Dscp )
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
func (p *Pledge_bwow) To_json( ) ( json string ) {
	if p == nil {
		return "{ }"
	}

	state, _, diff := p.Window.state_str()		// get state as a string
	v1 := p.vlan2string( )

	json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwout": %d, "src": "%s:%s%s", "dest": "%s:%s", "id": %q, "qid": %q, "dscp": %d, "ptype": %d }`,
				state, diff,  p.Bandw_out, *p.Src, *p.Src_tpport, v1, *p.Dest, *p.Dest_tpport, *p.Id, *p.Qid, p.Dscp, PT_OWBANDWIDTH )

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
func (p *Pledge_bwow) To_chkpt( ) ( chkpt string ) {

	if p.Is_expired( ) {			// will show expired if p is nil, so safe without check
		chkpt = "expired"
		return
	}

	commence, expiry := p.Window.get_values()
	v1 := p.vlan2string( )

	chkpt = fmt.Sprintf( `{ "src": "%s:%s%s", "dest": "%s:%s", "commence": %d, "expiry": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q, "dscp": %d, "ptype": %d }`,
			*p.Src, *p.Src_tpport, v1, *p.Dest, *p.Dest_tpport,  commence, expiry, p.Bandw_out, *p.Id, *p.Qid, *p.Usrkey, p.Dscp, PT_OWBANDWIDTH )

	return
}


/*
DEPRECATED -- use switch p.(type)  or p, ok := x.(*Pledge_bwow) instead
	Returns true if PT_OWBANDWIDTH passed in; false otherwise.
func (p *Pledge_bwow) Is_ptype( kind int ) ( bool ) {
	return kind == PT_OWBANDWIDTH
}
*/

/*
	Returns true if pledge started recently (between now and now - window seconds) and
	has not expired yet. If the pledge started within the window, but expired before
	the call to this function false is returned.
*/
func (p *Pledge_bwow) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.Window.commenced_recently( window )
}
