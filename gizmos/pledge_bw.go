// vi: sw=4 ts=4:

/*

	Mnemonic:	pledge_bw
	Abstract:	Bandwidth pledge -- provides pledge interface.
	Date:		20 November 2013
	Author:		E. Scott Daniels

	Mods:		08 Jan 2014 - Corrected bug that wasn't rejecting a pledge if the expiry time was < 0.
				11 Feb 2014 - Added better doc to some functions and we now save the queue id in
							the checkpoint file.
				13 May 2014 - Added support to enable an exit dscp value on a reservation.
				05 Jun 2014 - Added support for pause.
				20 Jun 2014 - Corrected bug that allowed future start time with an earlier expry time
							to be accepted.
				07 Jul 2014 - Added clone function.
				24 Sep 2014 - Support for keep/delete toggle for dscp values
				16 Jan 2014 - Conversion of transport port information to string to allow for mask.
				17 Feb 2015 - Added mirroring
				24 Feb 2015 - Corrected to_json reference of tpport values (pointers, not strings)
				21 May 2015 - Converted from generic pledge type.
				01 Jun 2015 - Addded equal() support
*/

package gizmos

import (
	"encoding/json"
	"fmt"

	"codecloud.web.att.com/gopkgs/clike"
)

type Pledge_bw struct {
	host1		*string
	host2		*string
	protocol	*string		// tcp/udp:port
	tpport1		*string		// transport port number or 0 if not defined
	tpport2		*string		// thee match h1/h2 respectively
	vlan1		*string		// vlan id to match with h1 match criteria
	vlan2		*string		// vlan id to macth with h2
	window		*pledge_window
	bandw_in	int64		// bandwidth to reserve inbound to host1
	bandw_out	int64		// bandwidth to reserve outbound from host1
	dscp		int			// dscp value that should be propigated
	dscp_koe	bool		// true if the dscp value should be kept when a packet exits the environment
	id			*string		// name that the client can use to manage (modify/delete)
	qid			*string		// name that we'll assign to the queue which allows us to look up the pledge's queues
	usrkey		*string		// a 'cookie' supplied by the user to prevent any other user from modifying
	path_list	[]*Path		// list of paths that represent the bandwith and can be used to send flowmods etc.
	pushed		bool		// set when pledge has been pushed into the openflow environment (skoogi)
	paused		bool		// set if reservation has been paused
	match_v6	bool		// true if we should force flow-mods to match on IPv6
}

