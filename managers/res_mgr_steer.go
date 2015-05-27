// vi: sw=4 ts=4:

/*

	Mnemonic:	res_mgr_steer
	Abstract:	reservation manager functions that are directly related to steering
				(broken out of fq_mgr to make merging easier).

	Date:		03 Nov 2014
	Author:		E. Scott Daniels

	Mods:		Fixes after merge with lite (lazy update) changes.
				27 Feb 2015 - Changes to work with lazy updates, long duration reservations
					and e*->l* fixes.
				26 May 2015 - Changes to support pledge as an interface.
*/

package managers

import (
	//"encoding/json"
	//"fmt"
	//"os"
	"strings"
	"time"

	//"codecloud.web.att.com/gopkgs/bleater"
	//"codecloud.web.att.com/gopkgs/clike"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/gizmos"
)


/*
	Given a protocol string and directionm, set the proper transport port values in 
	the fq_req structure. Direction is supplied as a forward == true or false value.
*/
func set_proto_port( fq_data *Fq_req, proto *string, forward bool ) {
	if proto != nil {								// set the protocol match port dest in forward direction, src in reverse
		toks := strings.Split( *proto, ":" );
		fq_data.Protocol = &toks[0]
		if forward {
			if len( toks ) == 2 {
				fq_data.Match.Tpdport = &toks[1]
			} else {
				fq_data.Match.Tpdport = &zero_string
			}
		} else {
			if len( toks ) == 2 {
				fq_data.Match.Tpsport = &toks[1]
			} else {
				fq_data.Match.Tpsport = &zero_string
			}
		}
	}
}


/*
	Generate flow-mod requests to the fq-manager for a given src,dest pair and list of 
	middleboxes.  This assumes that the middlebox list has been reversed if necessary. 
	Either source (ep1) or dest (ep2) may be nil which indicates a "to any" or "from any"
	intention. 

	DANGER:  We generate the flowmods in reverse order which _should_ generate the highest 
			priority f-mods first. This is absolutely necessary to prevent packet loops
			on the switches which can happen if a higher priority rule isn't in place 
			that would cause the lower priority rule to be skipped over. 
*/
func steer_fmods( ep1 *string, ep2 *string, mblist []*gizmos.Mbox, expiry int64, rname *string, proto *string, forward bool ) {
	var (
		fq_data *Fq_req
		fq_match *Fq_parms
		fq_action *Fq_parms
		mb	*gizmos.Mbox							// current middle box being worked with (must span various blocks)
	)

	if expiry < 5 {									// refuse if too short
		return
	}

	mstr := "0x00/0x01"								// meta data match string; match if mask 0x01 is not set
	mstr_2xx := "0x00/0x04"							// pri 200 rules match if 0x04 is not set
	nmb := len( mblist )
	for i := 0; i < nmb; i++ {						// check value of each in list and bail if any are nil
		if mblist[i] == nil {
			rm_sheep.Baa( 1, "IER: steer_fmods: unexpected nil mb i=%d nmb=%d", i,  nmb )
			return
		}
	}


	for i :=  nmb -1; i >= 0;  i-- {					// backward direction ep2->ep1 (see note in flower box)
		resub := "90 0"									// we resubmit to table 90 to set our meta data and then resub to 0 to catch openstack rules
		resub_2xx := "94 0"								// 2xx rules mark with 0x04 so they aren't skipped if a 300 rule matches

		if i == nmb - 1 {								// for last mb we need a rule that causes steering to be skipped based on mb mac
			fq_data = Mk_fqreq( rname )					// get a block and initialise to sane values
			fq_match = fq_data.Match
			fq_action = fq_data.Action

			fq_data.Pri = 300
			fq_data.Expiry = expiry

			mb = mblist[i]
			fq_match.Ip1 = ep1
			fq_match.Meta = &mstr

			fq_action.Resub = &resub

			set_proto_port( fq_data, proto, forward ) 		// set the protocol match port dest in forward direction, src in reverse

			if ep1 != nil {									// if source is a specific address, then we need only one 300 rule
				rm_sheep.Baa( 2, "specific endpoint, 300 fmod goes to the MB switch only" )
				fq_data.Match.Ip1 = nil									// there is no source to match at this point
				fq_data.Match.Smac = nil
				fq_data.Match.Ip2 = ep2
				fq_data.Swid, fq_data.Match.Swport = mb.Get_sw_port( )	// specific switch and input port needed for this fmod
				fq_data.Lbmac = mb.Get_mac()							// fqmgr will need the mac if the port is late binding
			} else {													// if no specific src, the 100 rule lives on each switch, so we must put a 300 on each too
				rm_sheep.Baa( 2, "no specific endpoint, 300 fmod goes to all switches" )
				fq_data.Swid = nil										// force to all switches
				fq_data.Match.Smac = mb.Get_mac()						// src for 300 is the last mbox
			}

			if ep1 == nil && ep2 == nil {
				rm_sheep.Baa( 1, "300 fmod not set -- src and dst were nil" )
			} else {
				jstr, _ := fq_data.To_json( ) 
				rm_sheep.Baa( 1, "write 300 fmod: %s", *jstr )

				msg := ipc.Mk_chmsg()
				msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_data, nil )			// final flow-mod from the last middlebox out
			}
		}

		fq_data = Mk_fqreq( rname )					// get a block and initialise to sane values
		fq_match = fq_data.Match
		fq_action = fq_data.Action

		fq_match.Meta = &mstr
		fq_action.Resub = &resub

		fq_data.Expiry = expiry
		fq_data.Match.Ip1 = ep1
		fq_data.Match.Ip2 = ep2
		set_proto_port( fq_data, proto, forward ) 		// set the protocol match port dest in forward direction, src in reverse

		if i == 0 {										// push the ingress rule (possibly to all switches)
			fq_data.Pri = 100

			mb = mblist[i]
			if ep1 != nil {
				rm_sheep.Baa( 1, "specific endpoint, 100 fmod goes to single switch: %s", *ep1 )
				_, fq_data.Match.Smac, fq_data.Swid, _ = get_hostinfo( ep1 )						// if a specific src host supplied, get it's switch and we'll land only one flow-mod on it
			} else {
				rm_sheep.Baa( 1, "no specific endpoint, 100 fmod goes to all switches" )

				fq_data.Swid = nil												// ensure unset; if ep1 is undefined (all), then 100 f-mod goes to all switches
			}

			fq_data.Match.Ip1 = nil												// for 100 rules we only want to match src based on mac in case both endpoint VMs live on same phys host
			fq_data.Nxt_mac = mb.Get_mac( )
			jstr, _ := fq_data.To_json( ) 
			rm_sheep.Baa( 1, "write ingress fmod: %s", *jstr )

			msg := ipc.Mk_chmsg()
			msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_data, nil )			// no response right now -- eventually we want an asynch error
		} else {																// push fmod on the switch that connects the previous mbox matching packets from it and directing to next mbox

			mb = mblist[i-1] 													// pull previous middlebox which will be the source and define the swtich and port for this rule
			fq_data.Swid, fq_data.Match.Swport = mb.Get_sw_port( )	 			// specific switch and input port needed for this fmod
			fq_data.Lbmac = mb.Get_mac()										// fqmgr will need the mac if the port is late binding (-128)
			fq_match.Meta = &mstr_2xx											// pri 2xx marks and avoids 0x04 so that they hit even if a 300 rule matched
			fq_data.Match.Smac = nil											// we match based on input port and dest mac, so no need for this
			if ep2 == nil {
				fq_data.Match.Ip1 = nil											// for L* there isn't an endpoint; prevent an IP match in fmod
			}
			fq_action.Resub = &resub_2xx

			if fq_data.Match.Dmac == nil && fq_data.Match.Ip2 == nil {			// for l* there won't be a destination endpoint inbound; need a lower priority in this case and an additional 2xx rule
				rm_sheep.Baa( 1, "adding 210 rule to match reverse" )
				fq_210 := fq_data.Clone()										// need to lay in a 210 f-mod first if there's no end point

				//clonedif ep2 == nil {
				//	fq_210.Match.Ip1 = nil										// for L* there isn't and endpoint and as such we need to prevent match on IP in fmod
				//}
				// clonedfq_210.Match.Smac = nil											// smac is nil since we want to match on the source IP

				//clonedfq_210.Swid, fq_210.Match.Swport = mb.Get_sw_port( ) 			// specific switch and input port needed for this fmod
				//clonedfq_210.Lbmac = mb.Get_mac()										// fqmgr will need the mac if the port is late binding (-128)
				//clonedfq_210.Action.Resub = &resub_2xx

				fq_210.Match.Ip2 = ep1											// the 210 rule will match the reverse (ip2 is the dest which we need to match on the fmod)
				fq_210.Pri = 210

				msg := ipc.Mk_chmsg()
				msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_210, nil )			// no response right now -- eventually we want an asynch error

				fq_data.Pri = 200
			} else {
				fq_data.Pri = 210												// ensure rule with a dest matches before a 2xx rule without dest
			}


			mb = mblist[i]	 													// now safe to get next middlebox in the list
			fq_data.Nxt_mac = mb.Get_mac( )
			jstr, _ := fq_data.To_json( ) 
			rm_sheep.Baa( 1, "write intermed fmod: %s", *jstr )

			msg := ipc.Mk_chmsg()
			msg.Send_req( fq_ch, nil, REQ_ST_RESERVE, fq_data, nil )			// flow mod for each intermediate link in backwards direction
		}
	}
}


