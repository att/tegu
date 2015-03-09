// vi: sw=4 ts=4:

/*

	Mnemonic:	pledge
	Abstract:	"object" that manages a pledge (reservation).
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
*/

package gizmos

import (
	//"bufio"
	"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	//"os"
	"strings"
	"time"

	//"codecloud.web.att.com/gopkgs/clike"
)

type Pledge struct {
	host1		*string
	host2		*string
	protocol	*string		// tcp/udp:port (for steering)
	tpport1		*string		// transport port number or 0 if not defined
	tpport2		*string		// thee match h1/h2 respectively
	commence	int64
	expiry		int64
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

	mbox_list	[]*Mbox		// list of middleboxes if the pledge is a steering pledge
	mbidx		int			// insertion point into mblist
	ptype		int			// pledge type from gizmos PT_ constants.
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
	Dscp		int
	Dscp_koe	bool
	Id			*string
	Qid			*string
	Usrkey		*string
	Ptype		int
	Mbox_list	[]*Mbox
}

// ---- private -------------------------------------------------------------------
/*
	Adjust window. Returns a valid commence time (if earlier than now) or 0 if the
	time window is not valid.
*/
func adjust_window( commence int64, conclude int64 ) ( adj_start int64, err error ) {

	now := time.Now().Unix()
	err = nil

	if commence < now {				// ajust forward to better play with windows on the paths
		adj_start = now
	} else {
		adj_start = commence
	}

	if conclude <= adj_start {						// bug #156 fix
		err = fmt.Errorf( "bad expiry submitted, already expired: now=%d expiry=%d", now, conclude );
		obj_sheep.Baa( 2, "pledge: %s", err )
		return
	}

	return
}

// ---- public -------------------------------------------------------------------

/*
	Constructor; creates a pledge.
	Creates a pledge of bandwidth between two hosts, allowing host2 to be nil which indicates that the
	pledge exists between host1 and any other host. If commence is 0, then the current time (now) is used.

	A nil pointer is returned if the expiry time is in the past and the commence time is adjusted forward
	(to the current time) if it is less than the current time.
*/
func Mk_pledge( host1 *string, host2 *string, p1 *string, p2 *string, commence int64, expiry int64, bandw_in int64, bandw_out int64, id *string, usrkey *string, dscp int, dscp_koe bool ) ( p *Pledge, err error ) {
	err = nil
	p = nil

	commence, err = adjust_window( commence, expiry )
	if err != nil {
		return
	}
	
	if *host2 == "" || *host2 == "any" {			// no longer allowed
		p = nil;
		err = fmt.Errorf( "bad host2 name submitted: %s", *host2 )
		obj_sheep.Baa( 1, "pledge: %s", err )
		return

		//any_str := "any"
		//p.host2 = &any_str
	}

	p = &Pledge{
		host1: host1,
		host2: host2,
		tpport1: p1,
		tpport2: p2,
		commence: commence,
		expiry: expiry,
		bandw_in:	bandw_in,
		bandw_out:	bandw_out,
		id: id,
		dscp: dscp,
		ptype:	PT_BANDWIDTH,
		protocol:	&empty_str,
		dscp_koe: dscp_koe,
	}

	if *usrkey != "" {
		p.usrkey = usrkey
	} else {
		p.usrkey = &empty_str
	}

	return
}

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
func Mk_steer_pledge( ep1 *string, ep2 *string, p1 *string, p2 *string, commence int64, expiry int64, id *string, usrkey *string, proto *string ) ( p *Pledge, err error ) {
	err = nil
	p = nil

	commence, err = adjust_window( commence, expiry )
	if err != nil {
		return
	}
	
	p = &Pledge{
		host1:		ep1,
		host2:		ep2,
		tpport1:	p1,
		tpport2:	p2,
		commence:	commence,
		expiry:		expiry,
		id:			id,
		ptype:		PT_STEERING,
		protocol:	proto,
	}

	s := "none"
	p.qid = &s

	if *usrkey != "" {
		p.usrkey = usrkey
	} else {
		p.usrkey = &empty_str
	}

	return
}