/*
	A work struct used to decode a json string using Go's json package which requires things to
	be exported (boo). We need this to easily parse the json saved in the checkpoint file.
	We assume that host1/2 are saved _with_ trailing :port and thus we don't explicitly save/restore
	the tp port fields.  The conversion from checkpoint value to full struct will split them off.
*/
type Json_pledge_bw struct {
	Host1		*string
	Host2		*string
	Protocol	*string
	Commence	int64
	Expiry		int64
	Bandwin		int64
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
	Formats v1 and v2 in the {n} format for adding to a host representation which is 
	now   token/project/vm:port{vlan}.
*/
func ( p *Pledge_bw ) bw_vlan2string( ) (v1 string, v2 string) {
	v1 = ""
	v2 = ""
	if p.vlan1 != nil && clike.Atoi( *p.vlan1 ) > 0 {
		v1 = "{" + *p.vlan1 + "}"
	}
	if p.vlan2 != nil && clike.Atoi( *p.vlan2 ) > 0  {
		v2 = "{" + *p.vlan2 + "}"
	}

	return v1, v2
}

// ---- public -------------------------------------------------------------------

/*
	Constructor; creates a pledge.
	Creates a pledge of bandwidth between two hosts, allowing host2 to be nil which indicates that the
	pledge exists between host1 and any other host. If commence is 0, then the current time (now) is used.

	A nil pointer is returned if the expiry time is in the past and the commence time is adjusted forward
	(to the current time) if it is less than the current time.
*/
func Mk_bw_pledge(	host1 *string, host2 *string, p1 *string, p2 *string, commence int64, expiry int64, bandw_in int64, bandw_out int64, id *string, usrkey *string, dscp int, dscp_koe bool ) ( p *Pledge_bw, err error ) {

	err = nil
	p = nil

	window, err := mk_pledge_window( commence, expiry )		// make the window and error if commence after expiry
	if err != nil {
		return
	}
	
	if *host2 == "" || *host2 == "any" {			// no longer allowed
		p = nil;
		err = fmt.Errorf( "bad host2 name submitted: %s", *host2 )
		obj_sheep.Baa( 1, "pledge: %s", err )
		return
	}

	p = &Pledge_bw {
		host1: host1,
		host2: host2,
		tpport1: p1,
		tpport2: p2,
		bandw_in:	bandw_in,
		bandw_out:	bandw_out,
		id: id,
		qid: &empty_str,
		dscp: dscp,
		protocol:	&empty_str,
		dscp_koe: dscp_koe,
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
func (p *Pledge_bw) Get_matchv6() ( bool ) {
	return p.match_v6
}

/*
	Returns a pointer to the queue ID
*/
func (p *Pledge_bw) Get_qid( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.qid
}

/*
	Returns the current total amount of bandwidth that has been assigned to the pledge.
*/
func (p *Pledge_bw) Get_bandw( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_in + p.bandw_out
}

/*
	Returns the current amount of bandwidth that has been assigned to the pledge for traffic outbound from host1.
*/
func (p *Pledge_bw) Get_bandw_out( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_out
}

/*
	Returns the current amount of bandwidth that has been assigned to the pledge for traffic inbound to hsot1.
*/
func (p *Pledge_bw) Get_bandw_in( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_in
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge_bw) Get_hosts( ) ( *string, *string ) {
	if p == nil {
		return &empty_str, &empty_str
	}

	return p.host1, p.host2
}

/*
	Returns the set of values that are needed to create a pledge in the network:
		pointer to host1 name,
		pointer to host2 name,
		the h1 transport port number and mask or ""
		the h2 transport port number and mask or ""
		the commence time,
		the expiry time,
		the inbound bandwidth,
		the outbound bandwidth
*/
func (p *Pledge_bw) Get_values( ) ( h1 *string, h2 *string, p1 *string, p2 *string, commence int64, expiry int64, bw_in int64, bw_out int64 ) {
	if p == nil {
		return &empty_str, &empty_str, &empty_str, &empty_str, 0, 0, 0, 0
	}

	c, e := p.window.get_values()
	return p.host1, p.host2, p.tpport1, p.tpport2, c, e, p.bandw_in, p.bandw_out
}

/*
	Return the dscp that was submitted with the reservation, and the state of the keep on
	exit flag.
*/
func (p *Pledge_bw) Get_dscp( ) ( int, bool ) {
	if p == nil {
		return 0, false
	}

	return p.dscp, p.dscp_koe
}

/*
	Returns the list of path objects that are needed to fulfill the pledge. Mulitple
	paths occur if the network is split.
*/
func (p *Pledge_bw) Get_path_list( ) ( []*Path ) {
	if p == nil {
		return nil
	}
	return p.path_list
}

/*
	Return the commence and expiry times.
*/
func (p *Pledge_bw) Get_window( ) ( int64, int64 ) {
	if p == nil {
		return 0, 0
	}

	return p.window.get_values()
}

/*
	Set the vlan IDs associated with the hosts (for matching)
*/
func (p *Pledge_bw) Set_vlan( v1 *string, v2 *string ) {
	if p == nil {
		return
	}

	p.vlan1 = v1
	p.vlan2 = v2	
}

/*
	Returns the matching vlan IDs.
*/
func (p *Pledge_bw) Get_vlan( ) ( v1 *string, v2 *string ) {
	if p == nil {
		return
	}

	return p.vlan1, p.vlan2
}

/*
	Create a clone of the pledge.  The path is NOT a copy, but just a reference to the list
	from the original.
*/
func (p *Pledge_bw) Clone( name string ) ( *Pledge_bw ) {
	newpbw := &Pledge_bw {
		host1:		p.host1,
		host2:		p.host2,
		tpport1: 	p.tpport1,
		tpport2: 	p.tpport2,
		bandw_in:	p.bandw_in,
		bandw_out:	p.bandw_out,
		dscp:		p.dscp,
		id:			&name,
		usrkey:		p.usrkey,
		qid:		p.qid,
		path_list:	p.path_list,
		pushed:		p.pushed,
		paused:		p.paused,
	}

	newpbw.window = p.window.clone()
	return newpbw
}

/*
	Accepts another pledge (op) and compairs the two returning true if the following values are 
	the same:
		hosts, protocol, transport ports, vlan match value, window

	The test for window involves whether the reservation overlaps. If there is any
	overlap they are considerd equal windows, otherwise not.

	It gets messy.... if p1.h1 == p2.h2 (hosts reversed), then we must match the 
	reverse of port since port and host must align.
*/
func (p *Pledge_bw) Equals( op *Pledge ) ( state bool ) {
	
	if p == nil {
		return
	}

	obw, ok := (*op).( *Pledge_bw )			// convert from generic type to specific
	if ok {
		if ! Strings_equal( p.protocol, obw.protocol ) { return false } // simple tests that don't swap if hosts are reversed

															// more complicated when only diff is h1 and h2 are swapped
		if Strings_equal( p.host1, obw.host1 ) {			// if hosts matche 1:1 and 2:2
			if !Strings_equal( p.host2, obw.host2 ) {		// then expect vlan and port to match the same
				return false
			}

			if ! Strings_equal( p.tpport1, obw.tpport1 ) { return false }
			if ! Strings_equal( p.tpport2, obw.tpport2 ) { return false }
			if ! Strings_equal( p.vlan1, obw.vlan1 ) { return false }
			if ! Strings_equal( p.vlan2, obw.vlan2 ) { return false }
		} else {
			if Strings_equal( p.host1, obw.host2 ) {			// if hosts are swapped and match
				if !Strings_equal( p.host2, obw.host1 ) {		// then expect the port and vlan vlues to match swapped
					return false
				}

				if ! Strings_equal( p.tpport1, obw.tpport2 ) { return false }
				if ! Strings_equal( p.tpport2, obw.tpport1 ) { return false }
				if ! Strings_equal( p.vlan1, obw.vlan2 ) { return false }
				if ! Strings_equal( p.vlan2, obw.vlan1 ) { return false }
			} else {
				return false
			}
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
func (p *Pledge_bw) Nuke( ) {
	p.host1 = nil
	p.host2 = nil
	p.id = nil
	p.qid = nil
	p.usrkey = nil
	if p.path_list != nil {
		for i := range p.path_list {
			p.path_list[i] = nil
		}
	}
}

/*
	Given a json string unpack it and put it into a pledge struct.
	We assume that the host names are name:port and split them apart
	as would be expected.
*/
func (p *Pledge_bw) From_json( jstr *string ) ( err error ){
	jp := new( Json_pledge_bw )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}

	if jp.Ptype != PT_BANDWIDTH {
		err = fmt.Errorf( "json was not a bandwidth pledge type" )
		return
	}

	p.host1, p.tpport1, p.vlan1  = Split_hpv( jp.Host1 )		// suss apart host and port
	p.host2, p.tpport2, p.vlan2  = Split_hpv( jp.Host2 )

	p.protocol = jp.Protocol
	p.window, _ = mk_pledge_window( jp.Commence, jp.Expiry )
	p.id = jp.Id
	p.dscp = jp.Dscp
	p.dscp_koe = jp.Dscp_koe
	p.usrkey = jp.Usrkey
	p.qid = jp.Qid
	p.bandw_out = jp.Bandwout
	p.bandw_in = jp.Bandwin

	p.protocol = jp.Protocol
	if p.protocol == nil {					// we don't tollerate nil ptrs
		p.protocol = &empty_str
	}

	return
}

// --- functions that extend the interface -- bw-only functions ---------
/*
	Associates a queue ID with the pledge.
*/
func (p *Pledge_bw) Set_qid( id *string ) {
	p.qid = id
}

/*
	Associates a path list with the pledge.
*/
func (p *Pledge_bw) Set_path_list( pl []*Path ) {
	p.path_list = pl
}

/*
	Add a protocol reference to the pledge (e.g. tcp:80 or udp:4444)
*/
func (p *Pledge_bw) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.protocol = proto
}

/*
	Return the protocol associated with the pledge.
*/
func (p *Pledge_bw) Get_proto( ) ( *string ) {
	return p.protocol
}

// --- functions required by the interface ------------------------------
/*
	Set match v6 flag based on user input.
*/
func (p *Pledge_bw) Set_matchv6( state bool ) {
	p.match_v6 = state
}

/*
	Sets a new expiry value on the pledge.
*/
func (p *Pledge_bw) Set_expiry ( v int64 ) {
	p.window.set_expiry_to( v )
	p.pushed = false		// force it to be resent to ajust times
}

// There is NOT a toggle pause on purpose; don't add one :)

/*
	Puts the pledge into paused state and optionally resets the pushed flag.
*/
func (p *Pledge_bw) Pause( reset bool ) {
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
func (p *Pledge_bw) Resume( reset bool ) {
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
func (p *Pledge_bw) Has_host( hname *string ) ( bool ) {
	return *p.host1 == *hname || *p.host2 == *hname
}


// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
*/
func (p *Pledge_bw) To_str( ) ( s string ) {
	return p.String()
}

/*
	Stringer interface so that fmt.Printf( "%s\n", p ) will just work.
*/
func (p *Pledge_bw) String( ) ( s string ) {

	if p == nil {
		return ""
	}

	state, caption, diff := p.window.state_str()
	commence, expiry := p.window.get_values( )
	v1, v2 := p.bw_vlan2string( )

	//NEVER put the usrkey into the string!
	s = fmt.Sprintf( "%s: togo=%ds %s h1=%s:%d%s h2=%s:%d%s id=%s qid=%s st=%d ex=%d bwi=%d bwo=%d push=%v dscp=%d ptype=bandwidth koe=%v", state, diff, caption,
		*p.host1, *p.tpport2, v1, *p.host2, *p.tpport2, v2, *p.id, *p.qid, commence, expiry, p.bandw_in, p.bandw_out, p.pushed, p.dscp, p.dscp_koe )
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
func (p *Pledge_bw) To_json( ) ( json string ) {
	if p == nil {
		return "{ }"
	}

	state, _, diff := p.window.state_str()		// get state as a string
	v1, v2 := p.bw_vlan2string( )

	json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwin": %d, "bandwout": %d, "host1": "%s:%s%s", "host2": "%s:%s%s", "id": %q, "qid": %q, "dscp": %d, "dscp_koe": %v, "ptype": %d }`,
				state, diff, p.bandw_in,  p.bandw_out, *p.host1, *p.tpport1, v1, *p.host2, *p.tpport2, v2, *p.id, *p.qid, p.dscp, p.dscp_koe, PT_BANDWIDTH )

	return
}

/*
	Build a checkpoint string -- probably json, but it will contain everything including the user key.
	We still won't use the json package because that means making all of the fileds available to outside
	users.

	There is no path information saved in the checkpt. If a reload from ckpt is needed, then we assume
	that the network information was completely reset and the paths will be rebult using the host,
	commence, expiry and bandwidth information that was saved.

	If the pledge is expired, the string "expired" is returned which seems a bit better than just returning
	an empty string, or "{ }" which is meaningless.

	The kind value is a constant that allows the user to know what kind of pledge this is for easy reload
	without having to blindly unbundle the json into all possible pledge types to discover the type. The
	type _is_ put into the json for error checking internally.
*/
func (p *Pledge_bw) To_chkpt( ) ( chkpt string ) {

	if p.Is_expired( ) {			// will show expired if p is nil, so safe without check
		chkpt = "expired"
		return
	}
	
	commence, expiry := p.window.get_values()
	v1, v2 := p.bw_vlan2string( )

	chkpt = fmt.Sprintf( `{ "host1": "%s:%s%s", "host2": "%s:%s%s", "commence": %d, "expiry": %d, "bandwin": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q, "dscp": %d, "dscp_koe": %v, "ptype": %d }`, 
			*p.host1, *p.tpport1, v1, *p.host2, *p.tpport2, v2, commence, expiry, p.bandw_in, p.bandw_out, *p.id, *p.qid, *p.usrkey, p.dscp, p.dscp_koe, PT_BANDWIDTH )

	return
}


/*
DEPRECATED -- use switch p.(type)  or p, ok := x.(*Pledge_bw) instead
	Returns true if PT_BANDWIDTH passed in; false otherwise.
func (p *Pledge_bw) Is_ptype( kind int ) ( bool ) {
	return kind == PT_BANDWIDTH
}
*/

/*
	Sets the pushed flag to true.
*/
func (p *Pledge_bw) Set_pushed( ) {
	if p != nil {
		p.pushed = true
	}
}

/*
	Resets the pushed flag to false.
*/
func (p *Pledge_bw) Reset_pushed( ) {
	if p != nil {
		p.pushed = false
	}
}

/*
	Returns true if the pushed flag has been set to true.
*/
func (p *Pledge_bw) Is_pushed( ) (bool) {
	if p == nil {
		return false
	}

	return p.pushed
}

/*
	Returns true if the reservation is paused.
*/
func (p *Pledge_bw) Is_paused( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.paused
}

/*
	Returns true if the pledge has expired (the current time is greather than
	the expiry time in the pledge).
*/
func (p *Pledge_bw) Is_expired( ) ( bool ) {
	if p == nil {
		return true
	}

	return p.window.is_expired()
}

/*
	Returns true if the pledge has not become active (the commence time is >= the current time).
*/
func (p *Pledge_bw) Is_pending( ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_pending()
}

/*
	Returns true if the pledge is currently active (the commence time is <= than the current time
	and the expiry time is > the current time.
*/
func (p *Pledge_bw) Is_active( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active()
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds have passed.
*/
func (p *Pledge_bw) Is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active_soon( window )
}

/*
	Check the cookie passed in and return true if it matches the cookie on the
	pledge.
*/
func (p *Pledge_bw) Is_valid_cookie( c *string ) ( bool ) {
	return *c == *p.usrkey
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
*/
func (p *Pledge_bw) Concluded_recently( window int64 ) ( bool ) {
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
func (p *Pledge_bw) Is_extinct( window int64 ) ( bool ) {
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
func (p *Pledge_bw) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.commenced_recently( window )
}

/*
	Return the type of pledge; one of the PT_ constants.
func (p *Pledge_bw) Get_ptype( ) ( int ) {
	return PT_BANDWIDTH
}
*/

/*
	Returns a pointer to the ID string of the pledge.
*/
func (p *Pledge_bw) Get_id( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.id
}


