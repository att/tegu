// vi: sw=4 ts=4:

/*

	Mnemonic:	pledge_steer
	Abstract:	A steering pledge
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
				26 May 2015 - Broken out of pledge with conversion to interface
*/

package gizmos

import (
	"encoding/json"
	"fmt"
)

type Pledge_steer struct {
	host1		*string
	host2		*string
	protocol	*string		// tcp/udp:port (for steering)
	tpport1		*string		// transport port number or 0 if not defined
	tpport2		*string		// thee match h1/h2 respectively
	window		*pledge_window // the commence expiry window
	id			*string		// name that the client can use to manage (modify/delete)
	usrkey		*string		// a 'cookie' supplied by the user to prevent any other user from modifying
	pushed		bool		// set when pledge has been pushed into the openflow environment (skoogi)
	paused		bool		// set if reservation has been paused

	mbox_list	[]*Mbox		// list of middleboxes if the pledge is a steering pledge
	mbidx		int			// insertion point into mblist
	match_v6	bool		// true if we should force flow-mods to match on IPv6
}

/*
	A work struct used to decode a json string using Go's json package which requires things to
	be exported (boo). We need this to easily parse the json saved in the checkpoint file.
	We assume that host1/2 are saved _with_ trailing :port and thus we don't explicitly save/restore
	the tp port fields.  The conversion from checkpoint value to full struct will split them off.
*/
type Json_stpledge struct {
	Host1		*string
	Host2		*string
	Protocol	*string
	Commence	int64
	Expiry		int64
	Id			*string
	Usrkey		*string
	Ptype		int
	Mbox_list	[]*Mbox
	Match_v6	bool
}

// ---- private -------------------------------------------------------------------


// ---- public -------------------------------------------------------------------

/*
	Makes a steering pledge.
	Ep 1 and 2 are the endpoints with ep1 being the source if 'direction' is important. Endpoints are
	things like:
			host-name
			username/host-name		(user name is tenant name, tenant ID, squatter name or what ever the in vogue moniker is)

			host name may be one of:
			VM or host DNS name
			IP address
			E*						(all external -- beyond the gateway)
			L*						(all local)

	TODO: eventually steering needs to match on protocol.
*/
func Mk_steer_pledge( ep1 *string, ep2 *string, p1 *string, p2 *string, commence int64, expiry int64, id *string, usrkey *string, proto *string ) ( p *Pledge_steer, err error ) {
	err = nil
	p = nil

	window, err := mk_pledge_window( commence, expiry )
	if err != nil {
		return
	}
	
	p = &Pledge_steer{
		host1:		ep1,
		host2:		ep2,
		tpport1:	p1,
		tpport2:	p2,
		id:			id,
		protocol:	proto,
	}

	p.window = window

	if *usrkey != "" {
		p.usrkey = usrkey
	} else {
		p.usrkey = &empty_str
	}

	return
}

/*
	Create a clone of the pledge.  The path is NOT a copy, but just a reference to the list
	from the original.
*/
func (p *Pledge_steer) Clone( name string ) ( *Pledge_steer ) {
	newp := &Pledge_steer {
		host1:		p.host1,
		host2:		p.host2,
		tpport1: 	p.tpport1,
		tpport2: 	p.tpport2,
		id:			&name,
		usrkey:		p.usrkey,
		pushed:		p.pushed,
		paused:		p.paused,
	}

	newp.window = p.window.clone()
	return newp
}

/*
	Destruction
*/
func (p *Pledge_steer) Nuke( ) {
	p.host1 = nil
	p.host2 = nil
	p.id = nil
	p.usrkey = nil
}

/*
	Given a json string unpack it and put it into a pledge struct.
	We assume that the host names are name:port and split them apart
	as would be expected.
*/
func (p *Pledge_steer) From_json( jstr *string ) ( err error ){
	jp := new( Json_stpledge )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}

	if jp.Ptype != PT_STEERING {
		err = fmt.Errorf( "json did not contain a steering pledge" )
		return
	}

	p.host1, p.tpport1 = Split_port( jp.Host1 )		// suss apart host and port
	p.host2, p.tpport2 = Split_port( jp.Host2 )

	p.protocol = jp.Protocol
	p.window, err = mk_pledge_window( jp.Commence, jp.Expiry )
	p.id = jp.Id
	p.usrkey = jp.Usrkey

	p.protocol = jp.Protocol
	if p.protocol == nil {					// we don't tollerate nil ptrs
		p.protocol = &empty_str
	}

	return
}

