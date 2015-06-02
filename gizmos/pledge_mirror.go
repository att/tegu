// vi: sw=4 ts=4:

/*

	Mnemonic:	pledge_mirror
	Abstract:	A pledge for a mirror.
				Now that a pledge has been converted to an interface, this needs to be 

	Date:		17 Feb 2015
	Author:		Bob Eby

	Mods:		
				17 Feb 2015 - Added mirroring
				26 May 2015 - Broken out of main pledge to allow for pledge to become an interface.
				01 Jun 2015 - Addded equal() support
*/

package gizmos

import (
	"encoding/json"
	"fmt"
	"strings"
)

// needs rework to rename fields that make sense to mirroring
type Pledge_mirror struct {
	host1		*string		// list of ports to mirror
	host2		*string		// destination of mirrors
	//protocol	*string		//
	tpport1		*string		//
	tpport2		*string		// these match h1/h2 respectively
	window		*pledge_window
	//bandw_in	int64		// bandwidth to reserve inbound to host1
	//bandw_out	int64		// bandwidth to reserve outbound from host1
	//dscp		int			// dscp value that should be propigated
	//dscp_koe	bool		// true if the dscp value should be kept when a packet exits the environment
	id			*string		// name that the client can use to manage (modify/delete)
	qid			*string		// physical host
	usrkey		*string		// a 'cookie' supplied by the user to prevent any other user from modifying
	//path_list	[]*Path		// list of paths that represent the bandwith and can be used to send flowmods etc.
	pushed		bool		// set when pledge has been pushed into the openflow environment (skoogi)
	paused		bool		// set if reservation has been paused

	//mbox_list	[]*Mbox		// list of middleboxes if the pledge is a steering pledge
	//mbidx		int			// insertion point into mblist
	match_v6	bool		// true if we should force flow-mods to match on IPv6
}

/*
	A work struct used to decode a json string using Go's json package which requires things to
	be exported (boo). We need this to easily parse the json saved in the checkpoint file.
	We assume that host1/2 are saved _with_ trailing :port and thus we don't explicitly save/restore
	the tp port fields.  The conversion from checkpoint value to full struct will split them off.
*/
type Json_pledge struct {
	Host1		*string
	Host2		*string
	Protocol	*string
	Commence	int64
	Expiry		int64
	Bandwin		int64
	Bandwout	int64
	//Dscp		int
	//Dscp_koe	bool
	Id			*string
	Qid			*string
	Usrkey		*string
	Ptype		int
	//Mbox_list	[]*Mbox
	Match_v6	bool
}

// ---- private -------------------------------------------------------------------

// ---- public -------------------------------------------------------------------

/*
 *	Makes a mirroring pledge. 
	
 */
func Mk_mirror_pledge( in_ports []string, out_port *string, commence int64, expiry int64, id *string, usrkey *string, phost *string, vlan *string ) ( p Pledge, err error ) {
	err = nil

	window, err := mk_pledge_window( commence, expiry )		// will adjust commence forward to now if needed, returns nil if expiry has past
	if err != nil {
		return
	}

	t := strings.Join(in_ports, " ")
	if vlan != nil && *vlan != "" {
		// Since we have to cram this in the pre-existing Pledge struct,
		// just glom it on the end of the port list
		// 2015/05/26... Now can redo it to make more sense.
		t = t + " vlan:" + *vlan
	}
	pm := &Pledge_mirror {
		host1:		&t,				// mirror input ports (space sep)
		host2:		out_port,		// mirror output port
		id:			id,				// mirror name
		qid:		phost,			// physical host (overloaded field)
		usrkey:		usrkey,			// user "cookie"
		window:		window,
	}

	if *usrkey == "" {
		pm.usrkey = &empty_str
	}

	p = pm
	return
}

/*
	Create a clone of the pledge.  The path is NOT a copy, but just a reference to the list
	from the original.
*/
func (p *Pledge_mirror) Clone( name string ) ( Pledge ) {
	newp := &Pledge_mirror {
		host1:		p.host1,
		host2:		p.host2,
		//tpport1: 	p.tpport1,
		//tpport2: 	p.tpport2,
		//bandw_in:	p.bandw_in,
		//bandw_out:	p.bandw_out,
		//dscp:		p.dscp,
		id:			&name,
		usrkey:		p.usrkey,
		qid:		p.qid,
		//path_list:	p.path_list,
		pushed:		p.pushed,
		paused:		p.paused,
	}

	newp.window = p.window.clone()
	return newp
}

