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
	"os"
	"strings"
	"time"

	"forge.research.att.com/gopkgs/clike"
)

type Pledge struct {
	host1		*string
	host2		*string
	tpport1		int			// transport port number or 0 if not defined
	tpport2		int			// thee match h1/h2 respectively
	commence	int64
	expiry		int64
	bandw_in	int64		// bandwidth to reserve inbound to host1
	bandw_out	int64		// bandwidth to reserve outbound from host1
	dscp		int			// dscp value that should be propigated
	id			*string		// name that the client can use to manage (modify/delete)
	qid			*string		// name that we'll assign to the queue which allows us to look up the pledge's queues
	usrkey		*string		// a 'cookie' supplied by the user to prevent any other user from modifying
	path_list	[]*Path		// list of paths that represent the bandwith and can be used to send flowmods etc.
	pushed		bool		// set when pledge has been pushed into the openflow environment (skoogi)
	paused		bool		// set if reservation has been paused
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
	Commence	int64
	Expiry		int64
	Bandwin		int64
	Bandwout	int64
	Dscp		int
	Id			*string
	Qid			*string
	Usrkey		*string
}

/*
	Constructor; creates a pledge.
	Creates a pledge of bandwidth between two hosts, allowing host2 to be nil which indicates that the 
	pledge exists between host1 and any other host. If commence is 0, then the current time (now) is used. 

	A nil pointer is returned if the expiry time is in the past and the comence time is adjusted forward 
	(to the current time) if it is less than the current time.
*/
func Mk_pledge( host1 *string, host2 *string, p1 int, p2 int, commence int64, expiry int64, bandw_in int64, bandw_out int64, id *string, usrkey *string, dscp int ) ( p *Pledge, err error ) {
	now := time.Now().Unix()

	err = nil
	p = nil

	if commence < now {				// ajust forward to better play with windows on the paths
		commence = now
	}

	if expiry <= commence {						// bug #156 fix
		err = fmt.Errorf( "bad expiry submitted, already expired: now=%d expiry=%d", now, expiry );
		obj_sheep.Baa( 2, "pledge: %s", err )
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
	}

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
	for i := range p.path_list {
		p.path_list[i] = nil
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
	p.host1 = &tokens[1]
	if len( tokens ) > 1 {
		p.tpport1 = clike.Atoi( tokens[2] )
	} else {
		p.tpport1 = 0
	}

	tokens = strings.Split( *jp.Host2, ":" )
	p.host2 = &tokens[1]
	if len( tokens ) > 1 {
		p.tpport2 = clike.Atoi( tokens[2] )
	} else {
		p.tpport2 = 0
	}

	p.commence = jp.Commence
	p.expiry = jp.Expiry
	p.id = jp.Id
	p.dscp = jp.Dscp
	p.usrkey = jp.Usrkey
	p.qid = jp.Qid
	p.bandw_out = jp.Bandwout
	p.bandw_in = jp.Bandwin

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

	s = fmt.Sprintf( "%s: togo=%ds %s h1=%s:%d h2=%s:%d id=%s qid=%s st=%d ex=%d bwi=%d bwo=%d push=%v", state, diff, caption, 
			*p.host1, p.tpport2, *p.host2, p.tpport2, *p.id, *p.qid, p.commence, p.expiry, p.bandw_in, p.bandw_out, p.pushed )
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
	
	json = fmt.Sprintf( `{ "state": %q, "time": %d, "bandwin": %d, "bandwout": %d, "host1": "%s:%d", "host2": "%s:%d", "id": %q, "qid": %q }`, state, diff, p.bandw_in,  p.bandw_out, *p.host1, p.tpport1, *p.host2, p.tpport2, *p.id, *p.qid )

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
	
	chkpt = fmt.Sprintf( `{ "host1": %q, "host2": %q, "commence": %d, "expiry": %d, "bandwin": %d, "bandwout": %d, "id": %q, "qid": %q, "usrkey": %q }`, *p.host1, *p.host2, p.commence, p.expiry, p.bandw_in, p.bandw_out, *p.id, *p.qid, *p.usrkey )

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
	Returns true if the pledge has not becoe active (the commence time is >= the current time).
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
fmt.Fprintf( os.Stderr, "__>>>> checking: %s == %s  %v\n", *c, *p.usrkey, bool( *c == *p.usrkey) )
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
		the h1 transport port number or 0
		the h2 transport port number or 0
		the commence time,
		the expiry time,
		the inbound bandwidth,
		the outbound bandwidth
*/
func (p *Pledge) Get_values( ) ( *string, *string, int, int, int64, int64, int64, int64 ) {
	if p == nil {
		return &empty_str, &empty_str, 0, 0, 0, 0, 0, 0
	}

	return p.host1, p.host2, p.tpport1, p.tpport2, p.commence, p.expiry, p.bandw_in, p.bandw_out 
}

/*
	Return the dscp that was submitted with the resrrvation
*/
func (p *Pledge) Get_dscp( ) ( int ) {
	if p == nil {
		return 0
	}

	return p.dscp;
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