/*
	Set match v6 flag based on user input.
*/
func (p *Pledge_steer) Set_matchv6( state bool ) {
	p.match_v6 = state
}

/*
	Sets a new expiry value on the pledge.
*/
func (p *Pledge_steer) Set_expiry ( v int64 ) {
	p.window.set_expiry_to( v )
	p.pushed = false						// force it to be resent to ajust times
}

// There is NOT a toggle pause on purpose; don't add one :)

/*
	Puts the pledge into paused state and optionally resets the pushed flag.
*/
func (p *Pledge_steer) Pause( reset bool ) {
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
func (p *Pledge_steer) Resume( reset bool ) {
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
func (p *Pledge_steer) Has_host( hname *string ) ( bool ) {
	return *p.host1 == *hname || *p.host2 == *hname
}

/*
	Return the number of middleboxes that are already inserted into the pledge.
*/
func (p *Pledge_steer) Get_mbidx( ) ( int ) {
	if p == nil {
		return 0
	}

	return p.mbidx
}

/*
	Add the middlebox reference to the pledge
*/
func (p *Pledge_steer) Add_mbox( mb *Mbox ) {
	if p == nil {
		return
	}

	if p.mbidx >= len( p.mbox_list ) {					// allocate more if out of space
		nmb := make( []*Mbox, p.mbidx + 10 )
		for i := 0; i < p.mbidx; i++ {
			nmb[i] = p.mbox_list[i]
		}
		p.mbox_list = nmb
	}
	
	p.mbox_list[p.mbidx] = mb
	p.mbidx++
}

/*
	Add a protocol reference to the pledge (e.g. tcp:80 or udp:4444)
*/
func (p *Pledge_steer) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.protocol = proto
}

/*
	Return the protocol associated with the pledge (e.g. tcp:80 or udp:4360).
*/
func (p *Pledge_steer) Get_proto( ) ( *string ) {
	return p.protocol
}


/*
	Return the mbox at index n, or nil if out of bounds.
*/
func (p *Pledge_steer) Get_mbox( n int ) ( *Mbox ) {
	if n < 0 || n >= p.mbidx {
		return nil
	}

	return p.mbox_list[n]
}

/*
	Return mbox count.
*/
func (p *Pledge_steer) Get_mbox_count( ) ( int ) {
	return p.mbidx
}
// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
	NEVER put the usrkey into the string!
*/
func (p *Pledge_steer) To_str( ) ( s string ) {
	return p.String( )
}

func (p *Pledge_steer) String( ) ( s string ) {

	state, caption, diff := p.window.state_str()
	commence, expiry := p.window.get_values()

	s = fmt.Sprintf( "%s: togo=%ds %s h1=%s:%d h2=%s:%d id=%s st=%d ex=%d push=%v ptype=steering", state, diff, caption,
			*p.host1, p.tpport2, *p.host2, p.tpport2, *p.id, commence, expiry,  p.pushed )
	return
}

/*
	Generate a json representation of a pledge. We do NOT use the json package because we
	don't put the object directly in; we render useful information, which excludes some of
	the raw data, and we don't want to have to expose the fields publicly that do go into
	the json output.
*/
func (p *Pledge_steer) To_json( ) ( json string ) {
	var (
		state string
		diff int64 = 0
	)

	state, _, diff = p.window.state_str()
	
	proto := ""
	if p.protocol != nil {
		proto = *p.protocol
	}
	json = fmt.Sprintf( `{ "state": %q, "time": %d, "host1": "%s:%s", "host2": "%s:%s", "protocol": %q, "id": %q, "ptype": %d, "mbox_list": [ `, 
			state, diff, *p.host1, *p.tpport1, *p.host2, *p.tpport2, proto, *p.id, PT_STEERING )

	sep := ""
	for i := 0; i < p.mbidx; i++ {
		json += fmt.Sprintf( `%s%q`, sep, *p.mbox_list[i].Get_id() )
		sep = ","			
	}
	json += " ] }"

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
func (p *Pledge_steer) To_chkpt( ) ( chkpt string ) {

	if p.window.is_expired() {
		chkpt = "expired"
		return
	}
	
	c, e := p.window.get_values()

	proto := ""
	if p.protocol != nil {
		proto = *p.protocol
	}
	chkpt = fmt.Sprintf( `{ "host1": "%s:%s", "host2": "%s:%s", "protocol": %q, "commence": %d, "expiry": %d, "id": %q, "usrkey": %q, "ptype": %d, "mbox_list": [ `, 
			*p.host1, *p.tpport1, *p.host2, *p.tpport2, proto, c, e, *p.id,  *p.usrkey, PT_STEERING )

	sep := ""
	for i := 0; i < p.mbidx; i++ {
		chkpt += fmt.Sprintf( `%s %s`, sep, *p.mbox_list[i].To_json() )
		sep = ","			
	}
	chkpt += " ] }"

	return
}

/*
	Sets the pushed flag to true.
*/
func (p *Pledge_steer) Set_pushed( ) {
	if p != nil {
		p.pushed = true
	}
}

/*
	Resets the pushed flag to false.
*/
func (p *Pledge_steer) Reset_pushed( ) {
	if p != nil {
		p.pushed = false
	}
}

/*
	Returns true if the pushed flag has been set to true.
*/
func (p *Pledge_steer) Is_pushed( ) (bool) {
	if p == nil {
		return false
	}

	return p.pushed
}

/*
	Returns true if the reservation is paused.
*/
func (p *Pledge_steer) Is_paused( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.paused
}

/*
	Returns true if PT_BANDWIDTH passed in; false otherwise.
*/
func (p *Pledge_steer) Is_ptype( kind int ) ( bool ) {
	return kind == PT_STEERING
}

/*
	Returns true if the pledge has expired (the current time is greather than
	the expiry time in the pledge).
*/
func (p *Pledge_steer) Is_expired( ) ( bool ) {
	if p == nil {
		return true
	}

	return p.window.is_expired()
}

/*
	Returns true if the pledge has not become active (the commence time is >= the current time).
*/
func (p *Pledge_steer) Is_pending( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_pending()
}

/*
	Returns true if the pledge is currently active (the commence time is <= than the current time
	and the expiry time is > the current time.
*/
func (p *Pledge_steer) Is_active( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active()
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds have passed.
*/
func (p *Pledge_steer) Is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.is_active_soon( window )
}

/*
	Check the cookie passed in and return true if it matches the cookie on the
	pledge.
*/
func (p *Pledge_steer) Is_valid_cookie( c *string ) ( bool ) {
	//fmt.Fprintf( os.Stderr, "pledge:>>>> checking: %s == %s  %v\n", *c, *p.usrkey, bool( *c == *p.usrkey) )
	return *c == *p.usrkey
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
*/
func (p *Pledge_steer) Concluded_recently( window int64 ) ( bool ) {
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
func (p *Pledge_steer) Is_extinct( window int64 ) ( bool ) {
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
func (p *Pledge_steer) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	return p.window.commenced_recently( window )
}

/*
	Return the type of pledge; one of the PT_ constants.
func (p *Pledge_steer) Get_ptype( ) ( int ) {
	return PT_STEERING
}
*/

/*
	Return whether the match on IPv6 flag is true
*/
func (p *Pledge_steer) Get_matchv6() ( bool ) {
	return p.match_v6
}

/*
	Returns a pointer to the ID string of the pledge.
*/
func (p *Pledge_steer) Get_id( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.id
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge_steer) Get_hosts( ) ( *string, *string ) {
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
		the expiry time

		bwin/out are always returned as 0, but are given so that there is a consitent 
		interface for network values.
*/
func (p *Pledge_steer) Get_values( ) ( h1 *string, h2 *string, p1 *string, p2 *string, commence int64, expiry int64, bw_in int64, bw_out int64 ) {
	if p == nil {
		return &empty_str, &empty_str, &empty_str, &empty_str, 0, 0, 0, 0
	}

	c, e := p.window.get_values()
	return p.host1, p.host2, p.tpport1, p.tpport2, c, e, 0, 0
}

/*
	Return the commence and expiry times.
*/
func (p *Pledge_steer) Get_window( ) ( commmence int64, expiry int64 ) {
	return p.window.get_values( )
}
