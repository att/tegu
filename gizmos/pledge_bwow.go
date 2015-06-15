// vi: sw=4 ts=4:

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

	Mods:
*/

package gizmos

import (
	"encoding/json"
	"fmt"

	"codecloud.web.att.com/gopkgs/clike"
)

type Pledge_bwow struct {
	src			*string		// hosts; gate applied for traffic from source to dest
	dest		*string		// could be an external IP  (e.g. !/IP-address
	protocol	*string		// tcp/udp:port
	src_tpport	*string		// transport port number or 0 if not defined
	dest_tpport	*string		// thee match h1/h2 respectively
	src_vlan	*string		// vlan id to match with src match criteria
	window		*pledge_window
	bandw_out	int64		// bandwidth to reserve outbound from src
	dscp		int			// dscp value that should be propigated
	id			*string		// name that the client can use to manage (modify/delete)
	qid			*string		// name that we'll assign to the queue which allows us to look up the pledge's queues
	usrkey		*string		// a 'cookie' supplied by the user to prevent any other user from modifying
	phost		*string		// the physical host
	pushed		bool		// set when pledge has been pushed into the openflow environment (skoogi)
	paused		bool		// set if reservation has been paused
	match_v6	bool		// true if we should force flow-mods to match on IPv6
	epoint		*Gate		// endpoint where the gate is applied
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
		if p.src_vlan != nil && clike.Atoi( *p.src_vlan ) > 0 {
			v = "{" + *p.src_vlan + "}"
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
		src: src,
		dest: dest,
		src_tpport: p1,
		dest_tpport: p2,
		bandw_out:	bandw_out,
		id: id,
		qid: &empty_str,
		dscp: dscp,
		protocol:	&empty_str,
		match_v6: false,
		window: window,
	}

	if *usrkey != "" {
		p.usrkey = usrkey
	} else {
		p.usrkey = &empty_str
	}

	return
}

/*
	Return whether the match on IPv6 flag is true
*/
func (p *Pledge_bwow) Get_matchv6() ( bool ) {
	return p.match_v6
}

/*
	Returns a pointer to the queue ID
*/
func (p *Pledge_bwow) Get_qid( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.qid
}

/*
	Returns the current total amount of bandwidth that has been assigned to the pledge.
*/
func (p *Pledge_bwow) Get_bandw( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_out
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge_bwow) Get_hosts( ) ( *string, *string ) {
	if p == nil {
		return &empty_str, &empty_str
	}

	return p.src, p.dest
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

	c, e = p.window.get_values()
	return p.src, p.dest, p.src_tpport, p.dest_tpport, c, e
}

/*
	Return the dscp that was submitted with the reservation, and the state of the keep on
	exit flag.
*/
func (p *Pledge_bwow) Get_dscp( ) ( int ) {
	if p == nil {
		return 0
	}

	return p.dscp
}

/*
	Return the commence and expiry times.
*/
func (p *Pledge_bwow) Get_window( ) ( int64, int64 ) {
	if p == nil {
		return 0, 0
	}

	return p.window.get_values()
}

/*
	Set the vlan IDs associated with the hosts (for matching)
*/
func (p *Pledge_bwow) Set_vlan( v1 *string ) {
	if p == nil {
		return
	}

	p.src_vlan = v1
}

/*
	Returns the matching vlan IDs.
*/
func (p *Pledge_bwow) Get_vlan( ) ( v1 *string ) {
	if p == nil {
		return
	}

	return p.src_vlan
}

/*
	Create a clone of the pledge.  
*/
func (p *Pledge_bwow) Clone( name string ) ( *Pledge_bwow ) {
	newp := &Pledge_bwow {
		src:		p.src,
		dest:		p.dest,
		src_tpport: 	p.src_tpport,
		dest_tpport: 	p.dest_tpport,
		bandw_out:	p.bandw_out,
		dscp:		p.dscp,
		id:			&name,
		usrkey:		p.usrkey,
		qid:		p.qid,
		pushed:		p.pushed,
		paused:		p.paused,
	}

	ep := *p.epoint		// make copy
	newp.epoint = &ep

	newp.window = p.window.clone()
	return newp
}

