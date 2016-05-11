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

	Mnemonic:	res_mgr
	Abstract:	Manages the inventory of reservations.
				We expect it to be executed as a goroutine and requests sent via a channel.
	Date:		02 December 2013
	Author:		E. Scott Daniels

	CFG:		These config file variables are used when present:
					default:alttable -  The OVS table number to be used for metadata marking.

					default:queue_type -  If "endpoint" then we only generate endpoint queues, not intermediate
									switch queues.

					resmgr:ckpt_dir	- name of the directory where checkpoint data is to be kept (/var/lib/tegu)
									FWIW: /var/lib/tegu selected based on description:
									http://www.tldp.org/LDP/Linux-Filesystem-Hierarchy/html/var.html

					resmgr:verbose	- Defines the initial verbose setting for reservation manager bleater

					deprecated - resmgr:set_vlan - If true (default) then we flag fq-mgr to add vlan setting to flow-mods

					resmgr:super_cookie - A cookie that can be used to manage any reservation.

					resmgr:hto_limit - The hard timeout limit that should be used to reset flow-mods on long reservation.

					resmgr:res_refresh - The rate (seconds) that reservations are refreshed if hto-limit is non-zero.


	TODO:		need a way to detect when skoogie/controller has been reset meaning that all
				pushed reservations need to be pushed again.

				need to check to ensure that a VM's IP address has not changed; repush
				reservation if it has and cancel the previous one (when skoogi allows drops)

	Mods:		03 Apr 2014 (sd) : Added endpoint flowmod support.
				30 Apr 2014 (sd) : Enhancements to send flow-mods and reservation request to agents (Tegu-light)
				13 May 2014 (sd) : Changed to support exit dscp value in reservation.
				18 May 2014 (sd) : Changes to allow cross tenant reservations.
				19 May 2014 (sd) : Changes to support using destination floating IP address in flow mod.
				07 Jul 2014 (sd) : Changed to send network manager a delete message when deleting a reservation
						rather than depending on the http manager to do that -- possible timing issues if we wait.
						Added support for reservation refresh.
				29 Jul 2014 : Change set user link cap such that 0 is a valid value, and -1 will delete.
				27 Aug 2014 : Changes to support using Fq_req for generating flowmods (support of
						meta marking of reservation traffic).
				28 Aug 2014 : Added message tags to crit/err/warn messages.
				29 Aug 2014 : Added code to allow alternate OVS table to be supplied from config.
				03 Sep 2014 : Corrected bug introduced with fq_req changes (ignored protocol and port)
				08 Sep 2014 : Fixed bugs with tcp oriented proto steering.
				24 Sep 2014 : Added support for ITONS traffic class demands.
				09 Oct 2014 : Added all_sys_up, and prevent checkpointing until all_sys_up is true.
				29 Oct 2014 : Corrected bug -- setting vlan id when VMs are on same switch.
				03 Nov 2014 : Removed straggling comments from the bidirectional fix.
						General cleanup to merge with steering code.
				17 Nov 2014 : Updated to support lazy data collection from openstack -- must update host
						information and push to network as we load from a checkpoint file.
				19 Nov 2014 : correct bug in loading reservation path.
				16 Jan 2014 : Allow mask on a tcp/udp port specification and to set priority a bit higher
						when a transport port is specified.
						Changed when meta table flow-mods are pushed (now with queues and only to hosts in
						the queue list).
				01 Feb 2014 : Disables periodic checkpointing as tegu_ha depends on checkpoint files
						written only when there are updates.
				09 Feb 2015 : Added timeout-limit to prevent overrun of virtual switch hard timeout value.
				10 Feb 2015 : Corrected bug -- reporting expired pledges in the get pledge list.
				24 Feb 2015 : Added mirroring
				27 Feb 2015 : Steering changes to work with lazy update.
				17 Mar 2015 : lite version of resmgr brought more in line with steering.
				25 Mar 2015 : Reservation pushing only happens after a new queue list is received from netmgr
						and sent to fq-mgr. The exception is if the hard switch timeout pops where reservations
						are pushed straight away (assumption is that queues don't change).
				20 Apr 2015 : Ensured that reservations are pushed following a cancel request.
				26 May 2015 : Conversion to support pledge as an interface.
				01 Jun 2015 : Corrected bug in delete all which was attempting to delete expired reservations.
				15 Jun 2015 : Added oneway support.
				18 Jun 2015 : Added oneway delete support.
				25 Jun 2015 : Corrected bug preventing mirror reservations from being deleted (they require an agent
						command to be run and it wasn't.)
				08 Sep 2015 : Prevent checkpoint files from being written in the same second (gh#22).
				08 Oct 2015 : Added !pushed check back to active reservation pushes.
				27 Jan 2015 : Changes to support passthru reservations.
				06 Mar 2016 : Added a second channel interface (rmgrlu_ch) to deal with lookup requests for
						mirrors since they need this from agent manager which was creating a deadlock.
				07 Mar 2016 : Special tickle channel introduced.  It allows only 5 tickels to be queued; tickle will
						drop and not block if full.  This prevents the main queue from being overrun with tickles
						if a request takes a while (chkpt load).
				08 Apr 2016 : Added retry cache to the inventory. Cache will track pledges which were loaded from
						the datacache (checkpoint) and we've not been able to vet.  They should be retried
						assuming that vetting failed because of a network graph issue (unknown path etc) and that
						later attempt will be successful.
				12 Apr 2016 : Added support to detect when a duplicate reservaiton should be allowed, and the previous
						one cancelled, due to a host move.	
*/

package managers

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/clike"
	"github.com/att/gopkgs/chkpt"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

//var (  NO GLOBALS HERE; use globals.go )

// --------------------------------------------------------------------------------------

/*
	Manages the reservation inventory
*/
type Inventory struct {
	cache		map[string]*gizmos.Pledge		// cache of pledges
	retry		map[string]*gizmos.Pledge		// pledges loaded from datacache that have not vetted
	ulcap_cache	map[string]int					// cache of user link capacity values (max value)
	chkpt		*chkpt.Chkpt
}

// --- Private --------------------------------------------------------------------------

/*
	Encapsulate all of the current reservations into a single json blob.
*/
func ( i *Inventory ) res2json( ) (json string, err error) {
	var (
		sep 	string = ""
	)

	err = nil;
	json = `{ "reservations": [ `

	for _, p := range i.cache {
		if ! (*p).Is_expired( ) {
			json += fmt.Sprintf( "%s%s", sep, (*p).To_json( ) )
			sep = ","
		}
	}

	json += " ] }"

	return
}

/*
	Given a name, send a request to the network manager to translate it to an IP address.
	If the name is nil or empty, we return nil. This is legit for steering in the case of
	L* endpoint specification.
*/
func name2ip( name *string ) ( ip *string ) {
	ip = nil

	if name == nil || *name == "" {
		return
	}

	ch := make( chan *ipc.Chmsg )
	defer close( ch )									// close it on return
	msg := ipc.Mk_chmsg( )
	msg.Send_req( nw_ch, ch, REQ_GETIP, name, nil )
	msg = <- ch
	if msg.State == nil {					// success
		ip = msg.Response_data.(*string)
	} else {
		rm_sheep.Baa( 2, "name didn't translate to ip: %s", name )
	}

	return
}

/*
	Given a name, get host info (IP, mac, switch-id, switch-port) from network.
*/
func get_hostinfo( name *string ) ( *string, *string, *string, int ) {

	if name != nil  &&  *name != "" {
		ch := make( chan *ipc.Chmsg );
		req := ipc.Mk_chmsg( )
		req.Send_req( nw_ch, ch, REQ_HOSTINFO, name, nil )		// get host info string (mac, ip, switch)
		req = <- ch
		if req.State == nil {
			htoks := strings.Split( req.Response_data.( string ), "," )					// results are: ip, mac, switch-id, switch-port; all strings
			return &htoks[0], &htoks[1], &htoks[2], clike.Atoi( htoks[3] )
		} else {
			rm_sheep.Baa( 1, "get_hostinfo: error from network mgr: %s", req.State )
		}
	}

	rm_sheep.Baa( 1, "get_hostinfo: no name provided" )
	return nil, nil, nil, 0
}


/*
	Handles a response from the fq-manager that indicates the attempt to send a proactive ingress/egress flowmod to skoogi
	has failed.  Issues a warning to the log, and resets the pushed flag for the associated reservation.
*/
func (i *Inventory) failed_push( msg *ipc.Chmsg ) {
	if msg.Req_data == nil {
		rm_sheep.Baa( 0, "IER: notification of failed push had no information" )
		return
	}

	fq_data := msg.Req_data.( *Fq_req ) 		// data that was passed to fq_mgr (we'll dig out pledge id

	// TODO: set a counter in pledge so that we only try to push so many times before giving up.
	rm_sheep.Baa( 1, "WRN: proactive ie reservation push failed, pledge marked unpushed: %s  [TGURMG002]", *fq_data.Id )
	p := i.cache[*fq_data.Id]
	if p != nil {
		(*p).Reset_pushed()
	}
}

/*
	Checks to see if any reservations expired in the recent past (seconds). Returns true if there were.
*/
func (i *Inventory) any_concluded( past int64 ) ( bool ) {

	for _, p := range i.cache {									// run all pledges that are in the cache
		if p != nil  &&  (*p).Concluded_recently( past ) {			// pledge concluded within past seconds
				return true
		}
	}

	return false
}

/*
	Checks to see if any reservations became active between (now - past) and the current time, or will become
	active between now and now + future seconds. (Past and future are number of seconds on either side of
	the current time to check and are NOT timestamps.)
*/
func (i *Inventory) any_commencing( past int64, future int64 ) ( bool ) {

	for _, p := range i.cache {							// run all pledges that are in the cache
		if p != nil  &&  ((*p).Commenced_recently( past ) || (*p).Is_active_soon( future ) ) {	// will activate between now and the window
				return true
		}
	}

	return false
}

/*
	Deprecated -- these should no longer be set by tegu and if really needed should
		be set by the ql_bw*fmods and other agent scripts.


	Push table 9x flow-mods. The flowmods we toss into the 90 range of
	tables generally serve to mark metadata in a packet since metadata
	cannot be marked prior to a resub action (flaw in OVS if you ask me).

	Marking metadata is needed so that when one of our f-mods match we can
	resubmit into table 0 without triggering a loop, or a match of any
	of our other rules.

	Table is the table number (we assume 9x, but it could be anything)
	Meta is a string supplying the value/mask that is used on the action (e.g. 0x02/0x02)
	to set the 00000010 bit as an and operation.
	Cookie is the cookie value used on the f-mod.
*/
func table9x_fmods( rname *string, host string, table int, meta string, cookie int ) {
		fq_data := Mk_fqreq( rname )							// f-mod request with defaults (output==none)
		fq_data.Table = table
		fq_data.Cookie = cookie
		fq_data.Expiry = 0										// never expire

		// CAUTION: fq_mgr generic fmod needs to be changed and when it does these next three lines will need to change too
		fq_data.Espq = gizmos.Mk_spq( host, -1, -1 )			// send to specific host
		dup_str := "br-int"										// these go to br-int only
		fq_data.Swid = &dup_str

		fq_data.Action.Meta = &meta								// sole purpose is to set metadata

		msg := ipc.Mk_chmsg()
		msg.Send_req( fq_ch, nil, REQ_GEN_FMOD, fq_data, nil )			// no response right now -- eventually we want an asynch error
}


/*
	Causes all alternate table flow-mods to be sent for the hosts in the given queue list
	It can be expensive (1-2 seconds/flow mod), so we assume this is being driven only
	when there are queue changes. Phsuffix is the host suffix that is added to any host
	name (e.g. -ops).
*/
func send_meta_fmods( qlist []string, alt_table int ) {
	target_hosts := make( map[string]bool )							// hosts that are actually affected by the queue list

	for i := range qlist {											// make a list of hosts we need to send fmods to
		toks := strings.SplitN( qlist[i], "/", 2 )					// split host from front
		if len( toks ) == 2 {										// should always be, but don't choke if not
			target_hosts[toks[0]] = true							// fq-mgr will add suffix if needed
		}
	}

	for h := range target_hosts {
		rm_sheep.Baa( 2, "sending metadata flow-mods to %s alt-table base %d", h, alt_table )
		id := "meta_" + h
		table9x_fmods( &id, h, alt_table, "0x01/0x01", 0xe5d )
		table9x_fmods( &id, h, alt_table+1, "0x02/0x02", 0xe5d )
	}
}


/*
	Runs the list of reservations in the cache and pushes out any that are about to become active (in the
	next 15 seconds).  Also handles undoing any mirror reservations that have expired.

	Favour_v6 is passed to push_bw and will favour the IPv6 address if a host has both addresses defined.

	Returns the number of reservations that were pushed.
*/
func (i *Inventory) push_reservations( ch chan *ipc.Chmsg, alt_table int, hto_limit int64, pref_v6 bool ) ( npushed int ) {
	var (
		bw_push_count	int = 0
		st_push_count	int = 0
		pend_count	int = 0
		pushed_count int = 0
	)

	rm_sheep.Baa( 4, "pushing reservations, %d in cache", len( i.cache ) )
	for rname, p := range i.cache {							// run all pledges that are in the cache
		if p != nil {
			if (*p).Is_expired() {								// some reservations need to be explicitly undone at expiry
				if (*p).Is_pushed() {							// no need if not pushed
					switch (*p).(type) {
						case *gizmos.Pledge_mirror: 				// mirror requests need to be undone when they become inactive
							undo_mirror_reservation( p, rname, ch )
					}

					(*p).Reset_pushed()
				}
			} else {
				if ! (*p).Is_pushed() && ((*p).Is_active() || (*p).Is_active_soon( 15 )) {			// not pushed, and became active while we napped, or will activate in the next 15 seconds
					switch (*p).(type) {
						case *gizmos.Pledge_bwow:
							bwow_push_res( p, &rname, ch, hto_limit, pref_v6 )
							(*p).Set_pushed( )

						case *gizmos.Pledge_bw:
							bw_push_count++
							bw_push_res( p, &rname, ch, hto_limit, alt_table, pref_v6 )

						case *gizmos.Pledge_steer:
							st_push_count++
							push_st_reservation( p, rname, ch, hto_limit )

						case *gizmos.Pledge_mirror:
							push_mirror_reservation( p, rname, ch )

						case *gizmos.Pledge_pass:
							pass_push_res( p, &rname, ch, hto_limit )
					}

					pushed_count++
				} else {					// stil pending
					pend_count++
				}
			}
		}
	}

	if st_push_count > 0 || bw_push_count > 0 || rm_sheep.Would_baa( 3 ) {			// bleat if we pushed something, or if higher level is set in the sheep
		rm_sheep.Baa( 1, "push_reservations: %d bandwidth, %d steering, %d pending, %d already pushed", bw_push_count, st_push_count, pend_count, pushed_count )
	}

	return pushed_count
}

/*
	Turn pause mode on for all current reservations and reset their push flag so that they all get pushed again.
*/
func (i *Inventory) pause_on( ) {
	for _, p := range i.cache {
		(*p).Pause( true )					// also reset the push flag
	}
}

/*
	Turn pause mode off for all current reservations and reset their push flag so that they all get pushed again.
*/
func (i *Inventory) pause_off( ) {
	for _, p := range i.cache {
		(*p).Resume( true )					// also reset the push flag
	}
}

/*
	Resets the pushed flag for all reservations so that we can periodically send them
	when needing to avoid timeout limits in virtual switches.
*/
func (i *Inventory) reset_push() {
	for _, p := range i.cache {
		(*p).Reset_pushed( )
	}
}

/*
	Run the set of reservations in the cache and write any that are not expired out to the checkpoint file.
	For expired reservations, we'll delete them if they test positive for extinction (dead for more than 120
	seconds).

	Because of timestamp limitations on the file system, it is possible for the start process to select the
	wrong checkpoint file if more than one checkpoint files were created within a second of each other. To
	prevent problems this function will only write a checkpoint if the last one was written more than two
	seconds ago (to avoid clock issues and the nano timer). If it hasn't been longe enough, this function
	returns true (retry) and the calling function should call again (probably after a tickler pop) to
	issue a checkpoint.  There is no need to "queue" anything because if several checkpoint requests are
	made in the same second, then all of them will be captured the next time a write is allowed and the
	inventory is parsed.  If the checkpoint can be written, then false is returned.  In either case,
	the time that the last checkpoint file was written is also returned.
*/
func (i *Inventory) write_chkpt( last int64 ) ( retry bool, timestamp int64 ) {

	now := time.Now().Unix()
	if now - last < 2 {
		rm_sheep.Baa( 2, "retry checkpoint signaled" )
		return true, last			// can only dump 1/min; show queued to force main loop to recall
	}

	err := i.chkpt.Create( )
	if err != nil {
		rm_sheep.Baa( 0, "CRI: resmgr: unable to create checkpoint file: %s  [TGURMG003]", err )
		return false, last
	}

	for nm, v := range i.ulcap_cache {							// write out user link capacity limits that have been set
		fmt.Fprintf( i.chkpt, "ucap: %s %d\n", nm, v ) 			// we'll check the overall error state on close
	}

	for key, p := range i.cache {
		s := (*p).To_chkpt()
		if s != "expired" {
			fmt.Fprintf( i.chkpt, "%s\n", s )		 					// we'll check the overall error state on close
		} else {
			if (*p).Is_extinct( 120 ) && (*p).Is_pushed( ) {			// if really old and extension was pushed, safe to clean it out
				rm_sheep.Baa( 1, "extinct reservation purged: %s", key )
				delete( i.cache, key )
			}
		}
	}

	for key, p := range i.retry {
		s := (*p).To_chkpt()
		if s != "expired" {
			fmt.Fprintf( i.chkpt, "%s\n", s )		 					// we'll check the overall error state on close
		} else {
			if (*p).Is_extinct( 120 ) && (*p).Is_pushed( ) {			// if really old and extension was pushed, safe to clean it out
				rm_sheep.Baa( 1, "extinct reservation purged: %s", key )
				delete( i.cache, key )
			}
		}
	}

	ckpt_name, err := i.chkpt.Close( )
	if err != nil {
		rm_sheep.Baa( 0, "CRI: resmgr: checkpoint write failed: %s: %s  [TGURMG004]", ckpt_name, err )
	} else {
		rm_sheep.Baa( 1, "resmgr: checkpoint successful: %s", ckpt_name )
	}

	return false, time.Now().Unix()				// not queued, and send back the new chkpt time
}

/*
	Given a host name, return all pledges that involve that host as a list.
	Currently no error is detected and the list may be nil if there are no pledges.
*/
func (inv *Inventory) pledge_list(  vmname *string ) ( []*gizmos.Pledge, error ) {

	if len( inv.cache ) <= 0 {
		return nil, nil
	}

	plist := make( []*gizmos.Pledge, len( inv.cache ) )
	i := 0
	for _, p := range inv.cache {
		if (*p).Has_host( vmname ) && ! (*p).Is_expired()  && ! (*p).Is_paused() {
			plist[i] = p
			i++
		}
	}

	return plist[0:i], nil
}

/*
	Set the user link capacity and forward it on to the network manager. We expect this
	to be a request from the far side (user/admin) or read from the chkpt file so
	the value is passed as a string (which is also what network wants too.
*/
func (inv *Inventory) add_ulcap( name *string, sval *string ) {
	val := clike.Atoi( *sval )

	pdata := make( []*string, 2 )		// parameters for message to network
	pdata[0] = name
	pdata[1] = sval

	if val >= 0 && val < 101 {
		rm_sheep.Baa( 2, "adding user cap: %s %d", *name, val )
		inv.ulcap_cache[*name] = val

		req := ipc.Mk_chmsg( )
		req.Send_req( nw_ch, nil, REQ_SETULCAP, pdata, nil ) 				// push into the network environment

	} else {
		if val == -1 {
			delete( inv.ulcap_cache, *name )
			req := ipc.Mk_chmsg( )
			req.Send_req( nw_ch, nil, REQ_SETULCAP, pdata, nil ) 				// push into the network environment
		} else {
			rm_sheep.Baa( 1, "user link capacity not set %d is out of range (1-100)", val )
		}
	}
}

// --- Public ---------------------------------------------------------------------------
/*
	constructor
*/
func Mk_inventory( ) (inv *Inventory) {

	inv = &Inventory { }

	inv.cache = make( map[string]*gizmos.Pledge, 4096 )		// initial size is not a limit but a hint
	inv.retry = make( map[string]*gizmos.Pledge, 2048 )
	inv.ulcap_cache = make( map[string]int, 64 )

	return
}

/*
	Stuff the pledge into the cache erroring if the pledge already exists.
	Expect either a Pledge, or a pointer to a pledge.
*/
func (inv *Inventory) Add_res( pi interface{} ) (err error) {
	var (
		p *gizmos.Pledge
	)

	err = nil

	px, ok := pi.( gizmos.Pledge )
	if ok {
		p = &px
	} else {
		py, ok := pi.( *gizmos.Pledge )
		if ok {
			p = py
		} else {
			err = fmt.Errorf( "internal mishap in Add_res: expected Pledge or *Pledge, got neither" )
			rm_sheep.Baa( 1, "%s", err )
			return
		}
	}

	id := (*p).Get_id()
	if inv.cache[*id] != nil {
		rm_sheep.Baa( 2, "reservation not added to inventory, already exists: %s", *id )
		err = fmt.Errorf( "reservation already exists: %s", *id )
		return
	}

	inv.cache[*id] = p

	rm_sheep.Baa( 1, "resgmgr: added reservation: %s", (*p).To_chkpt() )
	return
}

/*
	Return the reservation that matches the name passed in provided that the cookie supplied
	matches the cookie on the reservation as well.  The cookie may be either the cookie that
	the user supplied when the reservation was created, or may be the 'super cookie' admin
	'root' as you will, which allows access to all reservations. The return will be nil,nil
	if it's not found; nil,state indicates an error.
*/
func (inv *Inventory) Get_res( name *string, cookie *string ) (p *gizmos.Pledge, state error) {

	state = nil
	p = inv.cache[*name]
	if p == nil {
		state = fmt.Errorf( "cannot find reservation: %s", *name )
		return
	}

	if ! (*p).Is_valid_cookie( cookie ) &&  *cookie != *super_cookie {
		rm_sheep.Baa( 2, "resgmgr: denied fetch of reservation: cookie supplied (%s) didn't match that on pledge %s", *cookie, *name )
		p = nil
		state = fmt.Errorf( "not authorised to access or delete reservation: %s", *name )
		return
	}

	rm_sheep.Baa( 2, "resgmgr:: fetched reservation: %s", (*p).To_str() )
	return
}

/*
	Search the retry cache for the reservation and return if it is found and the given
	cookie matches, or the super cookie is given.
*/
func (inv *Inventory) Get_retry_res( name *string, cookie *string ) (p *gizmos.Pledge, state error) {

	state = nil
	p = inv.retry[*name]
	if p == nil {
		state = fmt.Errorf( "cannot find reservation in retry cache: %s", *name )
		return
	}

	if ! (*p).Is_valid_cookie( cookie ) &&  *cookie != *super_cookie {
		rm_sheep.Baa( 2, "resgmgr: denied fetch of reservation: cookie supplied (%s) didn't match that on pledge %s", *cookie, *name )
		p = nil
		state = fmt.Errorf( "not authorised to access or delete reservation: %s", *name )
		return
	}

	rm_sheep.Baa( 2, "resgmgr:: fetched reservation from retry cache: %s", (*p).To_str() )
	return
}

/*
	Check the two pledges (old, new) to see if the related physical hosts have moved.
	Returns true if the physical hosts have changed. We get the current physical location
	for the hosts from the network based on the new pledge, and look at the path of the
	old pledge to see if they are the same as what was captured in the original path.
	We return true if they are different.
*/
func phosts_changed( old *gizmos.Pledge, new *gizmos.Pledge ) ( bool ) {

	var (
		p2 *string = nil	
	)

	a1, a2 := (*new).Get_hosts( )							// get hosts from the new pledge
	ch := make( chan *ipc.Chmsg )						// do not close -- senders close channels
	req := ipc.Mk_chmsg( )
	req.Send_req( nw_ch, ch, REQ_GETPHOST, a1, nil )	// xlate hostnames to physical host location
	req = <- ch											// wait for response from network
	p1 := req.Response_data.( *string )

	if a2 != nil {
		if len( *a2) > 1  &&  (*a2)[0:1] != "!" {				// !// names aren't known, don't map
			req.Send_req( nw_ch, ch, REQ_GETPHOST, a2, nil )
			req = <- ch									
			if req.Response_data != nil {					// for an external address this will be unknown
				p2 = req.Response_data.( *string )
			}
		}
	}

	return (*old).Same_anchors( p1, p2 )
}

/*
	Search the given cache for a duplicate of the target pledge.  Return the reservation ID
	if a dup.  If a duplicate, and a bandwidth reservation, then test to see if the physical
	hosts have changed.  If they have, then we expire the old reservation, and allow this
	to go in by returning a nil string.
*/
func dup_in_cache( cache map[string]*gizmos.Pledge, target *gizmos.Pledge ) ( rid *string ) {

	isbw := false
	switch (*target).( type ) {
		case *gizmos.Pledge_bw:
			isbw = true

		case *gizmos.Pledge_bwow:
			isbw = true

		case *gizmos.Pledge_pass:
			isbw = true

		default:
	}

	rid = nil
	for _, r := range cache {
		if !(*r).Is_expired()  && (*target).Equals( r ) {			// duplicates; if a bandwidth pledge, see if anchor has shifted
			rid := (*r).Get_id( )								// duplicate id to send back if not a bw or no change to phost

			if isbw {											// if passed pledge is a bandwidth, check paths
				if ! phosts_changed( r, target ) {			// if they aren't on the same places, then we should refresh
					(*r).Reset_pushed( )							// we'll force this out
					(*r).Set_expiry( time.Now().Unix() + 15 )		// force expiry of old
					rm_sheep.Baa( 1, "duplicate with different anchors will be refreshed: %s", *r )
					return nil
				}
			}

			rm_sheep.Baa( 2, "duplicate detected: %s", *r )
			return rid
		}
	}

	return rid
}

/*
	Accept a reservation (pledge) and see if it matches any existing reservation in
	the inventory. If it does, return the reservation id as data, set error if
	we encounter problems.
*/
func (inv *Inventory) dup_check( p *gizmos.Pledge ) ( rid *string, state error ) {
	rid = nil
	state = nil

	if inv == nil {
		state = fmt.Errorf( "inventory is nil" )
		return
	}

	rid = dup_in_cache( inv.cache, p )
	if rid != nil {
		return rid, nil
	}

	rid = dup_in_cache( inv.retry, p )
	if rid != nil {
		return rid, nil
	}

/*
	bwr2, isbw  := (*p).( *gizmos.Pledge_bw )
	for _, r := range inv.cache {
		if !(*r).Is_expired()  && (*p).Equals( r ) {			// duplicates; if a bandwidth pledge, see if anchor has shifted
			rid := (*r).Get_id( )								// duplicate id to send back if not a bw or no change to phost

			if isbw {											// if passed pledge is a bandwidth, check paths
				bwr1, ok  := (*r).( *gizmos.Pledge_bw )			// should be, but be paranoid
				if ok {
					if ! phosts_changed( bw1, bw2 ) {			// if they aren't on the same places, then we should refresh
						(*r).Reset_pushed( )						// we'll force this out
						(*r).Set_expiry( time.Now().Unix() + 15 )	// force expiry of old
						rm_sheep.Baa( 1, "duplicate with different anchors will be refreshed: %s", *r )
						return nil, nil
					}
				}
			}

			rm_sheep.Baa( 2, "duplicate detected: %s", *r )
			return rid, nil
		}
	}

	for _, r := range inv.retry {							// not in main cache, check the retry cache
		if !(*r).Is_expired()  && (*p).Equals( r ) {
			rid := (*r).Get_id( )								// duplicate id to send back if not

			if isbw {											// if passed pledge is a bandwidth, check paths
				bwr1, ok  := (*r).( *gizmos.Pledge_bw )				// should be, but be paranoid
				if ok {
					if ! bwr1.Same_paths( bwr2 ) {				// different paths; something changed so trash the existing and let the new come in
						(*r).Reset_pushed( )						// we'll force this out
						(*r).Set_expiry( time.Now().Unix() + 15 )	// force expiry of old
						rm_sheep.Baa( 1, "duplicate with different anchors will be refreshed: %s", *r )
						return nil, nil
					}
				}
			}

			rm_sheep.Baa( 2, "duplicate detected: %s", *r )
			return rid, nil
		}
	}
*/

	return nil, nil
}


func (inv *Inventory) Get_mirrorlist() ( string ) {
	sep := ""
	bs := bytes.NewBufferString("")
	for _, gp := range inv.cache {
		//if (*p).Is_mirroring() && !(*p).Is_expired() {
		//if (*p).Is_ptype( gizmos.PT_MIRRORING ) && !(*p).Is_expired() {
		p, ok := (*gp).(*gizmos.Pledge_mirror)
		if ok  && !p.Is_expired() {
			bs.WriteString(fmt.Sprintf("%s%s", sep, *p.Get_id()))
			sep = " "
		}
	}
	return bs.String()
}

/*
	Looks for the named reservation and deletes it if found. The cookie must be either the
	supper cookie, or the cookie that the user supplied when the reservation was created.
	Deletion is affected by resetting the expiry time on the pledge to now + a few seconds.
	This will cause a new set of flow-mods to be sent out with an expiry time that will
	take them out post haste and without the need to send "delete" flow-mods out.

	This function sends a request to the network manager to delete the related queues. This
	must be done here so as to prevent any issues with the loosely coupled management of
	reservation and queue settings.  It is VERY IMPORTANT to delete the reservation from
	the network perspective BEFORE the expiry time is reset.  If it is reset first then
	the network splits timeslices based on the new expiry and queues end up dangling.
*/
func (inv *Inventory) Del_res( name *string, cookie *string ) (state error) {

	gp, state := inv.Get_res( name, cookie )

	if gp != nil {
		rm_sheep.Baa( 2, "resgmgr: deleted reservation: %s", (*gp).To_str() )
		state = nil

		switch p := (*gp).(type) {
			case *gizmos.Pledge_mirror:
				p.Set_expiry( time.Now().Unix() )					// expire the mirror NOW
				p.Set_pushed()						// need this to force undo to occur

			case *gizmos.Pledge_bw, *gizmos.Pledge_bwow:			// network handles either type
				ch := make( chan *ipc.Chmsg )						// do not close -- senders close channels
				req := ipc.Mk_chmsg( )
				req.Send_req( nw_ch, ch, REQ_DEL, p, nil )			// delete from the network point of view
				req = <- ch											// wait for response from network
				state = req.State
				p.Set_expiry( time.Now().Unix() + 15 )				// set the expiry to 15s from now which will force it out
				(*gp).Reset_pushed()								// force push of flow-mods that reset the expiry

			case *gizmos.Pledge_pass:
				p.Set_expiry( time.Now().Unix() + 15 )				// set the expiry to 15s from now which will force it out
				(*gp).Reset_pushed()								// force push of flow-mods that reset the expiry
		}
	} else {
		if state == nil {
			gp, state = inv.Get_retry_res( name, cookie )		// see if it's in the retry cache and cookie was valid for it
			if gp != nil {
				// FIXME????
				// do we need to mark and continue to retry this and after it passes vetting then let it delete by pusshing out
				// short term flow-mods?   this would cover the case where the flow-mods were pushed, but when tegu restarted ostack
				// didn't have enough info to vet the pledge, and thus the existing flow-mods do need to be reset on the phyisical
				// host.
				delete( inv.retry, *name )						// for pledges on the retry cache, they can just be deleted since no flow-mods exist etc
			}
		} else {
			rm_sheep.Baa( 2, "resgmgr: unable to delete reservation: not found: %s", *name )
		}
	}

	return
}


/*
	delete all of the reservations provided that the cookie is the super cookie. If cookie
	is a user cookie, then deletes all reservations that match the cookie.
*/
func (inv *Inventory) Del_all_res( cookie *string ) ( ndel int ) {
	var	(
		plist	[]*string			// we'll create a list to avoid deletion issues with range
		i		int
	)

	ndel = 0

	plist = make( []*string, len( inv.cache ) )			// build a list so we can safely remove from the map
	for _, pledge := range inv.cache {
		if ! (*pledge).Is_expired( ) {
			plist[i] = (*pledge).Get_id()
			i++
		}
	}
	plist = plist[:i]									// slice down to what was actually filled in

	for _, pname := range plist {
		rm_sheep.Baa( 2, "delete all attempt to delete: %s", *pname )
		err := inv.Del_res( pname,  cookie )
		if err == nil {
			ndel++
			rm_sheep.Baa( 1, "delete all deleted reservation %s", *pname )
		} else {
			rm_sheep.Baa( 1, "delete all skipped reservation %s", *pname )
		}
	}

	rm_sheep.Baa( 1, "delete all deleted %d reservations %s", ndel )
	return
}


/*
	Pulls the reservation from the inventory. Similar to delete, but not quite the same.
	This will clone the pledge. The clone is expired and left in the inventory to force
	a reset of flowmods. The network manager is sent a request to delete the queues
	associated with the path and the path is removed from the original pledge. The original
	pledge is returned so that it can be used to generate a new set of paths based on the
	hosts, expiry and bandwidth requirements of the initial reservation.

	Unlike the get/del functions, this is meant for internal support and does not
	require a cookie.

	It is important to delete the reservation from the network manager point of view
	BEFORE the expiry is reset. If expiry is set first then the network manager will
	cause queue timeslices to be split on that boundary leaving dangling queues.
*/
func (inv *Inventory) yank_res( name *string ) ( p *gizmos.Pledge, state error) {

	state = nil
	p = inv.cache[*name]
	if p != nil {
		switch pldg := (*p).(type) {
			case *gizmos.Pledge_bw:
				rm_sheep.Baa( 2, "resgmgr: yanked reservation: %s", (*p).To_str() )
				cp := pldg.Clone( *name + ".yank" )				// clone but DO NOT set conclude time until after network delete!

				icp := gizmos.Pledge(cp)							// must convert to a pledge interface
				inv.cache[*name + ".yank"] = &icp					// and then insert the address of the interface

				inv.cache[*name] = nil								// yank original from the list
				delete( inv.cache, *name )
				pldg.Set_path_list( nil )							// no path list for this pledge

				ch := make( chan *ipc.Chmsg )
				req := ipc.Mk_chmsg( )
				req.Send_req( nw_ch, ch, REQ_DEL, cp, nil )			// delete from the network point of view
				req = <- ch											// wait for response from network
				state = req.State

																// now safe to set these
				cp.Set_expiry( time.Now().Unix() + 1 )			// force clone to be expired
				cp.Reset_pushed( )								// force it to go out again

			// not supported for other pledge types
		}
	} else {
		state = fmt.Errorf( "no reservation with name: %s", *name )
		rm_sheep.Baa( 2, "resgmgr: unable to yank, no reservation with name: %s", *name )
	}

	return
}

/*
	Wait and respond to RMLU_ requests received on the channel.
	This interface is provided because agent manager wants to look up reservations
	rather than queuing the data to be attached to a reservation onto the main
	repmgr queue.
*/
func rm_lookup( my_chan chan *ipc.Chmsg, inv *Inventory ) {
	for {
		msg := <- my_chan					// wait for next message

		switch msg.Msg_type {
			case RMLU_GET_MIRRORS:									// user initiated get list of mirrors
				t := inv.Get_mirrorlist()
				msg.Response_data = &t;

			case RMLU_GET:											// user initiated get -- requires cookie
				data := msg.Req_data.( []*string )					// assume pointers to name and cookie
				msg.Response_data, msg.State = inv.Get_res( data[0], data[1] )

			default:
				rm_sheep.Baa( 1, "invalid request received by rm_lookup: %d", msg.Msg_type )
		}

		if msg.Response_ch != nil {			// if a response channel was provided
			msg.Response_ch <- msg			// send our result back to the requester
		}
	}
}

//---- res-mgr main goroutine -------------------------------------------------------------------------------

/*
	Executes as a goroutine to drive the reservation manager portion of tegu.
*/
func Res_manager( my_chan chan *ipc.Chmsg, cookie *string ) {

	var (
		inv	*Inventory
		msg	*ipc.Chmsg
		ckptd	string
		last_qcheck	int64 = 0			// time that the last queue check was made to set window
		last_chkpt	int64 = 0			// time that the last checkpoint was written
		retry_chkpt bool = false		// checkpoint needs to be retried because of a timing issue
		queue_gen_type = REQ_GEN_EPQMAP
		alt_table = DEF_ALT_TABLE		// table number where meta marking happens
		all_sys_up	bool = false;		// set when we receive the all_up message; some functions (chkpt) must wait for this
		hto_limit 	int = 3600 * 18		// OVS has a size limit to the hard timeout value, this caps it just under the OVS limit
		res_refresh	int64 = 0			// next time when we must force all reservations to refresh flow-mods (hto_limit nonzero)
		rr_rate		int = 3600			// refresh rate (1 hour)
		favour_v6 bool = true			// favour ipv6 addresses if a host has both defined.
	)

	super_cookie = cookie				// global for all methods

	rm_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	rm_sheep.Set_prefix( "res_mgr" )
	tegu_sheep.Add_child( rm_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	p := cfg_data["default"]["queue_type"]				// lives in default b/c used by fq-mgr too
	if p != nil {
		if *p == "endpoint" {
			queue_gen_type = REQ_GEN_EPQMAP
		} else {
			queue_gen_type = REQ_GEN_QMAP
		}
	}

	p = cfg_data["default"]["alttable"]				// alt table for meta marking
	if p != nil {
		alt_table = clike.Atoi( *p )
	}

	p = cfg_data["default"]["favour_ipv6"]
	if p != nil {
		favour_v6 = *p == "true"
	}

	if cfg_data["resmgr"] != nil {
		cdp := cfg_data["resmgr"]["chkpt_dir"]
		if cdp == nil {
			ckptd = "/var/lib/tegu/resmgr"							// default directory and prefix
		} else {
			ckptd = *cdp + "/resmgr"							// add prefix to directory in config
		}

		p = cfg_data["resmgr"]["verbose"]
		if p != nil {
			rm_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
		}

		/*
		p = cfg_data["resmgr"]["set_vlan"]
		if p != nil {
			set_vlan = *p == "true"
		}
		*/

		p = cfg_data["resmgr"]["super_cookie"]
		if p != nil {
			super_cookie = p
			rm_sheep.Baa( 1, "super-cookie was set from config file" )
		}

		p = cfg_data["resmgr"]["hto_limit"]					// if OVS or whatever has a max timeout we can ensure it's not surpassed
		if p != nil {
			hto_limit = clike.Atoi( *p )
		}

		p = cfg_data["resmgr"]["res_refresh"]				// rate that reservations are refreshed if hto_limit is non-zero
		if p != nil {
			rr_rate = clike.Atoi( *p )
			if rr_rate < 900 {
				if rr_rate < 120 {
					rm_sheep.Baa( 0, "NOTICE: reservation refresh rate in config is insanely low (%ds) and was changed to 1800s", rr_rate )
					rr_rate = 1800
				} else {
					rm_sheep.Baa( 0, "NOTICE: reservation refresh rate in config is too low: %ds", rr_rate )
				}
			}
		}
	}

	send_meta_counter := 200;										// send meta f-mods only now and again
	rm_sheep.Baa( 1, "ovs table number %d used for metadata marking", alt_table )

	res_refresh = time.Now().Unix() + int64( rr_rate )				// set first refresh in an hour (ignored if hto_limit not set
	inv = Mk_inventory( )
	inv.chkpt = chkpt.Mk_chkpt( ckptd, 10, 90 )

	last_qcheck = time.Now().Unix()

	tkl_ch := make( chan *ipc.Chmsg, 5 )								// special, short buffer, channel for tickles allows 5 to queue before blocking sender
	tklr.Add_spot( 2, tkl_ch, REQ_PUSH, nil, ipc.FOREVER )				// push reservations to agent just before they go live
	tklr.Add_spot( 1, tkl_ch, REQ_SETQUEUES, nil, ipc.FOREVER )			// drives us to see if queues need to be adjusted
	tklr.Add_spot( 5, tkl_ch, REQ_RTRY_CHKPT, nil, ipc.FOREVER )		// ensures that we retried any missed checkpoints
	tklr.Add_spot( 60, tkl_ch, REQ_VET_RETRY, nil, ipc.FOREVER )		// run the retry queue if it has size

	go rm_lookup( rmgrlu_ch, inv )

	rm_sheep.Baa( 3, "res_mgr is running  %x", my_chan )
	for {
		select {									// select next ready message on either channel
			case msg = <- tkl_ch:					// msg available on tickle channel
				msg.State = nil						// nil state is OK, no error
				my_chan <- msg;						// just pass it through; tkl_ch has a small buffer (blocks quickly) and this prevents filling the main queue w/ tickles if we get busy	

			case msg = <- my_chan:					// process message from the main channel
				rm_sheep.Baa( 3, "processing message: %d", msg.Msg_type )
				switch msg.Msg_type {
					case REQ_NOOP:			// just ignore

					case REQ_ADD:
						msg.State = inv.Add_res( msg.Req_data )			// add will determine the pledge type and do the right thing
						msg.Response_data = nil


					case REQ_ALLUP:			// signals that all initialisation is complete (chkpting etc. can go)
						all_sys_up = true
						// periodic checkpointing turned off with the introduction of tegu_ha
						//tklr.Add_spot( 180, my_chan, REQ_CHKPT, nil, ipc.FOREVER )		// tickle spot to drive us every 180 seconds to checkpoint

					case REQ_RTRY_CHKPT:									// called to attempt to send a queued checkpoint request
						if all_sys_up {
							if retry_chkpt {
								rm_sheep.Baa( 3, "invoking checkpoint (retry)" )
								retry_chkpt, last_chkpt = inv.write_chkpt( last_chkpt )
							}
						}

					case REQ_CHKPT:											// external thread has requested checkpoint
						if all_sys_up {
							rm_sheep.Baa( 3, "invoking checkpoint" )
							retry_chkpt, last_chkpt = inv.write_chkpt( last_chkpt )
						}

					case REQ_DEL:											// user initiated delete -- requires cookie
						data := msg.Req_data.( []*string )					// assume pointers to name and cookie
						if data[0] != nil  &&  *data[0] == "all" {
							inv.Del_all_res( data[1] )
							msg.State = nil
						} else {
							msg.State = inv.Del_res( data[0], data[1] )
						}

						inv.push_reservations( my_chan, alt_table, int64( hto_limit ), favour_v6 )			// must force a push to push augmented (shortened) reservations
						msg.Response_data = nil

					case REQ_DUPCHECK:
						if msg.Req_data != nil {
							msg.Response_data, msg.State = inv.dup_check(  msg.Req_data.( *gizmos.Pledge ) )
						}

					case REQ_GET:											// user initiated get -- requires cookie
						data := msg.Req_data.( []*string )					// assume pointers to name and cookie
						msg.Response_data, msg.State = inv.Get_res( data[0], data[1] )

					case REQ_LIST:											// list reservations	(for a client)
						msg.Response_data, msg.State = inv.res2json( )

					case REQ_LOAD:								// load from a checkpoint file
						data := msg.Req_data.( *string )		// assume pointers to name and cookie
						msg.State = inv.load_chkpt( data )
						msg.Response_data = nil
						rm_sheep.Baa( 1, "checkpoint file loaded" )

					case REQ_PAUSE:
						msg.State = nil							// right now this cannot fail in ways we know about
						msg.Response_data = ""
						inv.pause_on()
						res_refresh = 0;						// must force a push of everything on next push tickle
						rm_sheep.Baa( 1, "pausing..." )

					case REQ_RESUME:
						msg.State = nil							// right now this cannot fail in ways we know about
						msg.Response_data = ""
						res_refresh = 0;						// must force a push of everything on next push tickle
						inv.pause_off()

					case REQ_SETQUEUES:							// driven about every second to reset the queues if a reservation state has changed
						now := time.Now().Unix()
						if now > last_qcheck  &&  inv.any_concluded( now - last_qcheck ) || inv.any_commencing( now - last_qcheck, 0 ) {
							rm_sheep.Baa( 1, "channel states: rm=%d rmlu=%d fq=%d net=%d agent=%d", len( rmgr_ch ), len( rmgrlu_ch ), len( fq_ch ), len( nw_ch ), len( am_ch ) )
							rm_sheep.Baa( 1, "reservation state change detected, requesting queue map from net-mgr" )
							tmsg := ipc.Mk_chmsg( )
							tmsg.Send_req( nw_ch, my_chan, queue_gen_type, time.Now().Unix(), nil )		// get a queue map; when it arrives we'll push to fqmgr and trigger flow-mod push
						}
						last_qcheck = now

					case REQ_PUSH:								// driven every few seconds to check for need to refresh because of switch max timeout setting
						if hto_limit > 0 {						// if reservation flow-mods are capped with a hard timeout limit
							now := time.Now().Unix()
							if now > res_refresh {
								rm_sheep.Baa( 2, "refreshing all reservations" )
								inv.reset_push()							// reset pushed flag on all reservations to cause active ones to be pushed again
								res_refresh = now + int64( rr_rate )		// push everything again in an hour

								inv.push_reservations( my_chan, alt_table, int64( hto_limit ), favour_v6 )			// force a push of all
							}
						}


					case REQ_PLEDGE_LIST:						// generate a list of pledges that are related to the given VM
						msg.Response_data, msg.State = inv.pledge_list(  msg.Req_data.( *string ) )

					case REQ_SETULCAP:							// user link capacity; expect array of two string pointers (name and value)
						data := msg.Req_data.( []*string )
						inv.add_ulcap( data[0], data[1] )
						retry_chkpt, last_chkpt = inv.write_chkpt( last_chkpt )

					// CAUTION: the requests below come back as asynch responses rather than as initial message
					case REQ_IE_RESERVE:						// an IE reservation failed
						msg.Response_ch = nil					// immediately disable to prevent loop
						inv.failed_push( msg )					// suss out the pledge and mark it unpushed

					case REQ_GEN_QMAP:							// response caries the queue map that now should be sent to fq-mgr to drive a queue update
						fallthrough

					case REQ_GEN_EPQMAP:
						rm_sheep.Baa( 1, "received queue map from network manager" )

						qlist := msg.Response_data.( []string )							// get the qulist map for our use first
						if send_meta_counter >= 200 {
							send_meta_fmods( qlist, alt_table )								// push meta rules
							send_meta_counter = 0
						} else {
							send_meta_counter++
						}

						msg.Response_ch = nil											// immediately disable to prevent loop
						fq_data := make( []interface{}, 1 )
						fq_data[FQ_QLIST] = msg.Response_data
						tmsg := ipc.Mk_chmsg( )
						tmsg.Send_req( fq_ch, nil, REQ_SETQUEUES, fq_data, nil )		// send the queue list to fq manager to deal with

						inv.push_reservations( my_chan, alt_table, int64( hto_limit ), favour_v6 )			// now safe to push reservations if any activated

					case REQ_VET_RETRY:
						if inv != nil && len( inv.retry ) > 0 {
							inv.vet_retries( )
						}

					case REQ_YANK_RES:										// yank a reservation from the inventory returning the pledge and allowing flow-mods to purge
						if msg.Response_ch != nil {
							msg.Response_data, msg.State = inv.yank_res( msg.Req_data.( *string ) )
						}

					/* deprecated -- moved to rm_lookup
					case REQ_GET_MIRRORS:									// user initiated get list of mirrors
						t := inv.Get_mirrorlist()
						msg.Response_data = &t;
					*/

					default:
						rm_sheep.Baa( 0, "WRN: res_mgr: unknown message: %d [TGURMG001]", msg.Msg_type )
						msg.Response_data = nil
						msg.State = fmt.Errorf( "res_mgr: unknown message (%d)", msg.Msg_type )
						msg.Response_ch = nil				// we don't respond to these.
				}	// end main channel case

		}		// end select

		rm_sheep.Baa( 3, "processing message complete: %d", msg.Msg_type )
		if msg.Response_ch != nil {			// if a response channel was provided
			msg.Response_ch <- msg			// send our result back to the requester
		}
	}
}