/*
	Destruction
*/
func (p *Pledge_mirror) Nuke( ) {
	p.host1 = nil
	p.host2 = nil
	p.id = nil
	p.qid = nil
	p.usrkey = nil
}

/*
	Given a json string unpack it and put it into a pledge struct.
*/
func (p *Pledge_mirror) From_json( jstr *string ) ( err error ){
	jp := new( Json_pledge )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}
	
	if jp.Ptype != PT_MIRRORING {
		err = fmt.Errorf( "json was not for a mirror pledge" )
		return
	}

	p.host1, p.tpport1 = Split_port( jp.Host1 )		// suss apart host and port
	p.host2, p.tpport2 = Split_port( jp.Host2 )

	p.window, _ = mk_pledge_window( jp.Commence, jp.Expiry )
	//p.protocol = jp.Protocol
	//p.id = jp.Id
	//p.dscp_koe = jp.Dscp_koe
	p.usrkey = jp.Usrkey
	p.qid = jp.Qid
	//p.bandw_out = jp.Bandwout
	//p.bandw_in = jp.Bandwin

	return
}

/*
	Associates a queue ID with the pledge.
func (p *Pledge_mirror) Set_qid( id *string ) {
	p.qid = id
}
*/

/*
	Set match v6 flag based on user input.
*/
func (p *Pledge_mirror) Set_matchv6( state bool ) {
	p.match_v6 = state
}

/*
	Sets a new expiry value on the pledge.
*/
func (p *Pledge_mirror) Set_expiry( v int64 ) {
	p.window.set_expiry_to( v )
	p.pushed = false		// force it to be resent to ajust times
}

// There is NOT a toggle pause on purpose; don't add one :)