/*
	Accepts another pledge (op) and compairs the two returning true if the following values are 
	the same:
		hosts, protocol, transport ports, vlan match value, window

	The test for window involves whether the reservation overlaps. If there is any
	overlap they are considerd equal windows, otherwise not.

	For one way reservations the reerse ordering of the hosts is NOT a dup.
*/
func (p *Pledge_bwow) Equals( op *Pledge ) ( state bool ) {
	
	if p == nil {
		return
	}

	obw, ok := (*op).( *Pledge_bwow )			// convert from generic type to specific
	if ok {
		if ! Strings_equal( p.protocol, obw.protocol ) { return false } // simple tests that don't swap if hosts are reversed

															// more complicated when only diff is h1 and h2 are swapped
		if Strings_equal( p.src, obw.src ) {			// if hosts matche 1:1 and 2:2
			if !Strings_equal( p.dest, obw.dest ) {		// then expect vlan and port to match the same
				return false
			}

			if ! Strings_equal( p.src_tpport, obw.src_tpport ) { return false }
			if ! Strings_equal( p.dest_tpport, obw.dest_tpport ) { return false }
			if ! Strings_equal( p.src_vlan, obw.src_vlan ) { return false }
		}

		if !p.window.overlaps( obw.window ) {
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
	p.src = nil
	p.dest = nil
	p.id = nil
	p.qid = nil
	p.usrkey = nil
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

	p.src, p.src_tpport, p.src_vlan  = Split_hpv( jp.Src )		// suss apart host and port
	p.dest, p.dest_tpport, _  = Split_hpv( jp.Dest )

	p.protocol = jp.Protocol
	p.window, _ = mk_pledge_window( jp.Commence, jp.Expiry )
	p.id = jp.Id
	p.dscp = jp.Dscp
	p.usrkey = jp.Usrkey
	p.qid = jp.Qid
	p.bandw_out = jp.Bandwout

	p.protocol = jp.Protocol
	if p.protocol == nil {					// we don't tollerate nil ptrs
		p.protocol = &empty_str
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

	p.protocol = proto
}

/*
	Return the protocol associated with the pledge.
*/
func (p *Pledge_bwow) Get_proto( ) ( *string ) {
	if p== nil {
		return nil
	}

	return p.protocol
}

func (p *Pledge_bwow ) Set_phost( phost *string ) {
	if p== nil {
		return	
	}

	p.phost = phost
	
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
	p.match_v6 = state
}

/*
	Sets a new expiry value on the pledge.
*/
func (p *Pledge_bwow) Set_expiry ( v int64 ) {
	p.window.set_expiry_to( v )
	p.pushed = false		// force it to be resent to ajust times
}

// There is NOT a toggle pause on purpose; don't add one :)

/*
	Puts the pledge into paused state and optionally resets the pushed flag.
*/
func (p *Pledge_bwow) Pause( reset bool ) {
	if p != nil {
		p.paused = true
		if reset {
			p.pushed = false;
		}
	}
}

/*
	Puts the pledge into an unpaused (normal) state and optionally resets the pushed flag.
*/
func (p *Pledge_bwow) Resume( reset bool ) {
	if p != nil {
		p.paused = false
		if reset {
			p.pushed = false;
		}
	}
}

/*
	Accepts a host name and returns true if it matches either of the names associated with
	this pledge.
*/
func (p *Pledge_bwow) Has_host( hname *string ) ( bool ) {
	return *p.src == *hname || *p.dest == *hname
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

	state, caption, diff := p.window.state_str()
	commence, expiry := p.window.get_values( )
	v1 := p.vlan2string( )

	//NEVER put the usrkey into the string!
	s = fmt.Sprintf( "%s: togo=%ds %s h1=%s:%s%s h2=%s:%s id=%s qid=%s st=%d ex=%d bwo=%d push=%v dscp=%d ptype=bw_oneway", state, diff, caption,
		*p.src, *p.dest_tpport, v1, *p.dest, *p.dest_tpport,  *p.id, *p.qid, commence, expiry, p.bandw_out, p.pushed, p.dscp )
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

	state, _, diff := p.window.state_str()		// get state as a string
	v1 := p.vlan2string( )

	json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwout": %d, "src": "%s:%s%s", "dest": "%s:%s", "id": %q, "qid": %q, "dscp": %d, "ptype": %d }`,
				state, diff,  p.bandw_out, *p.src, *p.src_tpport, v1, *p.dest, *p.dest_tpport, *p.id, *p.qid, p.dscp, PT_OWBANDWIDTH )

	return
}

/*
	Build a checkpoint string -- probably json, but it will contain everything including the user key.
	We still won't use the json package because that means making all of the fileds available to outside
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
	
	commence, expiry := p.window.get_values()
	v1 := p.vlan2string( )

	chkpt = fmt.Sprintf( `{ "src": "%s:%s%s", "dest": "%s:%s", "commence": %d, "expiry": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q, "dscp": %d, "ptype": %d }`, 
			*p.src, *p.src_tpport, v1, *p.dest, *p.dest_tpport,  commence, expiry, p.bandw_out, *p.id, *p.qid, *p.usrkey, p.dscp, PT_OWBANDWIDTH )

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
	Sets the pushed flag to true.
*/
func (p *Pledge_bwow) Set_pushed( ) {
	if p != nil {
		p.pushed = true
	}
}

/*
	Resets the pushed flag to false.
*/
func (p *Pledge_bwow) Reset_pushed( ) {
	if p != nil {
		p.pushed = false
	}
}

/*
	Returns true if the pushed flag has been set to true.
*/
func (p *Pledge_bwow) Is_pushed( ) (bool) {
	if p == nil {
		return false
	}

	return p.pushed
}

/*
	Returns true if the reservation is paused.
*/
func (p *Pledge_bwow) Is_paused( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.paused
}

/*
	Returns true if the pledge has expired (the current time is greather than
	the expiry time in the pledge).
*/
func (p *Pledge_bwow) Is_expired( ) ( bool ) {
	if p == nil {
		return true
	}

	return p.window.is_expired()
}

/*
	Returns true if the pledge has not become active (the commence time is >= the current time).
*/
func (p *Pledge_bwow) Is_pending( ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_pending()
}

/*
	Returns true if the pledge is currently active (the commence time is <= than the current time
	and the expiry time is > the current time.
*/
func (p *Pledge_bwow) Is_active( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active()
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds have passed.
*/
func (p *Pledge_bwow) Is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active_soon( window )
}

/*
	Check the cookie passed in and return true if it matches the cookie on the
	pledge.
*/
func (p *Pledge_bwow) Is_valid_cookie( c *string ) ( bool ) {
	return *c == *p.usrkey
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
*/
func (p *Pledge_bwow) Concluded_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.concluded_recently( window )
}

/*
	Returns true if pledge expired long enough ago that it can safely be discarded.
	The window is the number of seconds that the pledge must have been expired to
	be considered extinct.
*/
func (p *Pledge_bwow) Is_extinct( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_extinct( window )
}

/*
	Returns true if pledge started recently (between now and now - window seconds) and
	has not expired yet. If the pledge started within the window, but expired before
	the call to this function false is returned.
*/
func (p *Pledge_bwow) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.commenced_recently( window )
}

/*
	Returns a pointer to the ID string of the pledge.
*/
func (p *Pledge_bwow) Get_id( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.id
}