/*
 *	Makes a mirroring pledge.  Note: I think Pledge should be an interface and not a struct.
 *  This would allow these 3 types of Pledges to be implemented as needed, rather than all
 *  having to share the same structure.
 */
func Mk_mirror_pledge( in_ports []string, out_port *string, commence int64, expiry int64, id *string, usrkey *string, phost *string, vlan *string ) ( p *Pledge, err error ) {
	err = nil
	p = nil

	commence, err = adjust_window( commence, expiry )
	if err != nil {
		return
	}

	t := strings.Join(in_ports, " ")
	if vlan != nil && *vlan != "" {
		// Since we have to cram this in the pre-existing Pledge struct,
		// just glom it on the end of the port list
		t = t + " vlan:" + *vlan
	}
	p = &Pledge{
		host1:		&t,				// mirror input ports (space sep)
		host2:		out_port,		// mirror output port
		commence:	commence,		// mirror start time
		expiry:		expiry,			// mirror end time
		id:			id,				// mirror name
		qid:		phost,			// physical host (overloaded field)
		usrkey:		usrkey,			// user "cookie"
		ptype:		PT_MIRRORING,	// type of pledge
	}
	if *usrkey == "" {
		p.usrkey = &empty_str
	}
	return
}

/*
	Create a clone of the pledge.  The path is NOT a copy, but just a reference to the list
	from the original.
*/
func (p *Pledge) Clone( name string ) ( newp *Pledge ) {
	newp = &Pledge {
		host1:		p.host1,
		host2:		p.host2,
		tpport1: 	p.tpport1,
		tpport2: 	p.tpport2,
		commence:	p.commence,
		expiry:		p.expiry,
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

	return newp
}

/*
	Destruction
*/
func (p *Pledge) Nuke( ) {
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
func (p *Pledge) From_json( jstr *string ) ( err error ){
	jp := new( Json_pledge )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err != nil {
		return
	}

	tokens := strings.Split( *jp.Host1, ":" )
	p.host1 = &tokens[0]
	if len( tokens ) > 1 {
		//p.tpport1 = clike.Atoi( tokens[1] )
		p.tpport1 =  &tokens[1]
	} else {
		dup_str := "0"
		p.tpport1 = &dup_str
		//p.tpport1 = 0
	}

	tokens = strings.Split( *jp.Host2, ":" )
	p.host2 = &tokens[0]
	if len( tokens ) > 1 {
		//p.tpport2 = clike.Atoi( tokens[1] )
		p.tpport2 = &tokens[1]
	} else {
		//p.tpport2 = 0
		dup_str := "0"
		p.tpport2 = &dup_str
	}

	p.protocol = jp.Protocol
	p.commence = jp.Commence
	p.expiry = jp.Expiry
	p.id = jp.Id
	p.dscp = jp.Dscp
	p.dscp_koe = jp.Dscp_koe
	p.usrkey = jp.Usrkey
	p.qid = jp.Qid
	p.bandw_out = jp.Bandwout
	p.bandw_in = jp.Bandwin
	p.ptype = jp.Ptype

	p.protocol = jp.Protocol
	if p.protocol == nil {					// we don't tollerate nil ptrs
		p.protocol = &empty_str
	}

	return
}

/*
	Associates a queue ID with the pledge.
*/
func (p *Pledge) Set_qid( id *string ) {
	p.qid = id
}

/*
	Associates a path list with the pledge.
*/
func (p *Pledge) Set_path_list( pl []*Path ) {
	p.path_list = pl
}

/*
	Sets a new expiry value on the pledge.
*/
func (p *Pledge) Set_expiry ( v int64 ) {
	p.expiry = v
	p.pushed = false		// force it to be resent to ajust times
}

// There is NOT a toggle pause on purpose; don't add one :)

/*
	Puts the pledge into paused state and optionally resets the pushed flag.
*/
func (p *Pledge) Pause( reset bool ) {
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
func (p *Pledge) Resume( reset bool ) {
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
func (p *Pledge) Has_host( hname *string ) ( bool ) {
	return *p.host1 == *hname || *p.host2 == *hname
}

/*
	Return the number of middleboxes that are already inserted into the pledge.
*/
func (p *Pledge) Get_mbidx( ) ( int ) {
	if p == nil {
		return 0
	}

	return p.mbidx
}

/*
	Add the middlebox reference to the pledge
*/
func (p *Pledge) Add_mbox( mb *Mbox ) {
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
func (p *Pledge) Add_proto( proto *string ) {
	if p == nil {
		return
	}

	p.protocol = proto
}

/*
	Return the protocol associated with the pledge.
*/
func (p *Pledge) Get_proto( ) ( *string ) {
	return p.protocol
}


/*
	Return the mbox at index n, or nil if out of bounds.
*/
func (p *Pledge) Get_mbox( n int ) ( *Mbox ) {
	if n < 0 || n >= p.mbidx {
		return nil
	}

	return p.mbox_list[n]
}

/*
	Return mbox count.
*/
func (p *Pledge) Get_mbox_count( ) ( int ) {
	return p.mbidx
}
// --------- humanisation or export functions --------------------------------------------------------

/*
	return a nice string from the data.
	NEVER put the usrkey into the string!
*/
func (p *Pledge) To_str( ) ( s string ) {
	var (
		now 	int64;
		state 	string
		caption string
		diff	int64
	)

	now = time.Now().Unix()
	if now > p.expiry {
		state = "EXPIRED"
		caption = "ago"
		diff = now - p.expiry
	} else {
		if now < p.commence {
			state = "PENDING"
			caption = "from now"
			diff = p.commence - now
		} else {
			state = "ACTIVE"
			caption = "remaining"
			diff = p.expiry -  now
		}
	}

	if p.ptype == PT_MIRRORING {
		s = fmt.Sprintf( "%s: togo=%ds %s ports=%s output=%s id=%s st=%d ex=%d push=%v ptype=%d", state, diff, caption,
			*p.host1, *p.host2, *p.id, p.commence, p.expiry, p.pushed, p.ptype )
	} else {
		s = fmt.Sprintf( "%s: togo=%ds %s h1=%s:%d h2=%s:%d id=%s qid=%s st=%d ex=%d bwi=%d bwo=%d push=%v dscp=%d ptype=%d koe=%v", state, diff, caption,
			*p.host1, p.tpport2, *p.host2, p.tpport2, *p.id, *p.qid, p.commence, p.expiry, p.bandw_in, p.bandw_out, p.pushed, p.dscp, p.ptype, p.dscp_koe )
	}
	return
}

/*
	Generate a json representation of a pledge. We do NOT use the json package because we
	don't put the object directly in; we render useful information, which excludes some of
	the raw data, and we don't want to have to expose the fields publicly that do go into
	the json output.
*/
func (p *Pledge) To_json( ) ( json string ) {
	var (
		now int64
		state string
		diff int64 = 0
	)

	now = time.Now().Unix()
	if now >= p.expiry {
		state = "EXPIRED"
	} else {
		if now < p.commence {
			state = "PENDING"
			diff = p.commence - now
		} else {
			state = "ACTIVE"
			diff = p.expiry -  now
		}
	}
	
	switch p.ptype {
		case PT_BANDWIDTH:
				json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwin": %d, "bandwout": %d, "host1": "%s:%s", "host2": "%s:%s", "id": %q, "qid": %q, "dscp": %d, "dscp_koe": %v, "ptype": "bandwidth" }`,
					state, diff, p.bandw_in,  p.bandw_out, *p.host1, *p.tpport1, *p.host2, *p.tpport2, *p.id, *p.qid, p.dscp, p.dscp_koe )

		case PT_STEERING:
				if p.protocol != nil {
					json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwin": %d, "bandwout": %d, "host1": "%s:%s", "host2": "%s:%s", "protocol": %q, "id": %q, "qid": %q, "ptype": "steering", "mbox_list": [ `,
							state, diff, p.bandw_in, p.bandw_out, *p.host1, p.tpport1, *p.host2, p.tpport2, *p.protocol, *p.id, *p.qid )
				} else {
					json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwin": %d, "bandwout": %d, "host1": "%s:%s", "host2": "%s:%s", "id": %q, "qid": %q, "ptype": "steering", "mbox_list": [ `,
							state, diff, p.bandw_in, p.bandw_out, *p.host1, p.tpport1, *p.host2, p.tpport2, *p.id, *p.qid )
				}
				sep := ""
				for i := 0; i < p.mbidx; i++ {
					json += fmt.Sprintf( `%s%q`, sep, *p.mbox_list[i].Get_id() )
					sep = ","			
				}
				json += " ] }"

		case PT_MIRRORING:
				json = fmt.Sprintf( `{ "state": %q, "time": %d, "host1": "%s", "host2": "%s", "id": %q, "ptype": "mirroring" }`,
					state, diff, *p.host1, *p.host2, *p.id )
	}

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
func (p *Pledge) To_chkpt( ) ( chkpt string ) {

	if p.Is_expired( ) {			// will show expired if p is nill, so safe without check
		chkpt = "expired"
		return
	}
	
	switch p.ptype {
		case PT_BANDWIDTH:
				chkpt = fmt.Sprintf( `{ "host1": "%s:%s", "host2": "%s:%s", "commence": %d, "expiry": %d, "bandwin": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q, "dscp": %d, "dscp_koe": %v, "ptype": %d }`,
						*p.host1, p.tpport1, *p.host2, p.tpport2, p.commence, p.expiry, p.bandw_in, p.bandw_out, *p.id, *p.qid, *p.usrkey, p.dscp, p.dscp_koe, p.ptype )

		case PT_STEERING:
				if p.protocol != nil {
					chkpt = fmt.Sprintf( `{ "host1": "%s:%d", "host2": "%s:%d", "protocol": %q, "commence": %d, "expiry": %d, "bandwin": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q, "ptype": %d, "mbox_list": [ `,
						*p.host1, p.tpport1, *p.host2, p.tpport2, *p.protocol, p.commence, p.expiry, p.bandw_in, p.bandw_out, *p.id, *p.qid, *p.usrkey, p.ptype )
				} else {
					chkpt = fmt.Sprintf( `{ "host1": "%s:%d", "host2": "%s:%d", "commence": %d, "expiry": %d, "bandwin": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q, "ptype": %d, "mbox_list": [ `,
						*p.host1, p.tpport1, *p.host2, p.tpport2,  p.commence, p.expiry, p.bandw_in, p.bandw_out, *p.id, *p.qid, *p.usrkey, p.ptype )
				}
				sep := ""
				for i := 0; i < p.mbidx; i++ {
					chkpt += fmt.Sprintf( `%s %s`, sep, *p.mbox_list[i].To_json() )
					sep = ","			
				}
				chkpt += " ] }"

		case PT_MIRRORING:
				chkpt = fmt.Sprintf( `{ "host1": "%s", "host2": "%s", "commence": %d, "expiry": %d, "id": %q, "usrkey": %q, "ptype": "mirroring" }`,
					*p.host1, *p.host2, p.commence, p.expiry, *p.id, *p.usrkey )
	}

	return
}

/*
	Sets the pushed flag to true.
*/
func (p *Pledge) Set_pushed( ) {
	if p != nil {
		p.pushed = true
	}
}

/*
	Resets the pushed flag to false.
*/
func (p *Pledge) Reset_pushed( ) {
	if p != nil {
		p.pushed = false
	}
}

/*
	Returns true if the pushed flag has been set to true.
*/
func (p *Pledge) Is_pushed( ) (bool) {
	if p == nil {
		return false
	}

	return p.pushed
}

/*
	Returns true if the reservation is paused.
*/
func (p *Pledge) Is_paused( ) ( bool ) {
	if p == nil {
		return false
	}

	return p.paused
}

/*
	Returns true if type is steering.
*/
func (p *Pledge) Is_steering( ) ( bool ) {
	return p.ptype == PT_STEERING
}

/*
	Returns true if type is bandwidth.
*/
func (p *Pledge) Is_bandwidth( ) ( bool ) {
	return p.ptype == PT_BANDWIDTH
}

/*
	Returns true if type is mirroring.
*/
func (p *Pledge) Is_mirroring( ) ( bool ) {
	return p.ptype == PT_MIRRORING
}

/*
	Returns true if the pledge has expired (the current time is greather than
	the expiry time in the pledge).
*/
func (p *Pledge) Is_expired( ) ( bool ) {
	if p == nil {
		return true
	}

	return time.Now().Unix( ) >= p.expiry
}

/*
	Returns true if the pledge has not become active (the commence time is >= the current time).
*/
func (p *Pledge) Is_pending( ) ( bool ) {
	if p == nil {
		return false
	}
	return time.Now().Unix( ) < p.commence
}

/*
	Returns true if the pledge is currently active (the commence time is <= than the current time
	and the expiry time is > the current time.
*/
func (p *Pledge) Is_active( ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return p.commence < now  && p.expiry > now
}

/*
	Returns true if pledge is active now, or will be active before elapsed seconds have passed.
*/
func (p *Pledge) Is_active_soon( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return (p.commence >= now) && p.commence <= (now + window)
}

/*
	Check the cookie passed in and return true if it matches the cookie on the
	pledge.
*/
func (p *Pledge) Is_valid_cookie( c *string ) ( bool ) {
	//fmt.Fprintf( os.Stderr, "pledge:>>>> checking: %s == %s  %v\n", *c, *p.usrkey, bool( *c == *p.usrkey) )
	return *c == *p.usrkey
}

/*
	Returns true if pledge concluded between (now - window) and now-1.
*/
func (p *Pledge) Concluded_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return (p.expiry < now)  && (p.expiry >= now - window)
}

/*
	Returns true if pledge expired long enough ago that it can safely be discarded.
	The window is the number of seconds that the pledge must have been expired to
	be considered extinct.
*/
func (p *Pledge) Is_extinct( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return p.expiry <= now - window
}

/*
	Returns true if pledge started recently (between now and now - window seconds) and
	has not expired yet. If the pledge started within the window, but expired before
	the call to this function false is returned.
*/
func (p *Pledge) Commenced_recently( window int64 ) ( bool ) {
	if p == nil {
		return false
	}

	now := time.Now().Unix()
	return (p.commence >= (now - window)) && (p.commence <= now ) && (p.expiry > now)
}

/*
	Return the type of pledge; one of the PT_ constants.
*/
func (p *Pledge) Get_ptype( ) ( int ) {
	return p.ptype
}

/*
	Returns a pointer to the ID string of the pledge.
*/
func (p *Pledge) Get_id( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.id
}

/*
	Returns a pointer to the queue ID
*/
func (p *Pledge) Get_qid( ) ( *string ) {
	if p == nil {
		return nil
	}

	return p.qid
}

/*
	Returns the current total amount of bandwidth that has been assigned to the pledge.
*/
func (p *Pledge) Get_bandw( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_in + p.bandw_out
}

/*
	Returns the current amount of bandwidth that has been assigned to the pledge for traffic outbound from host1.
*/
func (p *Pledge) Get_bandw_out( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_out
}

/*
	Returns the current amount of bandwidth that has been assigned to the pledge for traffic inbound to hsot1.
*/
func (p *Pledge) Get_bandw_in( ) ( int64 ) {
	if p == nil {
		return 0
	}

	return p.bandw_in
}

/*
	Returns pointers to both host strings that comprise the pledge.
*/
func (p *Pledge) Get_hosts( ) ( *string, *string ) {
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
func (p *Pledge) Get_values( ) ( h1 *string, h2 *string, p1 *string, p2 *string, commence int64, expiry int64, bw_in int64, bw_out int64 ) {
	if p == nil {
		return &empty_str, &empty_str, &empty_str, &empty_str, 0, 0, 0, 0
	}

	return p.host1, p.host2, p.tpport1, p.tpport2, p.commence, p.expiry, p.bandw_in, p.bandw_out
}

/*
	Return the dscp that was submitted with the reservation, and the state of the keep on
	exit flag.
*/
func (p *Pledge) Get_dscp( ) ( int, bool ) {
	if p == nil {
		return 0, false
	}

	return p.dscp, p.dscp_koe
}

/*
	Returns the list of path objects that are needed to fulfill the pledge. Mulitple
	paths occur if the network is split.
*/
func (p *Pledge) Get_path_list( ) ( []*Path ) {
	if p == nil {
		return nil
	}
	return p.path_list
}

/*
	Return the commence and expiry times.
*/
func (p *Pledge) Get_window( ) ( int64, int64 ) {
	return p.commence, p.expiry
}