/*
	Push the fmod requests to fq-mgr for a steering resrvation. 
*/
func push_st_reservation( gp *gizmos.Pledge, rname string, ch chan *ipc.Chmsg, hto_limit int64 ) {

	if gp == nil {												// expired
		return
	}

	p, ok :=  (*gp).( *gizmos.Pledge_steer )		// generic pledge better be a bw pledge!
	if ! ok {
		rm_sheep.Baa( 1, "internal error in push_st_reservation: pledge isn't a steering pledge" )
		(*gp).Set_pushed()						// prevent looping
		return
	}

	ep1, ep2, _, _, _, conclude, _, _ := p.Get_values( )		// hosts, ports and expiry are all we need
	now := time.Now().Unix()
	duration := conclude - now
	if hto_limit > 0  && duration > hto_limit {			// ovs might have int32 timeout values -- resmgr will refresh to achieve full duration
		duration = hto_limit
	}

	if (ep1 == nil || *ep1 == "") && (ep2 == nil || *ep2 == "") {		// expired, but not marked?
		rm_sheep.Baa( 1, "push_st_res: both end points were nil" )
		p.Set_pushed()													// prevent trying again
		return
	}

	ep1 = name2ip( ep1 )										// we work only with IP addresses; sets to nil if "" (L*)
	ep2 = name2ip( ep2 )

	nmb := p.Get_mbox_count()
	mblist := make( []*gizmos.Mbox, nmb ) 
	for i := range mblist {
		mblist[i] = p.Get_mbox( i )
	}
	steer_fmods( ep1, ep2, mblist, duration, &rname, p.Get_proto(), true )			// set forward fmods

	nmb--
	for i := range mblist {											// build middlebox list in reverse
		mblist[nmb-i] = p.Get_mbox( i )
	}
	steer_fmods( ep2, ep1, mblist, duration, &rname, p.Get_proto(), false )			// set backward fmods

	p.Set_pushed()
}