/*
	Puts the pledge into paused state and optionally resets the pushed flag.
*/
func (p *Pledge_mirror) Pause( reset bool ) {
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
func (p *Pledge_mirror) Resume( reset bool ) {
	if p != nil {
		p.paused = false
		if reset {
			p.pushed = false;
		}
	}
}

/*
	Accepts a physical host name and returns true if it matches either of the names associated with
	this pledge.
*/
func (p *Pledge_mirror) Has_host( hname *string ) ( bool ) {
	return *p.qid == *hname
}

/*
	must implement dummy for interface
func (p *Pledge_mirror) Set_path_list( pl []*Path ) {
	return
}
*/


/*
	Add a protocol reference to the pledge (e.g. tcp:80 or udp:4444)
func (p *Pledge_mirror) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.protocol = proto
}
*/

/*
	Return the protocol associated with the pledge.
func (p *Pledge_mirror) Get_proto( ) ( *string ) {
	return p.protocol
}
*/


// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
*/
func (p *Pledge_mirror) To_str( ) ( s string ) {
	return p.String()
}

func (p *Pledge_mirror) String( ) ( s string ) {

	state, caption, diff := p.window.state_str( )
	c, e := p.window.get_values( )

	//NEVER put the usrkey into the string!
	s = fmt.Sprintf( "%s: togo=%ds %s ports=%s output=%s id=%s st=%d ex=%d push=%v ptype=mirroring", state, diff, caption,
		*p.host1, *p.host2, *p.id, c, e, p.pushed )

	return
}

/*
	Generate a json representation of a pledge. We do NOT use the json package because we
	don't put the object directly in; we render useful information, which excludes some of
	the raw data, and we don't want to have to expose the fields publicly that do go into
	the json output.
*/
func (p *Pledge_mirror) To_json( ) ( json string ) {
	
	state, _, diff := p.window.state_str( )

	json = fmt.Sprintf( `{ "state": %q, "time": %d, "host1": "%s", "host2": "%s", "id": %q, "ptype": %d }`,
		state, diff, *p.host1, *p.host2, *p.id, PT_MIRRORING )

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
*/
func (p *Pledge_mirror) To_chkpt( ) ( chkpt string ) {

	if p.window.is_expired( ) {			// will show expired if window is nill, so safe without check
		chkpt = "expired"
		return
	}
	
	c, e := p.window.get_values( )
	
	chkpt = fmt.Sprintf( `{ "host1": "%s", "host2": "%s", "commence": %d, "expiry": %d, "id": %q, "usrkey": %q, "ptype": %d }`,
		*p.host1, *p.host2, c, e, *p.id, *p.usrkey, PT_MIRRORING )

	return
}

/*
	Sets the pushed flag to true.
*/
func (p *Pledge_mirror) Set_pushed( ) {
	if p != nil {
		p.pushed = true
	}
}

/*
	Resets the pushed flag to false.
*/
func (p *Pledge_mirror) Reset_pushed( ) {
	if p != nil {
		p.pushed = false
	}
}

/*
	Returns true if the pushed flag has been set to true.
*/
func (p *Pledge_mirror) Is_pushed( ) (bool) {
	if p == nil {
		return false
	}

	return p.pushed
}

/*
	Returns true if the reservation is paused.
*/
func (p *Pledge_mirror) Is_paused( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.paused
}

/*
	Returns true if kind is PT_MIRRORING, false otherwise
*/
func (p *Pledge_mirror) Is_ptype( kind int ) ( bool ) {
	return kind == PT_MIRRORING
}

/*
	Returns true if the pledge has expired (the current time is greather than
	the expiry time in the pledge).
*/
func (p *Pledge_mirror) Is_expired( ) ( bool ) {
	if p == nil {
		return true
	}

	return p.window.is_expired( )
}

/*
	Returns true if the pledge has not become active (the commence time is >= the current time).
*/
func (p *Pledge_mirror) Is_pending( ) ( bool ) {
	if p == nil {
		return false
	}
	return p.window.is_pending( )
}

/*
	Returns true if the pledge is currently active (the commence time is <= than the current time
	and the expiry time is > the current time.
*/
func (p *Pledge_mirror) Is_active( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active()
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds have passed.
*/
func (p *Pledge_mirror) Is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active_soon( window )
}

/*
	Check the cookie passed in and return true if it matches the cookie on the
	pledge.
*/
func (p *Pledge_mirror) Is_valid_cookie( c *string ) ( bool ) {
	//fmt.Fprintf( os.Stderr, "pledge:>>>> checking: %s == %s  %v\n", *c, *p.usrkey, bool( *c == *p.usrkey) )
	return *c == *p.usrkey
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
*/
func (p *Pledge_mirror) Concluded_recently( window int64 ) ( bool ) {
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
func (p *Pledge_mirror) Is_extinct( window int64 ) ( bool ) {
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
func (p *Pledge_mirror) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.commenced_recently( window )
}

/*
	Return the type of pledge; one of the PT_ constants.
func (p *Pledge_mirror) Get_ptype( ) ( int ) {
	return PT_MIRRORING
}
*/

/*
	Return whether the match on IPv6 flag is true
*/
func (p *Pledge_mirror) Get_matchv6() ( bool ) {
	return p.match_v6
}

/*
	Returns a pointer to the ID string of the pledge.
*/
func (p *Pledge_mirror) Get_id( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.id
}

/*
	Returns a pointer to the queue ID
*/
func (p *Pledge_mirror) Get_qid( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.qid
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge_mirror) Get_hosts( ) ( *string, *string ) {
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
		Bandwidth values (always 0, but written for parm consistency)
*/
func (p *Pledge_mirror) Get_values( ) ( h1 *string, h2 *string, p1 *string, p2 *string, commence int64, expiry int64, bw_in int64, bw_out int64 ) {
	if p == nil {
		return &empty_str, &empty_str, &empty_str, &empty_str, 0, 0, 0, 0
	}

	c, e := p.window.get_values( )
	return p.host1, p.host2, p.tpport1, p.tpport2, c, e, 0, 0
}

/*
	Return the commence and expiry times.
*/
func (p *Pledge_mirror) Get_window( ) ( int64, int64 ) {
	return p.window.get_values( )
}

/*
	Return true if the pledge passed in duplicates this pledge.
*/
func (p *Pledge_mirror) Equals( p2 *Pledge ) ( bool ) {
	
	if p == nil {
		return false
	}

	p2m, ok := (*p2).( *Pledge_mirror )			// convert from generic type to specific
	if ok {
		if ! Strings_equal( p.host1, p2m.host1 ) { return false }
		if ! Strings_equal( p.host2, p2m.host2 ) { return false }
		if ! Strings_equal( p.qid, p2m.qid ) { return false }

		if !p.window.overlaps( p2m.window ) {
			return false;
		}

		return true
	}

	return false
}
